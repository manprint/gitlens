package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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
	defer conn.Release()

	var statement string
	err = conn.QueryRow(ctx, `SELECT query FROM pg_stat_statements WHERE queryid = $1 AND query !~ '^[[:space:]]*EXPLAIN' LIMIT 1`, *args.QueryID).Scan(&statement)
	if errorsIsNoRows(err) {
		return nil, rejectedf("queryid %d is unknown on this instance", *args.QueryID)
	}
	if err != nil {
		return nil, err
	}
	if keyword := firstSQLKeyword(statement); !allowedExplainKeyword(keyword) {
		return nil, rejectedf("statement keyword %q is not safe to explain", keyword)
	}

	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(ctx, "ROLLBACK") }()
	if args.Analyze {
		if _, err := conn.Exec(ctx, "SET LOCAL statement_timeout = '30s'"); err != nil {
			return nil, err
		}
	}

	options := "FORMAT JSON, VERBOSE, COSTS"
	if args.Analyze {
		options = "ANALYZE, FORMAT JSON, VERBOSE, COSTS, BUFFERS"
	}
	explainSQL := fmt.Sprintf("EXPLAIN (%s) %s", options, statement)
	var plan json.RawMessage
	if err := conn.QueryRow(ctx, explainSQL).Scan(&plan); err != nil {
		if !args.Analyze {
			return nil, rejectedf("normalised statement cannot be explained: %v", err)
		}
		return nil, err
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

func errorsIsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func allowedExplainKeyword(keyword string) bool {
	switch keyword {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "VALUES", "WITH":
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
