package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/redact"
)

// ExplainExecutor explains the statement text associated with a queryid. It
// never accepts statement text from the command payload.
type ExplainExecutor struct{}

func NewExplainExecutor() *ExplainExecutor { return &ExplainExecutor{} }

func (e *ExplainExecutor) Kind() command.Kind { return command.KindExplain }

func (e *ExplainExecutor) Execute(ctx context.Context, target check.Target, args command.Args) (json.RawMessage, error) {
	if args.QueryID == nil {
		return nil, rejectedf("queryid is required")
	}
	conn, err := target.ConnFor(ctx, args.Datname)
	if err != nil {
		return nil, err
	}
	// The session ends with the command: see discardConn.
	defer discardConn(ctx, conn)

	statement, err := lookupStatement(ctx, target, conn, args.Datname, *args.QueryID)
	if errorsIsNoRows(err) {
		return nil, rejectedf("queryid %d is unknown on this instance", *args.QueryID)
	}
	if isUndefinedTable(err) {
		return nil, rejectedf("pg_stat_statements is not installed in database %q nor in %s", args.Datname, check.StatementsSourceName(target))
	}
	if err != nil {
		return nil, err
	}
	statement, ok := singleStatement(statement)
	if !ok {
		return nil, rejectedf("statement for queryid %d is not a single statement", *args.QueryID)
	}
	// The plan shows the constants of the statement, so a statement whose text
	// the stat_statements check withholds is not explained either.
	if redact.MayHoldCredential(statement) {
		return nil, rejectedf("statement for queryid %d may hold a credential; pglens does not explain it", *args.QueryID)
	}
	keyword := firstSQLKeyword(statement)
	if !allowedExplainKeyword(keyword) {
		return nil, rejectedf("statement keyword %q is not safe to explain", keyword)
	}
	hasParams := hasParameterPlaceholders(statement)
	if args.Analyze && !readOnlyExplainKeyword(keyword) {
		// ANALYZE executes the statement. pglens never modifies a monitored
		// database, so only statements that read are eligible; the READ ONLY
		// transaction below is the enforcement, this is the clear refusal.
		return nil, rejectedf("EXPLAIN ANALYZE would execute a %s statement; only read-only statements can be analyzed", keyword)
	}
	// pg_stat_statements replaces every constant with $n. The values come from
	// the command (typed by the operator, bound as parameters, never spliced
	// into the text), or else from a recent execution of the same statement
	// that pg_stat_activity still shows with its constants. Plan-only EXPLAIN
	// can do without them: GENERIC_PLAN on PostgreSQL 16+, a prepared statement
	// planned generically (plan_cache_mode = force_generic_plan) before 16.
	var params []*string
	valuesFrom := ""
	prepareGeneric := false
	if hasParams {
		count := placeholderCount(statement)
		switch {
		case len(args.Params) > 0:
			if len(args.Params) != count {
				return nil, rejectedf("the statement has %d parameter(s) ($1 to $%d) and the command gives %d value(s)", count, count, len(args.Params))
			}
			params, valuesFrom = args.Params, "values"
		case args.Analyze || target.PGVersion().Major() < 16:
			sample, err := recentExecution(ctx, conn, target.PGVersion().Major(), *args.QueryID, keyword)
			if err != nil {
				return nil, err
			}
			switch {
			case sample != "":
				statement, hasParams, valuesFrom = sample, false, "recent_execution"
			case args.Analyze:
				needs := "a value for its parameter $1"
				if count > 1 {
					needs = fmt.Sprintf("a value for each of its %d parameters ($1 to $%d)", count, count)
				}
				return nil, rejectedf("EXPLAIN ANALYZE needs %s: enter them, or run it while the application runs the statement, so that pglens can take the values of an execution pg_stat_activity shows", needs)
			default:
				prepareGeneric, valuesFrom = true, "generic"
			}
		default:
			valuesFrom = "generic"
		}
	}

	options := "FORMAT JSON, VERBOSE, COSTS"
	if args.Analyze {
		options = "ANALYZE, FORMAT JSON, VERBOSE, COSTS, BUFFERS"
	} else if hasParams && params == nil && !prepareGeneric {
		// GENERIC_PLAN plans a statement with placeholders and no values.
		options += ", GENERIC_PLAN"
	}

	// READ ONLY for both modes: the session is already read-only (see
	// ReadOnlySession), and saying so here keeps the guarantee local to the
	// one place that executes text written by database users. The transaction
	// makes PostgreSQL itself reject the writes made in it, under ANALYZE
	// INSERT/UPDATE/DELETE/MERGE, data-modifying CTEs, nextval() and the writes
	// of functions the statement calls. It does not reach a write made through
	// another session (dblink, postgres_fdw) or the other side effects listed
	// in explain_guard.go; the guard refuses those before the statement runs. Plan-only
	// EXPLAIN executes nothing; PostgreSQL still checks the statement's
	// privileges, so a write cannot even be planned by a role that only reads.
	if _, err := conn.Exec(ctx, "BEGIN READ ONLY"); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(ctx, "ROLLBACK") }()
	timeout := "SET LOCAL statement_timeout = '15s'"
	if args.Analyze {
		timeout = "SET LOCAL statement_timeout = '30s'"
	}
	if _, err := conn.Exec(ctx, timeout); err != nil {
		return nil, err
	}
	// The statement names its objects the way its application did, through
	// the database's own search_path rather than the pg_catalog-only one of
	// the agent's sessions. The guard resolves names through the same path.
	if _, err := conn.Exec(ctx, "SET LOCAL search_path TO DEFAULT"); err != nil {
		return nil, err
	}
	// The session turns JIT off (ReadOnlySession); the plan shows the database's own.
	if _, err := conn.Exec(ctx, "SET LOCAL jit TO DEFAULT"); err != nil {
		return nil, err
	}
	run := func(ctx context.Context, sql string) (json.RawMessage, error) {
		switch {
		case params != nil:
			return explainBound(ctx, conn, sql, params)
		case hasParams:
			// A statement with placeholders cannot go through the extended
			// protocol with no arguments (the server would expect a value per
			// placeholder); GENERIC_PLAN is designed for the simple protocol.
			// singleStatement above guarantees there is nothing after it.
			return explainVerbatim(ctx, conn, sql)
		default:
			var plan json.RawMessage
			err := conn.QueryRow(ctx, sql).Scan(&plan)
			return plan, err
		}
	}
	// Planning alone can run user code (see explain_guard.go), so the database
	// is checked before anything is planned, privileges are dropped next, and
	// ANALYZE runs only once the guard has inspected the statement and the plan.
	code := newCodeGraph(conn, target.PGVersion().Major())
	if err := guardPlanning(ctx, conn, code); err != nil {
		return nil, err
	}
	if err := dropExplainPrivileges(ctx, conn, target.PGVersion().Major(), args.Analyze); err != nil {
		return nil, err
	}
	// A tuning-workbook variant changes planner settings and adds hints only
	// now, inside the read-only transaction and after the privilege drop, so
	// the guard below and the EXPLAIN both see the variant's plan.
	if err := applyWorkbook(ctx, conn, args); err != nil {
		return nil, err
	}
	planned := withHints(statement, args.Hints)
	if args.Analyze {
		if err := guardAnalyze(ctx, conn, code, planned, run); err != nil {
			return nil, err
		}
	}

	explainSQL := fmt.Sprintf("EXPLAIN (%s) %s", options, planned)
	if prepareGeneric {
		// Before PostgreSQL 16 the generic plan of a prepared statement stands
		// in for GENERIC_PLAN: PREPARE infers the parameter types, and
		// force_generic_plan plans without looking at the NULLs EXECUTE passes.
		// PREPARE is allowed in a READ ONLY transaction and outlives it, but
		// the connection is discarded after the command (discardConn).
		if _, err := conn.Exec(ctx, "SET LOCAL plan_cache_mode = force_generic_plan"); err != nil {
			return nil, err
		}
		if err := execVerbatim(ctx, conn, "PREPARE "+genericStatementName+" AS "+planned); err != nil {
			if !readOnlyExplainKeyword(keyword) && isInsufficientPrivilege(err) {
				return nil, rejectedf("explaining a %s statement needs the privilege to run it, which the read-only monitoring role does not hold: %v", keyword, err)
			}
			return nil, rejectedf("normalised statement cannot be planned without values on PostgreSQL before 16: %v; enter a value for each parameter", err)
		}
		explainSQL = explainExecuteGenericSQL(options, placeholderCount(statement))
		hasParams = false
	}
	plan, scanErr := run(ctx, explainSQL)
	if scanErr != nil {
		if !readOnlyExplainKeyword(keyword) && isInsufficientPrivilege(scanErr) {
			// PostgreSQL checks the privileges a statement needs even when
			// it only plans it, and the monitoring role holds none that write.
			return nil, rejectedf("explaining a %s statement needs the privilege to run it, which the read-only monitoring role does not hold: %v", keyword, scanErr)
		}
		switch {
		case params != nil:
			return nil, rejectedf("the statement cannot be explained with the values given: %v", scanErr)
		case valuesFrom == "recent_execution":
			return nil, rejectedf("the statement cannot be explained with the values of its recent execution: %v", scanErr)
		case !args.Analyze:
			return nil, rejectedf("normalised statement cannot be explained: %v", scanErr)
		}
		return nil, scanErr
	}
	// The plan also shows the constants of the views and functions the planner
	// expanded, which the statement text does not: a view over dblink shows
	// its connection string. A plan with any sign of a credential is withheld.
	if redact.MayHoldCredential(string(plan)) {
		return nil, rejectedf("the plan for queryid %d may hold a credential; pglens does not ship it", *args.QueryID)
	}
	hash, err := planHash(plan)
	if err != nil {
		return nil, fmt.Errorf("hash explain plan: %w", err)
	}
	// The plan is filed under the database it was made in. A command that
	// names no database runs in the target's own, and the server has no other
	// way to learn that name.
	var datname string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&datname); err != nil {
		return nil, err
	}
	// values_from tells the console where the constants of the plan come
	// from: "values" typed by the operator, a "recent_execution", "generic"
	// (GENERIC_PLAN, or a prepared generic plan before 16), or "" when the statement has none.
	result, err := json.Marshal(struct {
		Plan       json.RawMessage   `json:"plan"`
		PlanHash   string            `json:"plan_hash"`
		Datname    string            `json:"datname"`
		ValuesFrom string            `json:"values_from,omitempty"`
		Settings   map[string]string `json:"settings,omitempty"`
		Hints      string            `json:"hints,omitempty"`
	}{Plan: plan, PlanHash: hash, Datname: datname, ValuesFrom: valuesFrom, Settings: args.Settings, Hints: args.Hints})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// singleStatement trims trailing semicolons and whitespace and reports
