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

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/pgtype"
)

type explainConn struct {
	statement   string
	plan        []byte
	lookupErr   error
	explainErr  error
	execSQL     []string
	lookupSQL   string
	lookupArgs  []any
	explainArgs []any
	simpleSQL   []string

	// The explain guard's answers: the session's privileges, the functions
	// the planner may run in the database, the pg_proc rows for the names it
	// looks up, the calls hidden in user-defined objects, and the VERBOSE
	// plan it inspects (plan when nil).
	session     string
	plannable   string
	functions   string
	hidden      string
	verbosePlan []byte
	verboseErr  error
	guardSQL    []string
	released    bool
}

func (c *explainConn) guardAnswer(sql string) string {
	answer, fallback := "", "[]"
	switch {
	case strings.Contains(sql, "explain guard, session"):
		answer, fallback = c.session, `{"superuser":false,"signal":false,"read_all_data":true}`
	case strings.Contains(sql, "explain guard, plannable functions"):
		answer = c.plannable
	case strings.Contains(sql, "explain guard, functions"):
		answer = c.functions
	case strings.Contains(sql, "explain guard, hidden calls"):
		answer = c.hidden
	}
	if answer == "" {
		return fallback
	}
	return answer
}

// SimpleQuery stands in for the verbatim simple-protocol path that
// placeholders take.
func (c *explainConn) SimpleQuery(_ context.Context, sql string) ([]*pgconn.Result, error) {
	c.execSQL = append(c.execSQL, sql)
	c.simpleSQL = append(c.simpleSQL, sql)
	if c.explainErr != nil {
		return nil, c.explainErr
	}
	return []*pgconn.Result{{Rows: [][][]byte{{c.plan}}}}, nil
}

func (c *explainConn) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *explainConn) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT n.nspname") { // check.ExtensionSchema
		return explainRow{value: "public"}
	}
	if strings.HasPrefix(sql, `SELECT query FROM "public"."pg_stat_statements"`) {
		c.lookupSQL = sql
		c.lookupArgs = args
		return explainRow{value: c.statement, err: c.lookupErr}
	}
	if strings.HasPrefix(sql, "/* pglens: explain guard") {
		c.guardSQL = append(c.guardSQL, sql)
		return explainRow{value: []byte(c.guardAnswer(sql))}
	}
	if strings.HasPrefix(sql, "EXPLAIN (VERBOSE, FORMAT JSON, COSTS OFF)") {
		c.execSQL = append(c.execSQL, sql)
		plan := c.verbosePlan
		if plan == nil {
			plan = c.plan
		}
		return explainRow{value: plan, err: c.verboseErr}
	}
	c.execSQL = append(c.execSQL, sql)
	c.explainArgs = args
	return explainRow{value: c.plan, err: c.explainErr}
}
func (c *explainConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	c.execSQL = append(c.execSQL, sql)
	return pgconn.CommandTag{}, nil
}
func (c *explainConn) Release() { c.released = true }

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

