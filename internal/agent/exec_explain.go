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
	// one place that executes text written by database users. A read-only
	// transaction makes PostgreSQL itself reject INSERT/UPDATE/DELETE/MERGE
	// under ANALYZE, data-modifying CTEs, nextval() and writes from volatile
	// functions. Plan-only EXPLAIN executes nothing; PostgreSQL still checks
	// the statement's privileges, so a write cannot even be planned by a
	// role that only reads.
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
	// Planning alone can run user code (see explain_guard.go), so privileges
	// are dropped before anything is planned, and ANALYZE runs only once the
	// guard has inspected the statement, the database and the plan.
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
	hash, err := planHash(plan)
	if err != nil {
		return nil, fmt.Errorf("hash explain plan: %w", err)
	}
	result, err := json.Marshal(struct {
		Plan     json.RawMessage `json:"plan"`
		PlanHash string          `json:"plan_hash"`
	}{Plan: plan, PlanHash: hash})
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

func firstSQLKeyword(statement string) string {
	for {
		statement = strings.TrimLeft(statement, " \t\r\n")
		switch {
		case strings.HasPrefix(statement, "--"):
			if newline := strings.IndexByte(statement, '\n'); newline >= 0 {
				statement = statement[newline+1:]
				continue
			}
			return ""
		case strings.HasPrefix(statement, "/*"):
			if end := strings.Index(statement[2:], "*/"); end >= 0 {
				statement = statement[end+4:]
				continue
			}
			return ""
		}
		break
	}
	end := 0
	for end < len(statement) && ((statement[end] >= 'A' && statement[end] <= 'Z') || (statement[end] >= 'a' && statement[end] <= 'z')) {
		end++
	}
	return strings.ToUpper(statement[:end])
}

func planHash(plan json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(plan, &value); err != nil {
		return "", err
	}
	normalized := stripPlanRuntimeFields(value)
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func stripPlanRuntimeFields(value any) any {
	switch typed := value.(type) {
	case []any:
		for i := range typed {
			typed[i] = stripPlanRuntimeFields(typed[i])
		}
	case map[string]any:
		for key, child := range typed {
			if key == "Actual Rows" || key == "Actual Total Time" || key == "Actual Startup Time" || key == "Actual Loops" || strings.HasPrefix(key, "Buffers") {
				delete(typed, key)
				continue
			}
			typed[key] = stripPlanRuntimeFields(child)
		}
	}
	return value
}
