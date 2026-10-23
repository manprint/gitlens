package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/check"
)

// The EXPLAIN guard. pglens never modifies a monitored database, and EXPLAIN
// is the one place where it runs SQL written by database users. The READ ONLY
// transaction makes PostgreSQL refuse writes made in it, but some effects
// happen outside it: session advisory locks outlive ROLLBACK, signals reach
// other backends, dblink and untrusted procedural languages reach other
// systems, and a few built-ins write WAL (pg_logical_emit_message) or consume
// a transaction ID (txid_current) inside a read-only transaction. Planning can
// run code too: the planner folds calls to IMMUTABLE functions with constant
// arguments, and evaluates STABLE ones for estimates.
//
// Every EXPLAIN therefore runs only after guardPlanning has found no function
// in the database that the planner may run, with as few privileges as the
// session can drop to (dropExplainPrivileges), on a connection that is closed
// afterwards (discardConn). EXPLAIN ANALYZE, which executes the statement, runs
// only when guardAnalyze finds nothing in the statement, in the database or in
// the VERBOSE plan that could act outside the transaction.
//
// What stays outside the guard, by construction: a function or view replaced
// concurrently between the check and the plan; code that runs while planning
// but is not a function in the database, such as a planner hook loaded with
// shared_preload_libraries; the IMMUTABLE and STABLE functions of installed
// extensions that a superuser owns, which guardPlanning does not list, so plain
// EXPLAIN folds them unchecked (ANALYZE refuses the ones outside
// sideEffectFreeExtensions that a statement reaches); and the remote planning
// of foreign-data wrappers other than postgres_fdw, whose use_remote_estimate
// guardPlanning refuses.

// sideEffectFreeExtensions only compute: no I/O outside the database, no SQL
// text executed on the caller's behalf, no state that outlives a transaction.
// The members a superuser owns are allowed whatever volatility they declare
// (random generators, PostGIS selectivity estimators), apart from the
// maintenance functions in extensionFunctionsDenied. A member a non-superuser
// added is not the extension's code: it is checked like any user function, so
// plain EXPLAIN is refused where one is IMMUTABLE or STABLE.
var sideEffectFreeExtensions = map[string]bool{
	"btree_gin":      true,
	"btree_gist":     true,
	"citext":         true,
	"cube":           true,
	"earthdistance":  true,
	"fuzzystrmatch":  true,
	"hstore":         true,
	"intarray":       true,
	"isn":            true,
	"ltree":          true,
	"pg_trgm":        true,
	"pgcrypto":       true,
	"postgis":        true,
	"postgis_sfcgal": true,
	"seg":            true,
	"unaccent":       true,
	"uuid-ossp":      true,
}

// extensionFunctionsDenied are the functions of a side-effect-free extension
// that change the database or the session. The read-only transaction refuses
// the DDL ones anyway; listing them gives a clear refusal instead.
var extensionFunctionsDenied = map[string]map[string]bool{
	"pg_trgm": {"set_limit": true},
	"postgis": {
		"addgeometrycolumn":          true,
		"dropgeometrycolumn":         true,
		"dropgeometrytable":          true,
		"populate_geometry_columns":  true,
		"postgis_extensions_upgrade": true,
		"updategeometrysrid":         true,
	},
}