// PostgreSQL checks a statement's privileges even when it only plans it, so
// explaining a write fails for the read-only monitoring role; the refusal
// says why instead of only relaying "permission denied".
func TestExplain_WriteWithoutPrivilegeIsRejectedClearly(t *testing.T) {
	denied := &pgconn.PgError{Code: "42501", Message: "permission denied for table t"}
	conn := &explainConn{statement: "UPDATE t SET x = 1", explainErr: denied}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	require.ErrorContains(t, err, "explaining a UPDATE statement needs the privilege to run it, which the read-only monitoring role does not hold")
	var rejected *RejectedError
	require.ErrorAs(t, err, &rejected)

	conn = &explainConn{statement: "SELECT x FROM t", explainErr: denied}
	_, err = NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	require.ErrorContains(t, err, "normalised statement cannot be explained")
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

// planOutput is EXPLAIN (ANALYZE, BUFFERS, VERBOSE, FORMAT JSON) of an index
// scan, with the keys PostgreSQL adds to a real plan: buffer counters on the
// node and in Planning, timings, and triggers at the top level.
func planOutput(rows, hit, planRows, executionTime, indexName string) string {
	return `[{"Plan":{"Node Type":"Index Scan","Parallel Aware":false,"Async Capable":false,` +
		`"Scan Direction":"Forward","Index Name":"` + indexName + `","Relation Name":"t","Schema":"public","Alias":"t",` +
		`"Startup Cost":0.15,"Total Cost":8.17,"Plan Rows":` + planRows + `,"Plan Width":36,` +
		`"Actual Startup Time":0.02,"Actual Total Time":0.03,"Actual Rows":` + rows + `,"Actual Loops":1,` +
		`"Output":["id","v"],"Index Cond":"(id = 1)","Rows Removed by Index Recheck":0,` +
		`"Shared Hit Blocks":` + hit + `,"Shared Read Blocks":0,"Shared Dirtied Blocks":0,"Shared Written Blocks":0,` +
		`"Local Hit Blocks":0,"Local Read Blocks":0,"Local Dirtied Blocks":0,"Local Written Blocks":0,` +
		`"Temp Read Blocks":0,"Temp Written Blocks":0,"I/O Read Time":0.0,"I/O Write Time":0.0},` +
		`"Planning":{"Shared Hit Blocks":5,"Shared Read Blocks":0,"Shared Dirtied Blocks":0,"Shared Written Blocks":0,` +
		`"Local Hit Blocks":0,"Local Read Blocks":0,"Local Dirtied Blocks":0,"Local Written Blocks":0,` +
		`"Temp Read Blocks":0,"Temp Written Blocks":0,"I/O Read Time":0.0,"I/O Write Time":0.0},` +
		`"Planning Time":0.112,"Triggers":[],"Execution Time":` + executionTime + `}]`
}

// Two runs of the same statement differ in every runtime figure and in the
// estimates the statistics produced; the hash must not move with either.
func TestExplain_PlanHashIgnoresRuntimeAndEstimates(t *testing.T) {
	first, err := planHash([]byte(planOutput("1", "2", "1", "0.051", "t_pkey")))
	require.NoError(t, err)
	second, err := planHash([]byte(planOutput("1", "3", "40", "0.203", "t_pkey")))
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestExplain_PlanHashDiffersOnDifferentShape(t *testing.T) {
	one, err := planHash([]byte(`[{"Plan":{"Node Type":"Index Scan"}}]`))
	require.NoError(t, err)
	two, err := planHash([]byte(`[{"Plan":{"Node Type":"Seq Scan"}}]`))
	require.NoError(t, err)
	require.NotEqual(t, one, two)

	// The same node type on another index is another plan.
	withPrimary, err := planHash([]byte(planOutput("1", "2", "1", "0.05", "t_pkey")))
	require.NoError(t, err)
	withOther, err := planHash([]byte(planOutput("1", "2", "1", "0.05", "t_v_idx")))
	require.NoError(t, err)
	require.NotEqual(t, withPrimary, withOther)
}

func TestExplain_AnalyzeAlwaysRollsBack(t *testing.T) {
	conn := &explainConn{statement: "SELECT * FROM t", explainErr: errors.New("canceling statement due to statement timeout")}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: true})
	require.Error(t, err)
	require.Contains(t, conn.execSQL, "BEGIN READ ONLY")
	require.Contains(t, conn.execSQL, "SET LOCAL statement_timeout = '30s'")
	require.Contains(t, conn.execSQL, "ROLLBACK")
}

// The monitored database is never modified: every EXPLAIN, plan-only or not,
// runs in a READ ONLY transaction, never a plain BEGIN.
func TestExplain_AlwaysUsesReadOnlyTransaction(t *testing.T) {
	for _, analyze := range []bool{false, true} {
		conn := &explainConn{statement: "SELECT 1", plan: []byte(`[{"Plan":{}}]`)}
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: analyze})
		require.NoError(t, err)
		require.Contains(t, conn.execSQL, "BEGIN READ ONLY")
		require.NotContains(t, conn.execSQL, "BEGIN")
	}
}