// whether what is left is one statement. pg_stat_statements stores one
// statement per entry, but the text is written by database users and is
// about to be executed by the monitoring role: any other semicolon, even one
// that would sit harmlessly inside a literal or a comment, is refused rather
// than parsed.
// simpleQuerier runs one statement over the simple query protocol exactly as
// written. Tests provide it directly; *pgxpool.Conn through its *pgx.Conn.
type simpleQuerier interface {
	SimpleQuery(ctx context.Context, sql string) ([]*pgconn.Result, error)
}

type pgxSimpleQuerier struct{ conn *pgx.Conn }

func (q pgxSimpleQuerier) SimpleQuery(ctx context.Context, sql string) ([]*pgconn.Result, error) {
	return q.conn.PgConn().Exec(ctx, sql).ReadAll()
}

// genericStatementName names the prepared statement whose generic plan
// stands in for GENERIC_PLAN before PostgreSQL 16.
const genericStatementName = "pglens_generic_plan"

// explainExecuteGenericSQL explains genericStatementName with a NULL for each
// of its count parameters.
func explainExecuteGenericSQL(options string, count int) string {
	nulls := strings.TrimSuffix(strings.Repeat("NULL, ", count), ", ")
	return fmt.Sprintf("EXPLAIN (%s) EXECUTE %s(%s)", options, genericStatementName, nulls)
}