// builtinVolatileAllowed are the volatile built-in functions that only read
// or compute. Every other volatile built-in is refused: volatility is how
// PostgreSQL marks a function with effects, and new ones appear with every
// major version.
var builtinVolatileAllowed = map[string]bool{
	"array_sample":                         true,
	"array_shuffle":                        true,
	"bernoulli":                            true,
	"clock_timestamp":                      true,
	"current_query":                        true,
	"currval":                              true,
	"gen_random_uuid":                      true,
	"lastval":                              true,
	"pg_blocking_pids":                     true,
	"pg_collation_actual_version":          true,
	"pg_control_checkpoint":                true,
	"pg_control_init":                      true,
	"pg_control_recovery":                  true,
	"pg_control_system":                    true,
	"pg_current_logfile":                   true,
	"pg_current_wal_flush_lsn":             true,
	"pg_current_wal_insert_lsn":            true,
	"pg_current_wal_lsn":                   true,
	"pg_database_collation_actual_version": true,
	"pg_database_size":                     true,
	"pg_get_wait_events":                   true,
	"pg_get_wal_replay_pause_state":        true,
	"pg_indexes_size":                      true,
	"pg_is_in_recovery":                    true,
	"pg_is_wal_replay_paused":              true,
	"pg_jit_available":                     true,
	"pg_last_committed_xact":               true,
	"pg_last_wal_receive_lsn":              true,
	"pg_last_wal_replay_lsn":               true,
	"pg_last_xact_replay_timestamp":        true,
	"pg_lock_status":                       true,
	"pg_notification_queue_usage":          true,
	"pg_partition_ancestors":               true,
	"pg_partition_tree":                    true,
	"pg_relation_size":                     true,
	"pg_safe_snapshot_blocking_pids":       true,
	"pg_sequence_last_value":               true,
	"pg_stat_have_stats":                   true,
	"pg_table_size":                        true,
	"pg_tablespace_size":                   true,
	"pg_total_relation_size":               true,
	"pg_xact_commit_timestamp":             true,
	"pg_xact_commit_timestamp_origin":      true,
	"pg_xact_status":                       true,
	"random":                               true,
	"random_normal":                        true,
	"system":                               true,
	"timeofday":                            true,
	"txid_status":                          true,
	"uuidv4":                               true,
	"uuidv7":                               true,
}

// builtinVolatileAllowedPrefixes cover function families that only read
// statistics.
var builtinVolatileAllowedPrefixes = []string{"pg_stat_get_"}

// builtinDenied are built-ins with effects that PostgreSQL does not declare
// VOLATILE: both assign a transaction ID, even in a read-only transaction.
var builtinDenied = map[string]bool{
	"pg_current_xact_id": true,
	"txid_current":       true,
}

// refusedPlanNodes run code the plan does not show: a foreign scan talks to
// another server through its foreign-data wrapper, and a custom scan runs an
// extension's executor.
var refusedPlanNodes = map[string]bool{
	"Custom Scan":  true,
	"Foreign Scan": true,
}

// funcRef names a function as the statement or the plan writes it. An empty
// schema means the name is resolved through the session's search_path.
type funcRef struct {
	Schema string
	Name   string
}

func (r funcRef) String() string {
	if r.Schema == "" {
		return r.Name + "()"
	}
	return r.Schema + "." + r.Name + "()"
}

// catalogFunction is one pg_proc row, as functionLookup reports it.
type catalogFunction struct {
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Volatility string `json:"volatility"`
	Extension  string `json:"extension"`
	Visible    bool   `json:"visible"`
}

func (f catalogFunction) String() string { return f.Schema + "." + f.Name + "()" }

// functionHazard reports why f could act outside the transaction, or "" when
// it cannot.
func functionHazard(f catalogFunction) string {
	if f.Extension != "" {
		if !sideEffectFreeExtensions[f.Extension] {
			return fmt.Sprintf("belongs to extension %s, which is not on the list of side-effect-free extensions", f.Extension)
		}
		if extensionFunctionsDenied[f.Extension][f.Name] {
			return fmt.Sprintf("changes the database or the session (extension %s)", f.Extension)
		}
		return ""
	}
	if f.Schema != "pg_catalog" && f.Schema != "information_schema" {
		return "is a user-defined function: its body is not part of the plan, and the volatility it declares is not enforced"
	}
	if builtinDenied[f.Name] {
		return "assigns a transaction ID even in a read-only transaction"
	}
	if f.Volatility == "v" && !builtinVolatileAllowedName(f.Name) {
		return "is a volatile built-in function that pglens does not know to be free of effects outside the transaction"
	}
	return ""
}

