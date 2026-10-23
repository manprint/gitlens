package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
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
	if check.MayHoldCredential(statement) {
		return nil, rejectedf("statement for queryid %d may hold a credential; pglens does not explain it", *args.QueryID)
	}
	keyword := firstSQLKeyword(statement)
	if !allowedExplainKeyword(keyword) {
		return nil, rejectedf("statement keyword %q is not safe to explain", keyword)
	}
	hasParams := hasParameterPlaceholders(statement)
	if args.Analyze {
		// ANALYZE executes the statement. pglens never modifies a monitored
		// database, so only statements that read are eligible; the READ ONLY
		// transaction below is the enforcement, this is the clear refusal.
		if !readOnlyExplainKeyword(keyword) {
			return nil, rejectedf("EXPLAIN ANALYZE would execute a %s statement; only read-only statements can be analyzed", keyword)
		}
		if hasParams {
			return nil, rejectedf("normalised statement has parameter placeholders; EXPLAIN ANALYZE needs concrete values")
		}
	}

	options := "FORMAT JSON, VERBOSE, COSTS"
	if args.Analyze {
		options = "ANALYZE, FORMAT JSON, VERBOSE, COSTS, BUFFERS"
	} else if hasParams {
		// pg_stat_statements replaces every constant with $n, so nearly every
		// normalised statement has placeholders and plain EXPLAIN rejects it
		// with "there is no parameter $1". GENERIC_PLAN (PostgreSQL 16+) plans
		// it without values — the only way such a statement can be explained.
		if target.PGVersion().Major() < 16 {
			return nil, rejectedf("normalised statement has parameter placeholders; explaining it requires PostgreSQL 16 or later (GENERIC_PLAN)")
		}
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
	// Planning alone can run user code (see explain_guard.go), so the database
	// is checked before anything is planned, privileges are dropped next, and
	// ANALYZE runs only once the guard has inspected the statement and the plan.
	if err := guardPlanning(ctx, conn); err != nil {
		return nil, err
	}
	if err := dropExplainPrivileges(ctx, conn, target.PGVersion().Major(), args.Analyze); err != nil {
		return nil, err
	}
	if args.Analyze {
		if err := guardAnalyze(ctx, conn, statement); err != nil {
			return nil, err
		}
	}

	explainSQL := fmt.Sprintf("EXPLAIN (%s) %s", options, statement)
	var plan json.RawMessage
	var scanErr error
	if hasParams {
		// A statement with placeholders cannot go through the extended
		// protocol with no arguments (the server would expect a value per
		// placeholder); GENERIC_PLAN is designed for the simple protocol.
		// singleStatement above guarantees there is nothing after it.
		plan, scanErr = explainVerbatim(ctx, conn, explainSQL)
	} else {
		scanErr = conn.QueryRow(ctx, explainSQL).Scan(&plan)
	}
	if scanErr != nil {
		if !readOnlyExplainKeyword(keyword) && isInsufficientPrivilege(scanErr) {
			// PostgreSQL checks the privileges a statement needs even when
			// it only plans it, and the monitoring role holds none that write.
			return nil, rejectedf("explaining a %s statement needs the privilege to run it, which the read-only monitoring role does not hold: %v", keyword, scanErr)
		}
		if !args.Analyze {
			return nil, rejectedf("normalised statement cannot be explained: %v", scanErr)
		}
		return nil, scanErr
	}
	// The plan also shows the constants of the views and functions the planner
	// expanded, which the statement text does not: a view over dblink shows
	// its connection string. A plan with any sign of a credential is withheld.
	if check.MayHoldCredential(string(plan)) {
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
	result, err := json.Marshal(struct {
		Plan     json.RawMessage `json:"plan"`
		PlanHash string          `json:"plan_hash"`
		Datname  string          `json:"datname"`
	}{Plan: plan, PlanHash: hash, Datname: datname})
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

// explainVerbatim sends an EXPLAIN whose statement keeps its $n placeholders,
// which is what GENERIC_PLAN plans. pgx's QueryExecModeSimpleProtocol cannot
// carry it: that mode substitutes the placeholders on the client and fails
// with "insufficient arguments" when there are no values, so the statement
// goes to the lower-level connection untouched.
func explainVerbatim(ctx context.Context, conn check.Conn, sql string) (json.RawMessage, error) {
	var querier simpleQuerier
	switch c := conn.(type) {
	case simpleQuerier:
		querier = c
	case interface{ Conn() *pgx.Conn }:
		querier = pgxSimpleQuerier{conn: c.Conn()}
	default:
		return nil, fmt.Errorf("connection %T cannot send a statement verbatim", conn)
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
