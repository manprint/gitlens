package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/command"
)

func refs(pairs ...string) []funcRef {
	out := make([]funcRef, 0, len(pairs))
	for _, pair := range pairs {
		if schema, name, ok := strings.Cut(pair, "."); ok {
			out = append(out, funcRef{Schema: schema, Name: name})
			continue
		}
		out = append(out, funcRef{Name: pair})
	}
	return out
}

// The expressions are what PostgreSQL 18 printed for EXPLAIN VERBOSE in the
// probe that shaped the guard, plus the forms ruleutils uses for keywords,
// literals and casts.
func TestScanCalls_Deparsed(t *testing.T) {
	cases := []struct {
		text string
		want []funcRef
	}{
		{"app.evil()", refs("app.evil")},
		{"evil()", refs("evil")},
		{"(pg_relation_size('pg_class'::regclass, 'main'::text) + a)", refs("pg_relation_size")},
		{"count(*) OVER w1", refs("count")},
		{"pg_try_advisory_lock((s.pid)::bigint)", refs("pg_try_advisory_lock")},
		{"pg_stat_get_activity(NULL::integer)", refs("pg_stat_get_activity")},
		{`"lower"(b)`, refs("lower")},
		{`"Evil"(1)`, refs("Evil")},
		{`"my ""odd"" schema".f(x)`, refs(`my "odd" schema.f`)},
		{"COALESCE(t.a, 1)", nil},
		{"NULLIF(t.a, 0)", nil},
		{"EXTRACT(year FROM t.d)", nil},
		{"(t.a = ANY ('{1,2}'::integer[]))", nil},
		{"((a)::text)::numeric(10,2)", nil},
		{"'lower(x)'::text", nil},
		{"'it''s lower(x)'::text", nil},
		{`E'it\'s lower(x)'::text`, nil},
		{"(NOT (hashed SubPlan 1))", nil},
		{"(InitPlan 1).col1", nil},
		{"w1 AS (PARTITION BY t.a ORDER BY t.b)", nil},
		{"ROWS FROM(generate_series(1, 3), unnest(t.arr))", refs("generate_series", "unnest")},
		{"bernoulli ('50'::real)", refs("bernoulli")},
		{"(t.c)::character varying(10)[] IS DISTINCT FROM upper(t.d)", refs("upper")},
		{"(t.ts)::timestamp(3) with time zone", nil},
		{"(t.x)::double precision", nil},
		{"(t.x)::app.mytype", nil},
		{"(t.x)::text IS NULL", nil},
		{"$1 + $2", nil},
		{"lower(t.a) || upper(t.b) || lower(t.c)", refs("lower", "upper")},
		{"pg_catalog.lower(t.a)", refs("pg_catalog.lower")},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, scanCalls(tc.text, true), tc.text)
	}
}

func TestScanCalls_StatementText(t *testing.T) {
	statement := "SELECT App.Evil(), COALESCE(x, 1), \"Mixed\"(y) FROM t -- lower(x)\n" +
		"/* upper(x) /* nested */ still a comment: initcap(x) */ WHERE z = $1 AND $$ trim(x) $$ = $tag$ md5(x) $tag$"
	require.Equal(t, refs("app.evil", "coalesce", "Mixed"), scanCalls(statement, false))
}

func TestInspectPlan_CollectsNodesAndCalls(t *testing.T) {
	plan := []byte(`[{"Plan": {
		"Node Type": "Nested Loop",
		"Output": ["g.g", "lower(t.b)"],
		"Plans": [
			{"Node Type": "Function Scan", "Function Name": "generate_series", "Schema": "pg_catalog",
			 "Function Call": "generate_series(1, 3)", "Output": ["g.g"]},
			{"Node Type": "Foreign Scan", "Relation Name": "remote", "Schema": "public", "Output": ["app.evil()"]}
		]}}]`)
	facts, err := inspectPlan(plan)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Nested Loop", "Function Scan", "Foreign Scan"}, facts.nodes)
	require.ElementsMatch(t, refs("lower", "pg_catalog.generate_series", "generate_series", "app.evil"), facts.calls)
}