func builtinVolatileAllowedName(name string) bool {
	if builtinVolatileAllowed[name] {
		return true
	}
	for _, prefix := range builtinVolatileAllowedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// functionLookup lists every function named in $1, in any schema. visible
// marks the schemas an unqualified name can resolve to; both the plan and
// this lookup run in the same session, so they share the search_path. Only a
// function a superuser owns can count as an extension's, for the reason given
// at plannableFunctionLookup.
const functionLookup = `/* pglens: explain guard, functions */
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'schema', n.nspname,
		'name', p.proname,
		'volatility', p.provolatile,
		'extension', COALESCE(e.extname, ''),
		'visible', n.nspname = ANY (pg_catalog.current_schemas(true)))), '[]')
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_roles ow ON ow.oid = p.proowner
LEFT JOIN pg_catalog.pg_depend d
	ON d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = p.oid AND d.deptype = 'e'
LEFT JOIN pg_catalog.pg_extension e ON e.oid = d.refobjid AND ow.rolsuper
WHERE p.proname = ANY ($1::pg_catalog.name[])`

// hiddenCallLookup lists the functions that user-defined operators, casts,
// domain constraints and aggregates call. A plan does not always show these
// calls: CASE x WHEN y and NULLIF hide the equality operator, an implicit cast
// shows as its argument, a domain check runs inside record input, and an
// aggregate shows only its own name. Built-in objects (OIDs below
// FirstNormalObjectId, 16384) only reference built-in functions, so they are
// skipped. An object of a side-effect-free extension is not skipped: a
// non-superuser who can install such an extension can add an operator of
// their own to it (ALTER EXTENSION ... ADD OPERATOR), and the object is then
// checked by the function it calls like any other.
const hiddenCallLookup = `/* pglens: explain guard, hidden calls */
WITH refs(object, objclass, objid, fn) AS (
	SELECT 'operator ' || o.oid::pg_catalog.regoperator::pg_catalog.text,
	       'pg_catalog.pg_operator'::pg_catalog.regclass::pg_catalog.oid, o.oid, f.fn
	FROM pg_catalog.pg_operator o,
	     LATERAL (VALUES (o.oprcode::pg_catalog.oid), (o.oprrest::pg_catalog.oid), (o.oprjoin::pg_catalog.oid)) AS f(fn)
	WHERE o.oid >= 16384
	UNION ALL
	SELECT 'cast (' || pg_catalog.format_type(c.castsource, NULL) || ' AS ' || pg_catalog.format_type(c.casttarget, NULL) || ')',
	       'pg_catalog.pg_cast'::pg_catalog.regclass::pg_catalog.oid, c.oid, c.castfunc
	FROM pg_catalog.pg_cast c
	WHERE c.oid >= 16384
	UNION ALL
	SELECT 'domain ' || pg_catalog.format_type(con.contypid, NULL),
	       'pg_catalog.pg_type'::pg_catalog.regclass::pg_catalog.oid, con.contypid, d.refobjid
	FROM pg_catalog.pg_constraint con
	JOIN pg_catalog.pg_depend d
		ON d.classid = 'pg_catalog.pg_constraint'::pg_catalog.regclass AND d.objid = con.oid
		AND d.refclassid = 'pg_catalog.pg_proc'::pg_catalog.regclass
	WHERE con.contypid <> 0
	UNION ALL
	SELECT 'aggregate ' || a.aggfnoid::pg_catalog.oid::pg_catalog.regprocedure::pg_catalog.text,
	       'pg_catalog.pg_proc'::pg_catalog.regclass::pg_catalog.oid, a.aggfnoid::pg_catalog.oid, f.fn
	FROM pg_catalog.pg_aggregate a,
	     LATERAL (VALUES (a.aggtransfn::pg_catalog.oid), (a.aggfinalfn::pg_catalog.oid), (a.aggcombinefn::pg_catalog.oid),
	                     (a.aggserialfn::pg_catalog.oid), (a.aggdeserialfn::pg_catalog.oid), (a.aggmtransfn::pg_catalog.oid),
	                     (a.aggminvtransfn::pg_catalog.oid), (a.aggmfinalfn::pg_catalog.oid)) AS f(fn)
	WHERE a.aggfnoid::pg_catalog.oid >= 16384
)
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'object', r.object,
		'schema', n.nspname,
		'name', p.proname,
		'volatility', p.provolatile,
		'extension', COALESCE(e.extname, '')) ORDER BY r.object), '[]')
FROM refs r
JOIN pg_catalog.pg_proc p ON p.oid = r.fn
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_roles ow ON ow.oid = p.proowner
LEFT JOIN pg_catalog.pg_depend d
	ON d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = p.oid AND d.deptype = 'e'
LEFT JOIN pg_catalog.pg_extension e ON e.oid = d.refobjid AND ow.rolsuper
WHERE r.fn <> 0`

// hiddenCall is one function a user-defined object calls.
type hiddenCall struct {
	Object string `json:"object"`
	catalogFunction
}

// sessionPrivileges reports what the explain session could do beyond reading.
// $1 is the privilege that lets a session SET ROLE (SET from PostgreSQL 16,
// plain membership before).
const sessionPrivileges = `/* pglens: explain guard, session */
SELECT pg_catalog.json_build_object(
	'superuser', pg_catalog.current_setting('is_superuser') = 'on',
	'signal', pg_catalog.pg_has_role('pg_signal_backend', 'USAGE'),
	'read_all_data', pg_catalog.pg_has_role('pg_read_all_data', $1))`

type explainSession struct {
	Superuser   bool `json:"superuser"`
	Signal      bool `json:"signal"`
	ReadAllData bool `json:"read_all_data"`
}

// dropExplainPrivileges switches the transaction to pg_read_all_data, which
// can read every table and do nothing else, before anything is planned, when
// the session could otherwise act on other sessions: a tier 2 session holds
// pg_signal_backend, and a function run by the statement — or folded by the
// planner — could signal any backend. A session that can signal and cannot
// drop the privilege is refused. Other sessions keep their role, and with it
// any grant an EXPLAIN of their statements needs. SET LOCAL ends with the
// transaction. This is a default, not a boundary: PostgreSQL refuses RESET
// ROLE in the IMMUTABLE and STABLE functions planning runs, but such a
// function can still call set_config('role', ...), which is why discardConn
// and guardAnalyze exist as well.
func dropExplainPrivileges(ctx context.Context, conn check.Conn, major int, analyze bool) error {
	mode := "SET"
	if major < 16 {
		mode = "MEMBER"
	}
	var raw json.RawMessage
	if err := conn.QueryRow(ctx, sessionPrivileges, mode).Scan(&raw); err != nil {
		return fmt.Errorf("read explain session privileges: %w", err)
	}
	var session explainSession
	if err := json.Unmarshal(raw, &session); err != nil {
		return fmt.Errorf("decode explain session privileges: %w", err)
	}
	if analyze && session.Superuser {
		return rejectedf("EXPLAIN ANALYZE refused: the monitoring session is a superuser, which no check can contain; connect pglens with the monitoring role")
	}
	if !session.Signal && !session.Superuser {
		return nil
	}
	if !session.ReadAllData {
		return rejectedf("EXPLAIN refused: the monitoring session can signal other backends and cannot switch to pg_read_all_data to plan without that privilege")
	}
	if _, err := conn.Exec(ctx, "SET LOCAL ROLE pg_read_all_data"); err != nil {
		return fmt.Errorf("drop explain privileges: %w", err)
	}
	return nil
}

// plannableFunctionLookup lists the functions the database defines itself that
// the planner may run while it plans a statement: the functions declared
// IMMUTABLE or STABLE, which the planner folds or evaluates for estimates. A
// VOLATILE function runs only when it executes, so it is not listed. Built-in
// functions are left out, since PostgreSQL declares their volatility itself.
// The functions of extensions are left out: an installed extension is the
// trust boundary here, and ANALYZE checks the ones a statement reaches. Only
// a function a superuser owns is an extension's, though. A non-superuser who
// can install a trusted extension, such as pg_trgm, can add a function of
// their own to it with ALTER EXTENSION ... ADD FUNCTION, and the planner would
// run that function like any other user function. The C functions a superuser
// defined are not left out, and neither are the cost and selectivity
// estimators of operators, which are C functions: an IMMUTABLE or STABLE one
// of them is listed and refuses EXPLAIN like any other.
const plannableFunctionLookup = `/* pglens: explain guard, plannable functions */
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'schema', n.nspname,
		'name', p.proname,
		'volatility', p.provolatile) ORDER BY n.nspname, p.proname), '[]')
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND p.provolatile IN ('i', 's')
  AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_depend d
                  JOIN pg_catalog.pg_roles ow ON ow.oid = p.proowner
                  WHERE d.classid = 'pg_catalog.pg_proc'::pg_catalog.regclass AND d.objid = p.oid AND d.deptype = 'e'
                    AND ow.rolsuper)`

// guardPlanning refuses every EXPLAIN, plan-only or ANALYZE, while the database
// defines a function the planner may run, or a foreign table whose planning
// queries the remote server. A plan does not show those runs, and they happen
// before any plan exists, so the check reads the catalog rather than a plan: it
// does not depend on which functions a statement names, directly or through
// views, operators, casts or policies. The cost is that a database with its
// own IMMUTABLE or STABLE functions refuses EXPLAIN altogether, and the refusal
// names the function.
func guardPlanning(ctx context.Context, conn check.Conn) error {
	var raw json.RawMessage
	if err := conn.QueryRow(ctx, plannableFunctionLookup).Scan(&raw); err != nil {
		return fmt.Errorf("look up the functions the planner may run: %w", err)
	}
	var functions []catalogFunction
	if err := json.Unmarshal(raw, &functions); err != nil {
		return fmt.Errorf("decode the functions the planner may run: %w", err)
	}
	for _, f := range functions {
		if reason := functionHazard(f); reason != "" {
			return rejectedf("EXPLAIN refused: the database defines %s, which %s; the planner may run such a function while it plans a statement, and a plan does not show that", f, reason)
		}
	}
	if err := conn.QueryRow(ctx, remoteEstimateLookup).Scan(&raw); err != nil {
		return fmt.Errorf("look up the foreign tables that estimate remotely: %w", err)
	}
	var remote []struct {
		Relation string `json:"relation"`
	}
	if err := json.Unmarshal(raw, &remote); err != nil {
		return fmt.Errorf("decode the foreign tables that estimate remotely: %w", err)
	}
	if len(remote) > 0 {
		return rejectedf("EXPLAIN refused: foreign table %s has use_remote_estimate on, so planning sends an EXPLAIN to the remote server, which plans it there and runs the functions it folds; a plan does not show that", remote[0].Relation)
	}
	return nil
}

// remoteEstimateLookup lists the foreign tables whose planning queries the
// remote server. postgres_fdw does so when use_remote_estimate is set on the
// table or on its server: it sends EXPLAIN to the remote side, where the remote
// planner folds that server's IMMUTABLE functions. An option set to one of the
// spellings PostgreSQL reads as false does not count.
const remoteEstimateLookup = `/* pglens: explain guard, remote estimates */
SELECT COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object(
		'relation', n.nspname || '.' || c.relname)), '[]')
FROM pg_catalog.pg_foreign_table ft
JOIN pg_catalog.pg_class c ON c.oid = ft.ftrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_foreign_server s ON s.oid = ft.ftserver
WHERE EXISTS (SELECT 1 FROM pg_catalog.unnest(COALESCE(ft.ftoptions, '{}') || COALESCE(s.srvoptions, '{}')) AS o(opt)
              WHERE pg_catalog.split_part(o.opt, '=', 1) = 'use_remote_estimate'
                AND pg_catalog.lower(pg_catalog.split_part(o.opt, '=', 2)) NOT IN ('false', 'f', 'off', 'no', 'n', '0'))`

// guardAnalyze decides whether statement may be executed by EXPLAIN ANALYZE.
// It runs inside the read-only transaction, after dropExplainPrivileges and
// before the statement is executed, and returns a RejectedError naming the
// first hazard it finds.
func guardAnalyze(ctx context.Context, conn check.Conn, statement string) error {
	// The statement's own calls are checked before anything is planned, so
	// that a function named directly is refused before the planner can fold
	// it. The plan check below is the complete one; names the statement uses
	// that are not functions (keywords, type names) are ignored here.
	if refs := scanCalls(statement, false); len(refs) > 0 {
		functions, err := lookupFunctions(ctx, conn, refs)
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if reason := refHazard(ref, functions, false); reason != "" {
				return rejectedf("EXPLAIN ANALYZE refused: the statement calls %s", reason)
			}
		}
	}

	hidden, err := lookupHiddenCalls(ctx, conn)
	if err != nil {
		return err
	}
	for _, call := range hidden {
		if reason := functionHazard(call.catalogFunction); reason != "" {
			return rejectedf("EXPLAIN ANALYZE refused: %s calls %s, which %s; a plan does not always show such calls", call.Object, call.catalogFunction, reason)
		}
	}

	var plan json.RawMessage
	if err := conn.QueryRow(ctx, "EXPLAIN (VERBOSE, FORMAT JSON, COSTS OFF) "+statement).Scan(&plan); err != nil {
		return rejectedf("EXPLAIN ANALYZE refused: the statement cannot be planned for inspection: %v", err)
	}
	facts, err := inspectPlan(plan)
	if err != nil {
		return fmt.Errorf("inspect plan: %w", err)
	}
	for _, node := range facts.nodes {
		if refusedPlanNodes[node] {
			return rejectedf("EXPLAIN ANALYZE refused: the plan contains a %s, which runs code outside the plan", node)
		}
	}
	if len(facts.calls) == 0 {
		return nil
	}
	functions, err := lookupFunctions(ctx, conn, facts.calls)
	if err != nil {
		return err
	}
	for _, ref := range facts.calls {
		if reason := refHazard(ref, functions, true); reason != "" {
			return rejectedf("EXPLAIN ANALYZE refused: the plan calls %s", reason)
		}
	}
	return nil
}

