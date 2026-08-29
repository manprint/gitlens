package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

type explainConn struct {
	statement  string
	plan       []byte
	lookupErr  error
	explainErr error
	execSQL    []string
}

func (c *explainConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *explainConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT query FROM pg_stat_statements") {
		return explainRow{value: c.statement, err: c.lookupErr}
	}
	c.execSQL = append(c.execSQL, sql)
	return explainRow{value: c.plan, err: c.explainErr}
}
func (c *explainConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.execSQL = append(c.execSQL, sql)
	return pgconn.CommandTag{}, nil
}
func (c *explainConn) Release() {}

type explainRow struct {
	value any
	err   error
}

func (r explainRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch dst := dest[0].(type) {
	case *string:
		*dst = r.value.(string)
	case *[]byte:
		*dst = append((*dst)[:0], r.value.([]byte)...)
	case *json.RawMessage:
		*dst = append((*dst)[:0], r.value.([]byte)...)
	default:
		panic("unsupported explain scan destination")
	}
	return nil
}

func TestExplain_RejectsUtilityStatement(t *testing.T) {
	for _, statement := range []string{"CREATE TABLE x (id int)", "DROP TABLE x", "CALL f()", "DO $$ BEGIN END $$", "VACUUM"} {
		require.False(t, allowedExplainKeyword(firstSQLKeyword(statement)), statement)
	}
}

func TestExplain_AcceptsDMLAndWith(t *testing.T) {
	for _, statement := range []string{"SELECT 1", "INSERT INTO t VALUES (1)", "UPDATE t SET x=1", "DELETE FROM t", "MERGE INTO t USING s ON true WHEN MATCHED THEN UPDATE SET x=1", "VALUES (1)", "WITH q AS (SELECT 1) SELECT * FROM q"} {
		conn := &explainConn{statement: statement, plan: []byte(`[{"Plan":{"Node Type":"Result"}}]`)}
		target := &dispatcherTarget{conn: conn}
		_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1)})
		require.NoError(t, err, statement)
	}
}

func TestExplain_StripsLeadingCommentsBeforeKeyword(t *testing.T) {
	statement := " /* block */ -- line\n /* second */\n SELECT 1"
	require.Equal(t, "SELECT", firstSQLKeyword(statement))
}

func TestExplain_UnknownQueryIDIsRejected(t *testing.T) {
	conn := &explainConn{lookupErr: pgx.ErrNoRows}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(404)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "unknown")
}

func TestExplain_BuffersOnlyWithAnalyze(t *testing.T) {
	for _, analyze := range []bool{false, true} {
		conn := &explainConn{statement: "SELECT 1", plan: []byte(`[{"Plan":{}}]`)}
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: analyze})
		require.NoError(t, err)
		var explainSQL string
		for _, sql := range conn.execSQL {
			if strings.HasPrefix(sql, "EXPLAIN") {
				explainSQL = sql
			}
		}
		if analyze {
			require.Contains(t, explainSQL, "BUFFERS")
		} else {
			require.NotContains(t, explainSQL, "BUFFERS")
		}
	}
}

func TestExplain_PlanHashIgnoresActualFields(t *testing.T) {
	one, err := planHash([]byte(`[{"Plan":{"Node Type":"Index Scan","Actual Rows":1,"Actual Total Time":2,"Buffers":{"Shared Hit Blocks":3}}}]`))
	require.NoError(t, err)
	two, err := planHash([]byte(`[{"Plan":{"Node Type":"Index Scan","Actual Rows":99,"Actual Total Time":88,"Buffers":{"Shared Hit Blocks":77}}}]`))
	require.NoError(t, err)
	require.Equal(t, one, two)
}

func TestExplain_PlanHashDiffersOnDifferentShape(t *testing.T) {
	one, err := planHash([]byte(`[{"Plan":{"Node Type":"Index Scan"}}]`))
	require.NoError(t, err)
	two, err := planHash([]byte(`[{"Plan":{"Node Type":"Seq Scan"}}]`))
	require.NoError(t, err)
	require.NotEqual(t, one, two)
}

func TestExplain_AnalyzeAlwaysRollsBack(t *testing.T) {
	conn := &explainConn{statement: "UPDATE t SET x=1", explainErr: errors.New("parameter $1 required")}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: true})
	require.Error(t, err)
	require.Contains(t, conn.execSQL, "BEGIN")
	require.Contains(t, conn.execSQL, "SET LOCAL statement_timeout = '30s'")
	require.Contains(t, conn.execSQL, "ROLLBACK")
}