func TestFunctionHazard(t *testing.T) {
	builtin := func(name, volatility string) catalogFunction {
		return catalogFunction{Schema: "pg_catalog", Name: name, Volatility: volatility}
	}
	safe := []catalogFunction{
		builtin("lower", "i"),
		builtin("now", "s"),
		builtin("random", "v"),
		builtin("pg_relation_size", "v"),
		builtin("pg_stat_get_xact_numscans", "v"),
		builtin("pg_current_wal_lsn", "v"),
		{Schema: "information_schema", Name: "_pg_expandarray", Volatility: "i"},
		{Schema: "public", Name: "st_area", Volatility: "i", Extension: "postgis"},
		{Schema: "public", Name: "_postgis_selectivity", Volatility: "v", Extension: "postgis"},
		{Schema: "public", Name: "gen_random_bytes", Volatility: "v", Extension: "pgcrypto"},
		{Schema: "public", Name: "similarity", Volatility: "i", Extension: "pg_trgm"},
	}
	for _, f := range safe {
		require.Empty(t, functionHazard(f), f.String())
	}
	unsafe := []catalogFunction{
		builtin("pg_advisory_lock", "v"),
		builtin("pg_try_advisory_lock", "v"),
		builtin("pg_terminate_backend", "v"),
		builtin("pg_cancel_backend", "v"),
		builtin("pg_logical_emit_message", "v"),
		builtin("pg_notify", "v"),
		builtin("pg_stat_reset", "v"),
		builtin("set_config", "v"),
		builtin("setseed", "v"),
		builtin("query_to_xml", "v"),
		builtin("pg_read_file", "v"),
		builtin("txid_current", "s"),
		builtin("pg_current_xact_id", "s"),
		{Schema: "app", Name: "evil", Volatility: "s"},
		{Schema: "app", Name: "pure", Volatility: "i"},
		{Schema: "public", Name: "dblink", Volatility: "v", Extension: "dblink"},
		{Schema: "public", Name: "crosstab", Volatility: "s", Extension: "tablefunc"},
		{Schema: "public", Name: "addgeometrycolumn", Volatility: "v", Extension: "postgis"},
		{Schema: "public", Name: "set_limit", Volatility: "v", Extension: "pg_trgm"},
		{Schema: "topology", Name: "createtopology", Volatility: "v", Extension: "postgis_topology"},
	}
	for _, f := range unsafe {
		require.NotEmpty(t, functionHazard(f), f.String())
	}
}

func TestRefHazard_ResolvesLikeThePlanner(t *testing.T) {
	functions := []catalogFunction{
		{Schema: "pg_catalog", Name: "lower", Volatility: "i", Visible: true},
		{Schema: "app", Name: "lower", Volatility: "v"},
		{Schema: "app", Name: "evil", Volatility: "s"},
		{Schema: "public", Name: "twice", Volatility: "i", Visible: true},
		{Schema: "public", Name: "twice", Volatility: "v", Visible: true},
	}
	// An unqualified name resolves through the search_path only.
	require.Empty(t, refHazard(funcRef{Name: "lower"}, functions, true))
	require.Contains(t, refHazard(funcRef{Schema: "app", Name: "lower"}, functions, true), "app.lower()")
	// Not visible unqualified: a plan would have qualified it.
	require.Contains(t, refHazard(funcRef{Name: "evil"}, functions, true), "cannot find")
	require.Empty(t, refHazard(funcRef{Name: "evil"}, functions, false))
	// One unsafe overload makes the name unsafe.
	require.Contains(t, refHazard(funcRef{Name: "twice"}, functions, true), "user-defined")
	require.Contains(t, refHazard(funcRef{Name: "unknown"}, functions, true), "cannot find")
	require.Empty(t, refHazard(funcRef{Name: "unknown"}, functions, false))
}