// refHazard describes why ref is unsafe, or returns "". A name that matches
// no function is unsafe in a plan (strict), where every lowercase call is a
// function, and ignored in statement text, where it may be a keyword.
func refHazard(ref funcRef, functions []catalogFunction, strict bool) string {
	matched := false
	for _, f := range functions {
		if f.Name != ref.Name {
			continue
		}
		if ref.Schema != "" && f.Schema != ref.Schema {
			continue
		}
		if ref.Schema == "" && !f.Visible {
			continue
		}
		matched = true
		// Overloads are not told apart: the plan does not show argument
		// types, so one unsafe overload makes the name unsafe.
		if reason := functionHazard(f); reason != "" {
			return fmt.Sprintf("%s, which %s", f, reason)
		}
	}
	if !matched && strict {
		return fmt.Sprintf("%s, which pglens cannot find in pg_proc", ref)
	}
	return ""
}

func lookupFunctions(ctx context.Context, conn check.Conn, refs []funcRef) ([]catalogFunction, error) {
	names := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if !seen[ref.Name] {
			seen[ref.Name] = true
			names = append(names, ref.Name)
		}
	}
	sort.Strings(names)
	var raw json.RawMessage
	if err := conn.QueryRow(ctx, functionLookup, names).Scan(&raw); err != nil {
		return nil, fmt.Errorf("look up functions for the explain guard: %w", err)
	}
	var functions []catalogFunction
	if err := json.Unmarshal(raw, &functions); err != nil {
		return nil, fmt.Errorf("decode functions for the explain guard: %w", err)
	}
	return functions, nil
}