func verbatimQuerier(conn check.Conn) (simpleQuerier, error) {
	switch c := conn.(type) {
	case simpleQuerier:
		return c, nil
	case interface{ Conn() *pgx.Conn }:
		return pgxSimpleQuerier{conn: c.Conn()}, nil
	}
	return nil, fmt.Errorf("connection %T cannot send a statement verbatim", conn)
}

// execVerbatim runs a statement that keeps its $n placeholders (PREPARE) over
// the simple query protocol.
func execVerbatim(ctx context.Context, conn check.Conn, sql string) error {
	querier, err := verbatimQuerier(conn)
	if err != nil {
		return err
	}
	_, err = querier.SimpleQuery(ctx, sql)
	return err
}

// explainVerbatim sends an EXPLAIN whose statement keeps its $n placeholders,
// which is what GENERIC_PLAN plans. pgx's QueryExecModeSimpleProtocol cannot
// carry it: that mode substitutes the placeholders on the client and fails
// with "insufficient arguments" when there are no values, so the statement
// goes to the lower-level connection untouched.
func explainVerbatim(ctx context.Context, conn check.Conn, sql string) (json.RawMessage, error) {
	querier, err := verbatimQuerier(conn)
	if err != nil {
		return nil, err
	}
	results, err := querier.SimpleQuery(ctx, sql)
	if err != nil {
		return nil, err
	}
	if len(results) != 1 || len(results[0].Rows) != 1 || len(results[0].Rows[0]) != 1 {
		return nil, errors.New("EXPLAIN returned an unexpected result shape")
	}
	return json.RawMessage(append([]byte(nil), results[0].Rows[0][0]...)), nil
}

