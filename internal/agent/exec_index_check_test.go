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

// indexCheckConn records every statement in order and answers the hypopg
// ones; the explain guard's statements go to the embedded explainConn.
type indexCheckConn struct {
	explainConn
	noHypopg   bool
	columns    []string
	planBefore string
	planAfter  string
	createErr  error
	plans      int
	log        []string
	discarded  bool
	createArgs []any
}

func (c *indexCheckConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.log = append(c.log, sql)
	return c.explainConn.Exec(ctx, sql, args...)
}

func (c *indexCheckConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	c.log = append(c.log, sql)
	if strings.Contains(sql, "pg_attribute") {
		return &textRows{values: c.columns}, nil
	}
	return c.explainConn.Query(ctx, sql, args...)
}

func (c *indexCheckConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.log = append(c.log, sql)
	switch {
	case strings.HasPrefix(sql, "SELECT n.nspname") && len(args) == 1 && args[0] == "hypopg":
		if c.noHypopg {
			return valuesRow{err: pgx.ErrNoRows}
		}
		return valuesRow{values: []any{"public"}}
	case strings.Contains(sql, "hypopg_create_index"):
		c.createArgs = args
		if c.createErr != nil {
			return valuesRow{err: c.createErr}
		}
		return valuesRow{values: []any{uint32(13543), "<13543>btree_orders_customer_id"}}
	case strings.Contains(sql, "hypopg_relation_size"):
		return valuesRow{values: []any{int64(8192)}}
	case strings.HasPrefix(sql, "EXPLAIN (FORMAT JSON"):
		c.plans++
		if c.plans == 1 {
			return valuesRow{values: []any{json.RawMessage(c.planBefore)}}
		}
		return valuesRow{values: []any{json.RawMessage(c.planAfter)}}
	}
	return c.explainConn.QueryRow(ctx, sql, args...)
}

func (c *indexCheckConn) Release() { c.discarded = true }

type valuesRow struct {
	values []any
	err    error
}

func (r valuesRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch dst := d.(type) {
		case *string:
			*dst = r.values[i].(string)
		case *uint32:
			*dst = r.values[i].(uint32)
		case *int64:
			*dst = r.values[i].(int64)
		case *json.RawMessage:
			*dst = r.values[i].(json.RawMessage)
		default:
			panic("unsupported scan destination")
		}
	}
	return nil
}

const (
	seqScanPlan   = `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"orders","Total Cost":1693.0}}]`
	indexScanPlan = `[{"Plan":{"Node Type":"Index Scan","Index Name":"<13543>btree_orders_customer_id","Total Cost":8.3}}]`
)

func indexCheckCommand() command.Args {
	q := int64(77)
	return command.Args{QueryID: &q, Datname: "app", IndexSchema: "public", IndexRelation: "orders", IndexColumns: []string{"customer_id"}}
}

func newIndexCheckConn() *indexCheckConn {
	return &indexCheckConn{
		explainConn: explainConn{statement: "SELECT * FROM orders WHERE customer_id = 42"},
		columns:     []string{"id", "customer_id", "total"},
		planBefore:  seqScanPlan,
		planAfter:   indexScanPlan,
	}
}

func TestIndexCheck_ReportsTheHypotheticalIndex(t *testing.T) {
	conn := newIndexCheckConn()
	raw, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	require.NoError(t, err)
	var got indexCheckResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, explainDatname, got.Datname)
	require.Equal(t, "<13543>btree_orders_customer_id", got.IndexName)
	require.Equal(t, int64(8192), got.IndexSizeBytes)
	require.InDelta(t, 1693.0, got.CostBefore, 1e-9)
	require.InDelta(t, 8.3, got.CostAfter, 1e-9)
	require.True(t, got.IndexUsed)
	require.JSONEq(t, indexScanPlan, string(got.PlanAfter))
	require.Equal(t, []any{`CREATE INDEX ON "public"."orders" USING btree ("customer_id")`}, conn.createArgs)
	require.True(t, conn.discarded)
	require.Contains(t, conn.log, "BEGIN READ ONLY")
	for _, sql := range conn.log {
		if strings.HasPrefix(sql, "EXPLAIN") {
			require.NotContains(t, sql, "ANALYZE", "index_check never executes the statement")
		}
	}
}