func lookupHiddenCalls(ctx context.Context, conn check.Conn) ([]hiddenCall, error) {
	var raw json.RawMessage
	if err := conn.QueryRow(ctx, hiddenCallLookup).Scan(&raw); err != nil {
		return nil, fmt.Errorf("look up operators, casts, domains and aggregates for the explain guard: %w", err)
	}
	var calls []hiddenCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil, fmt.Errorf("decode operators, casts, domains and aggregates for the explain guard: %w", err)
	}
	return calls, nil
}

// planFacts is what guardAnalyze needs from a plan: every node type and every
// function call in its expressions.
type planFacts struct {
	nodes []string
	calls []funcRef
}

// inspectPlan walks an EXPLAIN (VERBOSE, FORMAT JSON) document. VERBOSE
// deparses every expression — views expanded, SQL functions inlined — into
// the string fields of each node, where scanCalls finds the calls; a Function
// Scan also names its function in "Function Name" and "Schema".
func inspectPlan(plan json.RawMessage) (planFacts, error) {
	var document any
	if err := json.Unmarshal(plan, &document); err != nil {
		return planFacts{}, err
	}
	var facts planFacts
	seen := map[funcRef]bool{}
	add := func(ref funcRef) {
		if !seen[ref] {
			seen[ref] = true
			facts.calls = append(facts.calls, ref)
		}
	}
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []any:
			for _, child := range typed {
				walk(child)
			}
		case map[string]any:
			if node, ok := typed["Node Type"].(string); ok {
				facts.nodes = append(facts.nodes, node)
			}
			if name, ok := typed["Function Name"].(string); ok {
				if schema, ok := typed["Schema"].(string); ok {
					add(funcRef{Schema: schema, Name: name})
				}
			}
			// Keys in order, so that the first hazard reported does not
			// depend on map iteration.
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(typed[key])
			}
		case string:
			for _, ref := range scanCalls(typed, true) {
				add(ref)
			}
		}
	}
	walk(document)
	return facts, nil
}