func analyzeConn() *explainConn {
	return &explainConn{statement: "SELECT * FROM v", plan: []byte(`[{"Plan":{"Node Type":"Result"}}]`)}
}

func runAnalyze(t *testing.T, conn *explainConn) error {
	t.Helper()
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: true})
	return err
}

func requireRefusedBeforeAnalyze(t *testing.T, conn *explainConn, err error, reason string) {
	t.Helper()
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected), "want a refusal, got %v", err)
	require.Contains(t, rejected.Error(), reason)
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "ANALYZE", "the statement must not be executed")
	}
	require.Contains(t, conn.execSQL, "ROLLBACK")
}

func TestGuard_RefusesUserFunctionInPlan(t *testing.T) {
	conn := analyzeConn()
	conn.verbosePlan = []byte(`[{"Plan":{"Node Type":"Seq Scan","Output":["app.evil()"]}}]`)
	conn.functions = `[{"schema":"app","name":"evil","volatility":"s","extension":"","visible":false}]`
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "the plan calls app.evil(), which is a user-defined function")
}

func TestGuard_RefusesSideEffectBuiltinFromAView(t *testing.T) {
	conn := analyzeConn()
	conn.verbosePlan = []byte(`[{"Plan":{"Node Type":"Function Scan","Function Name":"pg_stat_get_activity","Schema":"pg_catalog",
		"Output":["pg_try_advisory_lock((s.pid)::bigint)"],"Function Call":"pg_stat_get_activity(NULL::integer)"}}]`)
	conn.functions = `[
		{"schema":"pg_catalog","name":"pg_stat_get_activity","volatility":"s","extension":"","visible":true},
		{"schema":"pg_catalog","name":"pg_try_advisory_lock","volatility":"v","extension":"","visible":true}]`
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "pg_catalog.pg_try_advisory_lock()")
}

func TestGuard_RefusesUnknownCallInPlan(t *testing.T) {
	conn := analyzeConn()
	conn.verbosePlan = []byte(`[{"Plan":{"Node Type":"Result","Output":["mystery(1)"]}}]`)
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "cannot find in pg_proc")
}

func TestGuard_RefusesForeignAndCustomScans(t *testing.T) {
	for _, node := range []string{"Foreign Scan", "Custom Scan"} {
		conn := analyzeConn()
		conn.verbosePlan = []byte(`[{"Plan":{"Node Type":"` + node + `"}}]`)
		err := runAnalyze(t, conn)
		requireRefusedBeforeAnalyze(t, conn, err, node)
	}
}

func TestGuard_RefusesHiddenCalls(t *testing.T) {
	conn := analyzeConn()
	conn.hidden = `[{"object":"operator app.===(integer,integer)","schema":"app","name":"eq","volatility":"i","extension":""}]`
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "operator app.===(integer,integer) calls app.eq()")
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "VERBOSE, FORMAT JSON, COSTS OFF", "refused before planning")
	}
}

func TestGuard_RefusesCallsInTheStatementBeforePlanning(t *testing.T) {
	conn := analyzeConn()
	conn.statement = "SELECT app.evil()"
	conn.functions = `[{"schema":"app","name":"evil","volatility":"i","extension":"","visible":false}]`
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "the statement calls app.evil()")
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "EXPLAIN", "nothing may be planned")
	}
}

func TestGuard_RefusesSuperuserSession(t *testing.T) {
	conn := analyzeConn()
	conn.session = `{"superuser":true,"signal":true,"read_all_data":true}`
	err := runAnalyze(t, conn)
	requireRefusedBeforeAnalyze(t, conn, err, "superuser")
}