// TestIDXV004: every name of the hypothetical index is quoted, so the DDL is
// one statement whatever the names hold.
func TestIDXV004(t *testing.T) {
	args := command.Args{IndexSchema: "public", IndexRelation: "orders", IndexColumns: []string{"customer_id", "created_at"}, IndexInclude: []string{"total"}}
	require.Equal(t, `CREATE INDEX ON "public"."orders" USING btree ("customer_id", "created_at") INCLUDE ("total")`, indexDDL(args))

	args = command.Args{IndexSchema: `we"ird`, IndexRelation: "orders", IndexColumns: []string{"id); DROP TABLE x; --", `a"b`}}
	ddl := indexDDL(args)
	require.Equal(t, `CREATE INDEX ON "we""ird"."orders" USING btree ("id); DROP TABLE x; --", "a""b")`, ddl)
	// Outside quoted identifiers the DDL has no semicolon and no comment.
	outside := stripQuotedIdentifiers(ddl)
	require.NotContains(t, outside, ";")
	require.NotContains(t, outside, "--")
	require.Equal(t, `CREATE INDEX ON . USING btree (, )`, outside)
}

func stripQuotedIdentifiers(sql string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(sql); i++ {
		if sql[i] == '"' {
			if quoted && i+1 < len(sql) && sql[i+1] == '"' {
				i++
				continue
			}
			quoted = !quoted
			continue
		}
		if !quoted {
			b.WriteByte(sql[i])
		}
	}
	return b.String()
}

// TestIDXV005: without hypopg the command stops at the extension check.
func TestIDXV005(t *testing.T) {
	conn := newIndexCheckConn()
	conn.noHypopg = true
	_, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	var rejected *RejectedError
	require.ErrorAs(t, err, &rejected)
	require.True(t, strings.HasPrefix(err.Error(), "hypopg_missing: "), err.Error())
	require.Contains(t, err.Error(), `"app"`)
	require.Len(t, conn.log, 1, "no statement after the extension check: %v", conn.log)
	require.True(t, conn.discarded)
}

// TestIDXV007: an error after the reset still resets, rolls back and
// discards the session, in that order.
func TestIDXV007(t *testing.T) {
	conn := newIndexCheckConn()
	conn.createErr = errors.New("boom")
	_, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	require.ErrorContains(t, err, "boom")
	require.True(t, conn.discarded)
	n := len(conn.log)
	require.GreaterOrEqual(t, n, 2)
	require.Equal(t, `SELECT "public"."hypopg_reset"()`, conn.log[n-2])
	require.Equal(t, "ROLLBACK", conn.log[n-1])
	resets := 0
	for _, sql := range conn.log {
		if strings.Contains(sql, "hypopg_reset") {
			resets++
		}
	}
	require.Equal(t, 2, resets, "one reset before planning, one at the end")
}

func TestIndexCheck_RefusesUnknownTableAndColumn(t *testing.T) {
	conn := newIndexCheckConn()
	conn.columns = nil
	_, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	require.ErrorContains(t, err, "relation_not_found: ")
	require.Equal(t, "ROLLBACK", conn.log[len(conn.log)-1])

	conn = newIndexCheckConn()
	args := indexCheckCommand()
	args.IndexInclude = []string{"missing"}
	_, err = NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, args)
	require.ErrorContains(t, err, `column_not_found: table "public"."orders" has no column "missing"`)
	for _, sql := range conn.log {
		require.NotContains(t, sql, "hypopg_create_index")
	}
}

func TestIndexCheck_UnknownQueryIDIsStatementUnavailable(t *testing.T) {
	conn := newIndexCheckConn()
	conn.lookupErr = pgx.ErrNoRows
	_, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	require.ErrorContains(t, err, "statement_unavailable: ")
}

// TestIDXV008: index_used is true only when the plan after reads the index
// hypopg returned.
func TestIDXV008(t *testing.T) {
	name := "<13543>btree_orders_customer_id"
	require.True(t, planUsesIndex(json.RawMessage(indexScanPlan), name))
	nested := `[{"Plan":{"Node Type":"Nested Loop","Plans":[{"Node Type":"Seq Scan"},{"Node Type":"Bitmap Heap Scan","Plans":[{"Node Type":"Bitmap Index Scan","Index Name":"<13543>btree_orders_customer_id"}]}]}}]`
	require.True(t, planUsesIndex(json.RawMessage(nested), name))
	other := `[{"Plan":{"Node Type":"Index Scan","Index Name":"orders_pkey","Total Cost":1200.5}}]`
	require.False(t, planUsesIndex(json.RawMessage(other), name))
	require.False(t, planUsesIndex(json.RawMessage(seqScanPlan), name))
	require.False(t, planUsesIndex(json.RawMessage(indexScanPlan), ""))

	conn := newIndexCheckConn()
	conn.planAfter = other
	raw, err := NewIndexCheckExecutor().Execute(context.Background(), &dispatcherTarget{conn: conn}, indexCheckCommand())
	require.NoError(t, err)
	var got indexCheckResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.False(t, got.IndexUsed)
}