// scanCalls returns the function calls written in text, in order of first
// appearance: a name, optionally schema-qualified, followed by "(". String
// literals, quoted identifiers, comments and the type names after "::" are
// not calls.
//
// deparsed is true for expressions printed by PostgreSQL in a plan. Those
// print every keyword in upper case and quote every name that is not all
// lower case, so an unquoted word with an upper-case letter is a keyword
// (COALESCE, ANY, AS, ROW, EXTRACT, ...), never a function. In statement
// text, unquoted words are folded to lower case as PostgreSQL folds them.
func scanCalls(text string, deparsed bool) []funcRef {
	var refs []funcRef
	seen := map[funcRef]bool{}
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'':
			escaped := i > 0 && (text[i-1] == 'E' || text[i-1] == 'e') && (i < 2 || !identByte(text[i-2]))
			i = skipStringLiteral(text, i, escaped)
		case c == '-' && i+1 < len(text) && text[i+1] == '-':
			i = skipLineComment(text, i)
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i = skipBlockComment(text, i)
		case c == '$':
			i = skipDollar(text, i)
		case c == ':' && i+1 < len(text) && text[i+1] == ':':
			i = skipTypeName(text, i+2)
		case c >= '0' && c <= '9':
			for i < len(text) && (identByte(text[i]) || text[i] == '.') {
				i++
			}
		case c == '"' || identStart(c):
			ref, keyword, end := readQualifiedName(text, i, deparsed)
			i = end
			j := end
			for j < len(text) && isSpace(text[j]) {
				j++
			}
			if keyword || j >= len(text) || text[j] != '(' {
				continue
			}
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		default:
			i++
		}
	}
	return refs
}

