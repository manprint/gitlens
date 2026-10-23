package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

// pgstattupleConn is a connection to the database the relation lives in;
// installed says whether that database has the extension.
type pgstattupleConn struct {
	installed bool
	queries   []string
	args      [][]any
}

func (c *pgstattupleConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *pgstattupleConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c.queries = append(c.queries, sql)
	c.args = append(c.args, args)
	if strings.HasPrefix(sql, "SELECT n.nspname") { // check.ExtensionSchema
		return schemaRow(c.installed)
	}
	return pgstattupleRow{}
}
func (c *pgstattupleConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.queries = append(c.queries, sql)
	return pgconn.CommandTag{}, nil
}
func (c *pgstattupleConn) Release() {}

// schemaRow answers check.ExtensionSchema: the extension lives in public
// when it is installed.
type schemaRow bool

func (r schemaRow) Scan(dest ...any) error {
	if !r {
		return pgx.ErrNoRows
	}
	*dest[0].(*string) = "public"
	return nil
}

type pgstattupleRow struct{}

func (pgstattupleRow) Scan(dest ...any) error {
	values := []any{"postgres", int64(100), int64(4), int64(80), int64(1), int64(20), int64(40), 4.0}
	for i, value := range values {
		switch dst := dest[i].(type) {
		case *string:
			*dst = value.(string)
		case *int64:
			*dst = value.(int64)
		case *float64:
			*dst = value.(float64)
		default:
			panic("unsupported pgstattuple scan destination")
		}
	}
	return nil
}

func pgstattupleArgs(schema, relation string) command.Args {
	return command.Args{Schema: schema, Relation: relation, Datname: "postgres"}
}

// The maintenance database having the extension says nothing about the
// database the relation lives in.
func TestPgstattuple_RejectsWithoutExtensionInTheRelationsDatabase(t *testing.T) {
	conn := &pgstattupleConn{}
	target := &dispatcherTarget{conn: conn, pgstattuple: true}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), target, pgstattupleArgs("public", "items"))
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), `pgstattuple extension is not installed in database "postgres"`)
	for _, query := range conn.queries {
		require.NotContains(t, query, "pgstattuple\"(")
	}
}

func TestPgstattuple_MeasuresWhereTheExtensionIsInstalled(t *testing.T) {
	conn := &pgstattupleConn{installed: true}
	target := &dispatcherTarget{conn: conn} // not in the maintenance database
	_, err := NewPgstattupleExecutor().Execute(context.Background(), target, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
}

func TestPgstattuple_SanitizesIdentifier(t *testing.T) {
	conn := &pgstattupleConn{installed: true}
	target := &dispatcherTarget{conn: conn}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), target, pgstattupleArgs(`weird"schema`, `table"name`))
	require.NoError(t, err)
	var scan string
	for _, query := range conn.queries {
		if strings.Contains(query, `FROM "public"."pgstattuple"(`) {
			scan = query
		}
	}
	require.Contains(t, scan, "$1::pg_catalog.regclass")
	// The scan is the last QueryRow, and QueryRow records its arguments.
	require.Equal(t, []any{`"weird""schema"."table""name"`}, conn.args[len(conn.args)-1])
}

// The server-side timeout must expire before the dispatcher's own execution
// limit, or the scan keeps running after the agent has given up on it.
func TestPgstattuple_TimeoutFitsCommandLimit(t *testing.T) {
	conn := &pgstattupleConn{installed: true}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
	require.Contains(t, conn.queries, "SET LOCAL statement_timeout = '50s'")
	require.Less(t, 50*time.Second, commandExecutionLimit+time.Nanosecond)
}

func TestPgstattuple_UsesReadOnlyTransaction(t *testing.T) {
	conn := &pgstattupleConn{installed: true}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
	require.Equal(t, "BEGIN READ ONLY", conn.queries[0])
	require.NotContains(t, conn.queries, "BEGIN")
}

func TestPgstattuple_NeverIssuesCreateExtension(t *testing.T) {
	conn := &pgstattupleConn{installed: true}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
	for _, query := range conn.queries {
		require.False(t, strings.Contains(strings.ToLower(query), "create extension"), query)
	}
}
