package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

type pgstattupleConn struct {
	queries []string
	args    [][]any
}

func (c *pgstattupleConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *pgstattupleConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	c.queries = append(c.queries, sql)
	c.args = append(c.args, args)
	return pgstattupleRow{}
}
func (c *pgstattupleConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.queries = append(c.queries, sql)
	return pgconn.CommandTag{}, nil
}
func (c *pgstattupleConn) Release() {}

type pgstattupleRow struct{}

func (pgstattupleRow) Scan(dest ...any) error {
	values := []any{int64(100), int64(4), int64(80), int64(1), int64(20), int64(40), 4.0}
	for i, value := range values {
		switch dst := dest[i].(type) {
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

func TestPgstattuple_RejectsWithoutExtension(t *testing.T) {
	conn := &pgstattupleConn{}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, pgstattupleArgs("public", "items"))
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "pgstattuple")
	require.Empty(t, conn.queries)
}

func TestPgstattuple_SanitizesIdentifier(t *testing.T) {
	conn := &pgstattupleConn{}
	target := &dispatcherTarget{conn: conn, pgstattuple: true}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), target, pgstattupleArgs(`weird"schema`, `table"name`))
	require.NoError(t, err)
	require.Len(t, conn.args, 1)
	require.Equal(t, `"weird""schema"."table""name"`, conn.args[0][0])
	require.Contains(t, conn.queries[2], "$1::regclass")
}

func TestPgstattuple_TimeoutIs300s(t *testing.T) {
	conn := &pgstattupleConn{}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn, pgstattuple: true}, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
	require.Contains(t, conn.queries, "SET LOCAL statement_timeout = '300s'")
}

func TestPgstattuple_NeverIssuesCreateExtension(t *testing.T) {
	conn := &pgstattupleConn{}
	_, err := NewPgstattupleExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn, pgstattuple: true}, pgstattupleArgs("public", "items"))
	require.NoError(t, err)
	for _, query := range conn.queries {
		require.False(t, strings.Contains(strings.ToLower(query), "create extension"), query)
	}
}