func TestExplain_AnalyzeRejectsWritingStatements(t *testing.T) {
	for _, statement := range []string{"UPDATE t SET x=1", "INSERT INTO t VALUES (1)", "DELETE FROM t", "MERGE INTO t USING s ON true WHEN MATCHED THEN DELETE"} {
		conn := &explainConn{statement: statement, plan: []byte(`[{"Plan":{}}]`)}
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: true})
		var rejected *RejectedError
		require.True(t, errors.As(err, &rejected), statement)
		require.Contains(t, rejected.Error(), "read-only", statement)
		require.Empty(t, conn.execSQL, "nothing may be executed for %s", statement)
	}
}

func TestExplain_AnalyzeRejectsPlaceholders(t *testing.T) {
	conn := &explainConn{statement: "SELECT * FROM t WHERE id = $1", plan: []byte(`[{"Plan":{}}]`)}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: true})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Empty(t, conn.execSQL)
}

// TestExplain_PlaceholdersUseGenericPlanVerbatim: the statement reaches the
// server with its placeholders intact. pgx's simple-protocol mode would try
// to substitute them and fail with "insufficient arguments".
func TestExplain_PlaceholdersUseGenericPlanVerbatim(t *testing.T) {
	conn := &explainConn{statement: "SELECT * FROM t WHERE id = $1 AND v > $2", plan: []byte(`[{"Plan":{}}]`)}
	result, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	require.NoError(t, err)
	require.Len(t, conn.simpleSQL, 1)
	require.Contains(t, conn.simpleSQL[0], "GENERIC_PLAN")
	require.Contains(t, conn.simpleSQL[0], "id = $1 AND v > $2")
	require.Nil(t, conn.explainArgs, "the extended protocol is not used")
	var payload struct {
		Plan json.RawMessage `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(result, &payload))
	require.JSONEq(t, `[{"Plan":{}}]`, string(payload.Plan))
}

func TestExplain_PlaceholdersBeforePG16AreRejected(t *testing.T) {
	conn := &explainConn{statement: "SELECT * FROM t WHERE id = $1", plan: []byte(`[{"Plan":{}}]`)}
	target := &versionedTarget{dispatcherTarget: dispatcherTarget{conn: conn}, version: 150004}
	_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected))
	require.Contains(t, rejected.Error(), "PostgreSQL 16")
	require.Empty(t, conn.execSQL)
}

func TestExplain_RejectsMultipleStatements(t *testing.T) {
	for _, statement := range []string{"SELECT 1; SELECT 2", "SELECT 1; COMMIT; SET default_transaction_read_only = off", "SELECT ';'"} {
		conn := &explainConn{statement: statement, plan: []byte(`[{"Plan":{}}]`)}
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
		var rejected *RejectedError
		require.True(t, errors.As(err, &rejected), statement)
		require.Empty(t, conn.execSQL, statement)
	}
	stmt, ok := singleStatement("SELECT 1 ;\n")
	require.True(t, ok)
	require.Equal(t, "SELECT 1", stmt)
}

func TestExplain_LookupIsScopedToCurrentDatabase(t *testing.T) {
	conn := &explainConn{statement: "SELECT 1", plan: []byte(`[{"Plan":{}}]`)}
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Datname: "app"})
	require.NoError(t, err)
	require.Contains(t, conn.lookupSQL, "current_database()")
}

// maintenanceTarget hands out a separate connection for the maintenance
// database, where pg_stat_statements may be installed instead.
type maintenanceTarget struct {
	dispatcherTarget
	maint *explainConn
}

func (t *maintenanceTarget) Conn(context.Context) (check.Conn, error) { return t.maint, nil }

func TestExplain_ReadsTheTextFromTheMaintenanceDatabaseWithoutTheView(t *testing.T) {
	conn := &explainConn{lookupErr: &pgconn.PgError{Code: "42P01"}, plan: []byte(`[{"Plan":{}}]`)}
	maint := &explainConn{statement: "SELECT 1"}
	target := &maintenanceTarget{dispatcherTarget: dispatcherTarget{conn: conn}, maint: maint}

	_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1), Datname: "app"})

	require.NoError(t, err)
	require.Contains(t, maint.lookupSQL, "datname = $2", "the text of this database, read where the view exists")
	require.Contains(t, strings.Join(conn.execSQL, "\n"), "EXPLAIN (FORMAT JSON, VERBOSE, COSTS) SELECT 1", "the plan is still made in the command's database")
}

// The database name is free text, so the lookup binds it as an argument and
// never splices it into the statement. That is what lets a name that looks
// like SQL pass validation.
func TestExplain_DatnameStaysOutOfTheLookupSQL(t *testing.T) {
	const hostile = `app"; DROP TABLE t; --`
	conn := &explainConn{lookupErr: &pgconn.PgError{Code: "42P01"}, plan: []byte(`[{"Plan":{}}]`)}
	maint := &explainConn{statement: "SELECT 1"}
	target := &maintenanceTarget{dispatcherTarget: dispatcherTarget{conn: conn}, maint: maint}

	_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1), Datname: hostile})

	require.NoError(t, err)
	require.NotContains(t, maint.lookupSQL, "DROP")
	require.NotContains(t, maint.lookupSQL, "app")
	require.Equal(t, []any{int64(1), hostile}, maint.lookupArgs)
}

