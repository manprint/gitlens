package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/redact"
)

// Error codes of an index_check command. Each refusal starts with its code
// and a colon, so the console can explain it in the operator's language.
const (
	indexCheckHypopgMissing        = "hypopg_missing"
	indexCheckStatementUnavailable = "statement_unavailable"
	indexCheckRelationNotFound     = "relation_not_found"
	indexCheckColumnNotFound       = "column_not_found"
)

// IndexCheckExecutor plans the statement of a queryid with and without a
// hypothetical index (hypopg) and reports whether the planner would use it.
// Nothing is created on the database: a hypopg index lives in the backend's
// memory only, and the session is reset, rolled back and closed at the end.
type IndexCheckExecutor struct{}

func NewIndexCheckExecutor() *IndexCheckExecutor { return &IndexCheckExecutor{} }

func (e *IndexCheckExecutor) Kind() command.Kind { return command.KindIndexCheck }

type indexCheckResult struct {
	Datname        string          `json:"datname"`
	IndexName      string          `json:"index_name"`
	IndexSizeBytes int64           `json:"index_size_bytes"`
	CostBefore     float64         `json:"cost_before"`
	CostAfter      float64         `json:"cost_after"`
	IndexUsed      bool            `json:"index_used"`
	PlanAfter      json.RawMessage `json:"plan_after"`
}

func (e *IndexCheckExecutor) Execute(ctx context.Context, target check.Target, args command.Args) (json.RawMessage, error) {
	if err := args.Validate(command.KindIndexCheck); err != nil {
		return nil, rejectedf("%v", err)
	}
	queryID := *args.QueryID
	conn, err := target.ConnFor(ctx, args.Datname)
	if err != nil {
		return nil, err
	}
	// The hypothetical index belongs to the session: closing it is the last
	// guarantee that nothing outlives the command.
	defer discardConn(ctx, conn)

	hypopgSchema, err := check.ExtensionSchema(ctx, conn, "hypopg")
	if isUndefinedTable(err) {
		return nil, rejectedf("%s: the hypopg extension is not installed in database %q; pglens does not install it", indexCheckHypopgMissing, args.Datname)
	}
	if err != nil {
		return nil, err
	}

	statement, err := lookupStatement(ctx, target, conn, args.Datname, queryID)
	if errorsIsNoRows(err) || isUndefinedTable(err) {
		return nil, rejectedf("%s: no statement text for queryid %d in database %q", indexCheckStatementUnavailable, queryID, args.Datname)
	}
	if err != nil {
		return nil, err
	}
	statement, ok := singleStatement(statement)
	if !ok {
		return nil, rejectedf("statement for queryid %d is not a single statement", queryID)
	}
	if redact.MayHoldCredential(statement) {
		return nil, rejectedf("statement for queryid %d may hold a credential; pglens does not explain it", queryID)
	}
	keyword := firstSQLKeyword(statement)
	if !allowedExplainKeyword(keyword) {
		return nil, rejectedf("statement keyword %q is not safe to explain", keyword)
	}
	major := target.PGVersion().Major()
	plan, err := indexCheckPlanner(ctx, conn, major, queryID, statement, keyword)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Exec(ctx, "BEGIN READ ONLY"); err != nil {
		return nil, err
	}
	resetSQL := fmt.Sprintf("SELECT %s()", pgx.Identifier{hypopgSchema, "hypopg_reset"}.Sanitize())
	reset := false
	defer func() {
		if !reset {
			_, _ = conn.Exec(ctx, "ROLLBACK")
			return
		}
		// In a failed transaction the reset is refused; it runs again once the
		// transaction is gone. discardConn closes the session in any case.
		if _, err := conn.Exec(ctx, resetSQL); err != nil {
			_, _ = conn.Exec(ctx, "ROLLBACK")
			_, _ = conn.Exec(ctx, resetSQL)
			return
		}
		_, _ = conn.Exec(ctx, "ROLLBACK")
	}()
	for _, sql := range []string{"SET LOCAL statement_timeout = '15s'", "SET LOCAL search_path TO DEFAULT"} {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return nil, err
		}
	}
	if err := guardPlanning(ctx, conn, newCodeGraph(conn, major)); err != nil {
		return nil, err
	}
	if err := dropExplainPrivileges(ctx, conn, major, false); err != nil {
		return nil, err
	}

	reset = true
	if _, err := conn.Exec(ctx, resetSQL); err != nil {
		return nil, err
	}
	before, err := plan(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkIndexColumns(ctx, conn, args); err != nil {
		return nil, err
	}
	var indexOID uint32
	var indexName string
	createSQL := fmt.Sprintf("SELECT indexrelid, indexname FROM %s($1)", pgx.Identifier{hypopgSchema, "hypopg_create_index"}.Sanitize())
	if err := conn.QueryRow(ctx, createSQL, indexDDL(args)).Scan(&indexOID, &indexName); err != nil {
		return nil, fmt.Errorf("hypopg_create_index: %w", err)
	}
	var size int64
	sizeSQL := fmt.Sprintf("SELECT %s($1)", pgx.Identifier{hypopgSchema, "hypopg_relation_size"}.Sanitize())
	if err := conn.QueryRow(ctx, sizeSQL, indexOID).Scan(&size); err != nil {
		return nil, fmt.Errorf("hypopg_relation_size: %w", err)
	}
	after, err := plan(ctx)
	if err != nil {
		return nil, err
	}
	if redact.MayHoldCredential(string(before)) || redact.MayHoldCredential(string(after)) {
		return nil, rejectedf("the plan for queryid %d may hold a credential; pglens does not ship it", queryID)
	}
	costBefore, err := rootTotalCost(before)
	if err != nil {
		return nil, err
	}
	costAfter, err := rootTotalCost(after)
	if err != nil {
		return nil, err
	}
	var datname string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&datname); err != nil {
		return nil, err
	}
	return json.Marshal(indexCheckResult{
		Datname:        datname,
		IndexName:      indexName,
		IndexSizeBytes: size,
		CostBefore:     costBefore,
		CostAfter:      costAfter,
		IndexUsed:      planUsesIndex(after, indexName),
		PlanAfter:      after,
	})
}