func singleStatement(statement string) (string, bool) {
	trimmed := strings.TrimRight(statement, " \t\r\n;")
	if trimmed == "" || strings.Contains(trimmed, ";") {
		return "", false
	}
	return trimmed, true
}

var parameterPlaceholder = regexp.MustCompile(`\$[0-9]+`)

func hasParameterPlaceholders(statement string) bool {
	return parameterPlaceholder.MatchString(statement)
}

// placeholderCount is the highest $n in statement: the number of values it
// takes.
func placeholderCount(statement string) int {
	highest := 0
	for _, match := range parameterPlaceholder.FindAllString(statement, -1) {
		if n, err := strconv.Atoi(match[1:]); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// recentExecutionLookup lists what pg_stat_activity shows of the sessions
// whose current or last statement is this queryid, in this database, newest
// first. A text as long as track_activity_query_size allows was cut and is
// left out. query_id exists from PostgreSQL 14.
const recentExecutionLookup = `/* pglens: explain, recent execution */
SELECT a.query
FROM pg_catalog.pg_stat_activity a
WHERE a.query_id = $1
  AND a.datid = (SELECT d.oid FROM pg_catalog.pg_database d WHERE d.datname = pg_catalog.current_database())
  AND a.pid <> pg_catalog.pg_backend_pid()
  AND pg_catalog.octet_length(a.query) < (SELECT s.setting::int FROM pg_catalog.pg_settings s WHERE s.name = 'track_activity_query_size') - 1
ORDER BY a.query_start DESC NULLS LAST
LIMIT 20`

// recentExecution returns the text of a recent execution of the statement
// with its constants, as its application sent it, or "" when pg_stat_activity
// shows none that pglens can use: one statement, no placeholders, the same
// kind of statement, no sign of a credential.
func recentExecution(ctx context.Context, conn check.Conn, major int, queryID int64, keyword string) (string, error) {
	if major < 14 {
		return "", nil
	}
	rows, err := conn.Query(ctx, recentExecutionLookup, queryID)
	if err != nil {
		return "", fmt.Errorf("look up a recent execution: %w", err)
	}
	defer rows.Close()
	var found string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return "", err
		}
		statement, ok := singleStatement(text)
		if found != "" || !ok || hasParameterPlaceholders(statement) || firstSQLKeyword(statement) != keyword || redact.MayHoldCredential(statement) {
			continue
		}
		found = statement
	}
	return found, rows.Err()
}

// boundQuerier runs one statement over the extended protocol with its
// parameter values in text form and their types left to the server, as psql
// would read the same constants. Tests provide it directly.
type boundQuerier interface {
	QueryBound(ctx context.Context, sql string, values [][]byte) (json.RawMessage, error)
}

type pgxBoundQuerier struct{ conn *pgx.Conn }

func (q pgxBoundQuerier) QueryBound(ctx context.Context, sql string, values [][]byte) (json.RawMessage, error) {
	result := q.conn.PgConn().ExecParams(ctx, sql, values, nil, nil, nil).Read()
	if result.Err != nil {
		return nil, result.Err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
		return nil, errors.New("EXPLAIN returned an unexpected result shape")
	}
	return json.RawMessage(append([]byte(nil), result.Rows[0][0]...)), nil
}