func TestGuard_AllowsSafePlanAndDropsPrivileges(t *testing.T) {
	conn := analyzeConn()
	conn.verbosePlan = []byte(`[{"Plan":{"Node Type":"Aggregate","Output":["count(*)","max(pg_relation_size((c.oid)::regclass))"]}}]`)
	conn.functions = `[
		{"schema":"pg_catalog","name":"count","volatility":"i","extension":"","visible":true},
		{"schema":"pg_catalog","name":"max","volatility":"i","extension":"","visible":true},
		{"schema":"pg_catalog","name":"pg_relation_size","volatility":"v","extension":"","visible":true}]`
	require.NoError(t, runAnalyze(t, conn))
	require.NotContains(t, conn.execSQL, "SET LOCAL ROLE pg_read_all_data", "a session that cannot signal keeps its role")
	var analyzed bool
	for _, sql := range conn.execSQL {
		analyzed = analyzed || strings.HasPrefix(sql, "EXPLAIN (ANALYZE,")
	}
	require.True(t, analyzed)
}

// A session that can signal backends and cannot drop to pg_read_all_data
// plans nothing, ANALYZE or not: planning can run user code.
func TestGuard_SignalSessionWithoutReadAllDataIsRefused(t *testing.T) {
	for _, analyze := range []bool{false, true} {
		conn := analyzeConn()
		conn.session = `{"superuser":false,"signal":true,"read_all_data":false}`
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: analyze})
		requireRefusedBeforeAnalyze(t, conn, err, "can signal other backends")
		for _, sql := range conn.execSQL {
			require.NotContains(t, sql, "EXPLAIN")
		}
	}
}

// A session that can signal plans as pg_read_all_data, ANALYZE or not.
func TestGuard_SignalSessionPlansAsReadAllData(t *testing.T) {
	for _, analyze := range []bool{false, true} {
		conn := analyzeConn()
		conn.session = `{"superuser":false,"signal":true,"read_all_data":true}`
		_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1), Analyze: analyze})
		require.NoError(t, err)
		require.Contains(t, conn.execSQL, "SET LOCAL ROLE pg_read_all_data")
	}
}

// Plain EXPLAIN plans only when the database defines no function the planner
// may run. The planner runs IMMUTABLE and STABLE functions while it plans, so a
// plain EXPLAIN is refused before it plans, just as ANALYZE is.
func TestGuard_PlainExplainRefusesWhenThePlannerMayRunAFunction(t *testing.T) {
	conn := analyzeConn()
	conn.plannable = `[{"schema":"app","name":"current_tenant","volatility":"s","extension":""}]`
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected), "want a refusal, got %v", err)
	require.Contains(t, rejected.Error(), "app.current_tenant()")
	require.Contains(t, rejected.Error(), "user-defined function")
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "EXPLAIN (", "nothing may be planned")
	}
	require.Contains(t, conn.execSQL, "ROLLBACK")
}

func TestGuard_PlainExplainPlansWhenNoFunctionIsPlannable(t *testing.T) {
	conn := analyzeConn()
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	require.NoError(t, err)
	require.Len(t, conn.guardSQL, 3, "the session, the planning and the remote estimate checks run")
	var planned bool
	for _, sql := range conn.execSQL {
		planned = planned || strings.HasPrefix(sql, "EXPLAIN (FORMAT JSON, VERBOSE, COSTS)")
	}
	require.True(t, planned)
}

// A foreign table whose planning goes to its remote server is refused before
// anything is planned, as a plan does not show the remote side's functions.
func TestGuard_PlainExplainRefusesForeignTablesThatEstimateRemotely(t *testing.T) {
	conn := analyzeConn()
	conn.remote = `[{"relation":"public.remote_t"}]`
	_, err := NewExplainExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, command.Args{QueryID: int64Ptr(1)})
	var rejected *RejectedError
	require.True(t, errors.As(err, &rejected), "want a refusal, got %v", err)
	require.Contains(t, rejected.Error(), "use_remote_estimate")
	require.Contains(t, rejected.Error(), "public.remote_t")
	for _, sql := range conn.execSQL {
		require.NotContains(t, sql, "EXPLAIN (", "nothing may be planned")
	}
	require.Contains(t, conn.execSQL, "ROLLBACK")
}

func TestDiscardConn_ReleasesWhatCannotBeHijacked(t *testing.T) {
	conn := analyzeConn()
	discardConn(context.Background(), conn)
	require.True(t, conn.released)
}