// indexCheckPlanner returns how the statement is planned, exactly as a
// plan-only explain plans it: placeholders take GENERIC_PLAN on PostgreSQL 16+
// and the constants of a recent execution before that.
func indexCheckPlanner(ctx context.Context, conn check.Conn, major int, queryID int64, statement, keyword string) (func(context.Context) (json.RawMessage, error), error) {
	options := "FORMAT JSON, VERBOSE, COSTS"
	generic := false
	if hasParameterPlaceholders(statement) {
		if major < 16 {
			sample, err := recentExecution(ctx, conn, major, queryID, keyword)
			if err != nil {
				return nil, err
			}
			if sample == "" {
				return nil, rejectedf("%s: the statement has parameters and PostgreSQL before 16 has no GENERIC_PLAN; run it while the application runs the statement", indexCheckStatementUnavailable)
			}
			statement = sample
		} else {
			options += ", GENERIC_PLAN"
			generic = true
		}
	}
	sql := fmt.Sprintf("EXPLAIN (%s) %s", options, statement)
	return func(ctx context.Context) (json.RawMessage, error) {
		var plan json.RawMessage
		var err error
		if generic {
			plan, err = explainVerbatim(ctx, conn, sql)
		} else {
			err = conn.QueryRow(ctx, sql).Scan(&plan)
		}
		if err != nil {
			return nil, rejectedf("normalised statement cannot be explained: %v", err)
		}
		return plan, nil
	}, nil
}

const indexColumnsQuery = `SELECT a.attname::text FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped`

func checkIndexColumns(ctx context.Context, conn check.Conn, args command.Args) error {
	rows, err := conn.Query(ctx, indexColumnsQuery, args.IndexSchema, args.IndexRelation)
	if err != nil {
		return err
	}
	defer rows.Close()
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(present) == 0 {
		return rejectedf("%s: table %s does not exist in database %q", indexCheckRelationNotFound, pgx.Identifier{args.IndexSchema, args.IndexRelation}.Sanitize(), args.Datname)
	}
	for _, name := range append(append([]string{}, args.IndexColumns...), args.IndexInclude...) {
		if !present[name] {
			return rejectedf("%s: table %s has no column %s", indexCheckColumnNotFound, pgx.Identifier{args.IndexSchema, args.IndexRelation}.Sanitize(), pgx.Identifier{name}.Sanitize())
		}
	}
	return nil
}

// indexDDL writes the hypothetical index. Every name is quoted as an
// identifier, so a name is never read as SQL.
func indexDDL(args command.Args) string {
	quote := func(names []string) string {
		out := make([]string, len(names))
		for i, name := range names {
			out[i] = pgx.Identifier{name}.Sanitize()
		}
		return strings.Join(out, ", ")
	}
	ddl := fmt.Sprintf("CREATE INDEX ON %s USING btree (%s)", pgx.Identifier{args.IndexSchema, args.IndexRelation}.Sanitize(), quote(args.IndexColumns))
	if len(args.IndexInclude) > 0 {
		ddl += fmt.Sprintf(" INCLUDE (%s)", quote(args.IndexInclude))
	}
	return ddl
}

func rootTotalCost(plan json.RawMessage) (float64, error) {
	var doc []struct {
		Plan struct {
			TotalCost *float64 `json:"Total Cost"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &doc); err != nil || len(doc) == 0 || doc[0].Plan.TotalCost == nil {
		return 0, fmt.Errorf("plan has no root Total Cost")
	}
	return *doc[0].Plan.TotalCost, nil
}

// planUsesIndex reports whether any node of the plan reads the index.
func planUsesIndex(plan json.RawMessage, indexName string) bool {
	var doc any
	if indexName == "" || json.Unmarshal(plan, &doc) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch node := v.(type) {
		case []any:
			for _, child := range node {
				if walk(child) {
					return true
				}
			}
		case map[string]any:
			if name, ok := node["Index Name"].(string); ok && name == indexName {
				return true
			}
			for _, key := range []string{"Plan", "Plans"} {
				if child, ok := node[key]; ok && walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(doc)
}