// explainBound sends an EXPLAIN with the values the operator gave for the
// statement's placeholders. A nil value is SQL NULL.
func explainBound(ctx context.Context, conn check.Conn, sql string, params []*string) (json.RawMessage, error) {
	var querier boundQuerier
	switch c := conn.(type) {
	case boundQuerier:
		querier = c
	case interface{ Conn() *pgx.Conn }:
		querier = pgxBoundQuerier{conn: c.Conn()}
	default:
		return nil, fmt.Errorf("connection %T cannot bind parameter values", conn)
	}
	values := make([][]byte, len(params))
	for i, value := range params {
		if value != nil {
			values[i] = []byte(*value)
		}
	}
	return querier.QueryBound(ctx, sql, values)
}

// readOnlyExplainKeyword lists the statements EXPLAIN ANALYZE may execute. A
// WITH may still hide a data-modifying CTE; the READ ONLY transaction rejects
// that one inside PostgreSQL.
func readOnlyExplainKeyword(keyword string) bool {
	switch keyword {
	case "SELECT", "VALUES", "WITH", "TABLE":
		return true
	default:
		return false
	}
}

func errorsIsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// statementLookup reads the text pg_stat_statements recorded for a queryid in
// one database. Without the dbid filter a queryid shared by two databases
// (catalog queries, the same schema deployed twice) could return the other
// database's text, which then failed — or explained something else — in this
// one. The first %s is the view's schema-qualified name, the second names
// that database.
const statementLookup = `SELECT query FROM %s
		WHERE queryid = $1
		  AND dbid = (SELECT oid FROM pg_database WHERE datname = %s)
		  AND query !~ '^[[:space:]]*EXPLAIN'
		LIMIT 1`

// lookupStatement reads the statement text for queryid in the database the
// command targets. CREATE EXTENSION creates the pg_stat_statements view in
// one database only, while the statistics are cluster-wide: the scheduled
// collection reads a database without the view through another database's
// (check.StatementsConn), so its statements are listed, and explaining one of
// them used to fail on the missing view. The text is read the same way here;
// the plan is still made on conn, in the command's own database.
func lookupStatement(ctx context.Context, target check.Target, conn check.Conn, datname string, queryID int64) (string, error) {
	statement, err := readStatementText(ctx, conn, "current_database()", queryID)
	if datname == "" || !isUndefinedTable(err) {
		return statement, err
	}
	source, err := check.StatementsConn(ctx, target)
	if err != nil {
		return "", err
	}
	defer source.Release()
	return readStatementText(ctx, source, "$2", queryID, datname)
}

// readStatementText runs statementLookup on conn; database is the SQL
// expression naming the database, args its parameters after the queryid.
func readStatementText(ctx context.Context, conn check.Conn, database string, args ...any) (string, error) {
	schema, err := check.ExtensionSchema(ctx, conn, "pg_stat_statements")
	if err != nil {
		return "", err
	}
	view := pgx.Identifier{schema, "pg_stat_statements"}.Sanitize()
	var statement string
	err = conn.QueryRow(ctx, fmt.Sprintf(statementLookup, view, database), args...).Scan(&statement)
	return statement, err
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}