// readQualifiedName reads name[.name...] starting at i and returns the last
// two parts as schema and name. keyword reports a final part that cannot
// name a function in deparsed text.
func readQualifiedName(text string, i int, deparsed bool) (ref funcRef, keyword bool, end int) {
	var parts []string
	for {
		part, quoted, next, ok := readNamePart(text, i, deparsed)
		if !ok {
			end = i
			break
		}
		parts = append(parts, part)
		keyword = deparsed && !quoted && part != strings.ToLower(part)
		end = next
		if next < len(text) && text[next] == '.' && next+1 < len(text) && (text[next+1] == '"' || identStart(text[next+1])) {
			i = next + 1
			continue
		}
		break
	}
	switch len(parts) {
	case 0:
		return funcRef{}, true, end
	case 1:
		ref = funcRef{Name: parts[0]}
	default:
		ref = funcRef{Schema: parts[len(parts)-2], Name: parts[len(parts)-1]}
	}
	return ref, keyword, end
}

func readNamePart(text string, i int, deparsed bool) (part string, quoted bool, end int, ok bool) {
	if i >= len(text) {
		return "", false, i, false
	}
	if text[i] == '"' {
		var b strings.Builder
		j := i + 1
		for j < len(text) {
			if text[j] == '"' {
				if j+1 < len(text) && text[j+1] == '"' {
					b.WriteByte('"')
					j += 2
					continue
				}
				return b.String(), true, j + 1, true
			}
			b.WriteByte(text[j])
			j++
		}
		return b.String(), true, j, true
	}
	if !identStart(text[i]) {
		return "", false, i, false
	}
	j := i
	for j < len(text) && identByte(text[j]) {
		j++
	}
	part = text[i:j]
	if !deparsed {
		part = strings.ToLower(part)
	}
	return part, false, j, true
}