func TestExplain_NamesAMissingViewInBothDatabases(t *testing.T) {
	missing := &pgconn.PgError{Code: "42P01"}
	conn := &explainConn{lookupErr: missing}
	target := &maintenanceTarget{dispatcherTarget: dispatcherTarget{conn: conn}, maint: &explainConn{lookupErr: missing}}

	_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1), Datname: "app"})

	var rejected *RejectedError
	require.ErrorAs(t, err, &rejected)
	require.Contains(t, err.Error(), "not installed in database \"app\" nor in the maintenance database")
}

// sourcedTarget found the view in another database than the maintenance
// one, and hands out that database's connection.
type sourcedTarget struct {
	dispatcherTarget
	source string
	viaDB  *explainConn
}

func (t *sourcedTarget) StatementsDatabase() string { return t.source }
func (t *sourcedTarget) ConnFor(_ context.Context, datname string) (check.Conn, error) {
	if datname == t.source {
		return t.viaDB, nil
	}
	return t.conn, nil
}

func TestExplain_ReadsTheTextThroughTheDatabaseWithTheView(t *testing.T) {
	conn := &explainConn{lookupErr: &pgconn.PgError{Code: "42P01"}, plan: []byte(`[{"Plan":{}}]`)}
	viaDB := &explainConn{statement: "SELECT 1"}
	target := &sourcedTarget{dispatcherTarget: dispatcherTarget{conn: conn}, source: "reporting", viaDB: viaDB}

	_, err := NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1), Datname: "app"})

	require.NoError(t, err)
	require.Contains(t, viaDB.lookupSQL, "datname = $2")
	require.Contains(t, strings.Join(conn.execSQL, "\n"), "EXPLAIN (FORMAT JSON, VERBOSE, COSTS) SELECT 1")

	viaDB.lookupErr = &pgconn.PgError{Code: "42P01"}
	_, err = NewExplainExecutor().Execute(context.Background(), target, command.Args{QueryID: int64Ptr(1), Datname: "app"})
	require.ErrorContains(t, err, `not installed in database "app" nor in database "reporting"`)
}

type versionedTarget struct {
	dispatcherTarget
	version pgtype.PGVersion
}

func (t *versionedTarget) PGVersion() pgtype.PGVersion { return t.version }