func isInsufficientPrivilege(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func allowedExplainKeyword(keyword string) bool {
	switch keyword {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "VALUES", "WITH", "TABLE":
		return true
	default:
		return false
	}
}

// firstSQLKeyword skips leading whitespace and comments the way PostgreSQL's
// lexer does, so the keyword it returns is the one the server executes. The
// gate is only as good as that agreement: a comment that ends early here lets
// a write read as a SELECT.
func firstSQLKeyword(statement string) string {
	for {
		statement = strings.TrimLeft(statement, " \t\r\n")
		switch {
		case strings.HasPrefix(statement, "--"):
			// A carriage return ends a line comment too, not only a newline.
			if end := strings.IndexAny(statement, "\r\n"); end >= 0 {
				statement = statement[end+1:]
				continue
			}
			return ""
		case strings.HasPrefix(statement, "/*"):
			end := blockCommentEnd(statement)
			if end < 0 {
				return ""
			}
			statement = statement[end:]
			continue
		}
		break
	}
	end := 0
	for end < len(statement) && ((statement[end] >= 'A' && statement[end] <= 'Z') || (statement[end] >= 'a' && statement[end] <= 'z')) {
		end++
	}
	return strings.ToUpper(statement[:end])
}

// blockCommentEnd returns the offset just past the block comment that opens
// statement, or -1 when the comment is never closed. PostgreSQL nests block
// comments, so a "*/" closes only the innermost one: in
// "/* /* x */ SELECT 1 */ DELETE" the comment ends after the second "*/".
func blockCommentEnd(statement string) int {
	depth := 0
	for i := 0; i+1 < len(statement); {
		switch statement[i : i+2] {
		case "/*":
			depth++
			i += 2
		case "*/":
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return -1
}

// PlanHash is the structural hash of a FORMAT JSON plan, the same one the
// EXPLAIN command stores (plan 007 phase 7).
func PlanHash(plan json.RawMessage) (string, error) { return planHash(plan) }

func planHash(plan json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(plan, &value); err != nil {
		return "", err
	}
	normalized := structuralPlanFields(value)
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// structuralPlanKeys are the keys of a FORMAT JSON plan that describe its
// shape. The hash keeps only these, not everything but a list of runtime
// keys: EXPLAIN ANALYZE with BUFFERS adds timings, buffer counters, planning
// and execution time, trigger and I/O figures, and their names change across
// major versions, so a denylist misses some on every release. Estimates
// (costs, row counts and widths) are left out too, so that the hash moves when
// the plan changes shape, not when the statistics behind it do.
var structuralPlanKeys = map[string]bool{
	"Plan":                true,
	"Plans":               true,
	"Node Type":           true,
	"Parent Relationship": true,
	"Parallel Aware":      true,
	"Async Capable":       true,
	"Join Type":           true,
	"Partial Mode":        true,
	"Strategy":            true,
	"Scan Direction":      true,
	"Command":             true,
	"Operation":           true,
	"Relation Name":       true,
	"Schema":              true,
	"Alias":               true,
	"Index Name":          true,
	"Function Name":       true,
	"CTE Name":            true,
	"Subplan Name":        true,
	"Filter":              true,
	"Index Cond":          true,
	"Recheck Cond":        true,
	"Hash Cond":           true,
	"Merge Cond":          true,
	"Join Filter":         true,
	"One-Time Filter":     true,
	"Sort Key":            true,
	"Group Key":           true,
	"Output":              true,
	"Workers Planned":     true,
	"Inner Unique":        true,
}

func structuralPlanFields(value any) any {
	switch typed := value.(type) {
	case []any:
		for i := range typed {
			typed[i] = structuralPlanFields(typed[i])
		}
	case map[string]any:
		for key, child := range typed {
			if !structuralPlanKeys[key] {
				delete(typed, key)
				continue
			}
			typed[key] = structuralPlanFields(child)
		}
	}
	return value
}

// hintEnableSQL reads whether pg_hint_plan is loaded (NULL: the setting does
// not exist) and enabled. The agent never sends LOAD: a role that is not
// superuser cannot, so hints work only where the server preloads the library.
const hintEnableSQL = "SELECT current_setting('pg_hint_plan.enable_hint', true)"

// setConfigSQL applies one planner setting local to the transaction; name and
// value are bound, never spliced into SQL text.
const setConfigSQL = "SELECT set_config($1, $2, true)"

// applyWorkbook checks that hints can take effect and applies the variant's
// settings in name order. It runs inside the explain's read-only transaction,
// which ends with ROLLBACK, so nothing outlives the command.
func applyWorkbook(ctx context.Context, conn check.Conn, args command.Args) error {
	if err := command.ValidateSettings(args.Settings); err != nil {
		return rejectedf("%v", err)
	}
	if err := command.ValidateHints(args.Hints); err != nil {
		return rejectedf("%v", err)
	}
	if args.Hints != "" {
		var enabled *string
		if err := conn.QueryRow(ctx, hintEnableSQL).Scan(&enabled); err != nil {
			return err
		}
		switch {
		case enabled == nil:
			return rejectedf("hint_plan_inactive: pg_hint_plan is not loaded on this server; add it to shared_preload_libraries to use hints")
		case *enabled == "off":
			return rejectedf("hint_plan_disabled: pg_hint_plan.enable_hint is off on this server")
		}
	}
	names := make([]string, 0, len(args.Settings))
	for name := range args.Settings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var applied string
		if err := conn.QueryRow(ctx, setConfigSQL, name, command.SettingValue(name, args.Settings[name])).Scan(&applied); err != nil {
			return rejectedf("setting %s could not be applied: %v", name, err)
		}
	}
	return nil
}

// withHints places the pg_hint_plan comment immediately before the statement
// text. ValidateHints keeps * and / out of the hints, so the comment cannot
// be closed early.
func withHints(statement, hints string) string {
	if hints == "" {
		return statement
	}
	return "/*+ " + hints + " */ " + statement
}