// typeNameWords may follow the first word of a multi-word type name:
// "character varying", "double precision", "timestamp with time zone",
// "interval day to second".
var typeNameWords = map[string]bool{
	"day": true, "hour": true, "minute": true, "month": true, "precision": true,
	"second": true, "time": true, "to": true, "varying": true, "with": true,
	"without": true, "year": true, "zone": true,
}

// skipTypeName skips the type name after "::": a possibly qualified name,
// the words of a multi-word type, numeric type modifiers and array bounds.
// Anything else ends the type name, so a call can never be hidden in it.
func skipTypeName(text string, i int) int {
	for i < len(text) && isSpace(text[i]) {
		i++
	}
	_, _, i = readQualifiedName(text, i, false)
	for {
		i = skipTypeModifier(text, i)
		for i+1 < len(text) && text[i] == '[' {
			end := strings.IndexByte(text[i:], ']')
			if end < 0 || strings.Trim(text[i+1:i+end], "0123456789") != "" {
				return i
			}
			i += end + 1
		}
		if i >= len(text) || text[i] != ' ' {
			return i
		}
		j := i + 1
		k := j
		for k < len(text) && identByte(text[k]) {
			k++
		}
		if k == j || !typeNameWords[strings.ToLower(text[j:k])] {
			return i
		}
		i = k
	}
}

// skipTypeModifier skips "(n)" or "(n,m)" at i.
func skipTypeModifier(text string, i int) int {
	if i >= len(text) || text[i] != '(' {
		return i
	}
	end := strings.IndexByte(text[i:], ')')
	if end < 0 || strings.Trim(text[i+1:i+end], "0123456789, ") != "" {
		return i
	}
	return i + end + 1
}

func skipStringLiteral(text string, i int, escaped bool) int {
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			if escaped {
				j++
			}
		case '\'':
			if j+1 < len(text) && text[j+1] == '\'' {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(text)
}

func skipLineComment(text string, i int) int {
	if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
		return i + end + 1
	}
	return len(text)
}

// skipBlockComment skips a /* */ comment; PostgreSQL nests them.
func skipBlockComment(text string, i int) int {
	depth := 0
	for j := i; j+1 < len(text); j++ {
		switch {
		case text[j] == '/' && text[j+1] == '*':
			depth++
			j++
		case text[j] == '*' && text[j+1] == '/':
			depth--
			j++
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(text)
}

// skipDollar skips a $n parameter or a $tag$...$tag$ string.
func skipDollar(text string, i int) int {
	j := i + 1
	if j < len(text) && text[j] >= '0' && text[j] <= '9' {
		for j < len(text) && text[j] >= '0' && text[j] <= '9' {
			j++
		}
		return j
	}
	for j < len(text) && text[j] != '$' && identByte(text[j]) {
		j++
	}
	if j >= len(text) || text[j] != '$' {
		return i + 1
	}
	tag := text[i : j+1]
	if end := strings.Index(text[j+1:], tag); end >= 0 {
		return j + 1 + end + len(tag)
	}
	return len(text)
}

func identStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func identByte(c byte) bool {
	return identStart(c) || (c >= '0' && c <= '9') || c == '$'
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// discardConn closes the connection an EXPLAIN used instead of returning it
// to the pool. Whatever the statement or the planner left in the session — a
// session advisory lock, which ROLLBACK does not release, a prepared
// statement, a setting — then ends with it, and the next caller gets a fresh
// session. The close outlives a cancelled command context: it is what ends
// the session.
func discardConn(ctx context.Context, conn check.Conn) {
	hijacker, ok := conn.(interface{ Hijack() *pgx.Conn })
	if !ok {
		conn.Release()
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = hijacker.Hijack().Close(ctx)
}
