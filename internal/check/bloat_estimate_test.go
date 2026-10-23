package check

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestBloat_SkipsSmallRelations(t *testing.T) {
	r := bloatRowsResult([]bloatRow{{Schema: "public", Relation: "small", Real: 100}}, time.Now())
	require.Empty(t, r.Metrics)
}
func TestBloat_SkipsUnanalysedRelations(t *testing.T) {
	r := bloatRowsResult([]bloatRow{{Schema: "public", Relation: "new", Real: 2 << 20, Tuples: -1}}, time.Now())
	require.Empty(t, r.Metrics)
}
func TestBloat_RatioIsZeroWhenNoBloat(t *testing.T) {
	r := bloatResult([]cardinality.Candidate{{Key: "public.t"}}, map[string]bloatRow{"public.t": {Schema: "public", Relation: "t", Kind: "table", Real: 10, Expected: 10}})
	for _, m := range r.Metrics {
		if m.Name == "pg_bloat_ratio" {
			require.Zero(t, m.Value)
		}
	}
}
func TestBloat_MethodIsEstimate(t *testing.T) {
	require.Contains(t, bloatEstimateSQL, "bloat_ratio")
	require.Contains(t, bloatEstimateSQL, "COALESCE")
	require.Equal(t, 6*time.Hour, (&bloatEstimateCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 1})}).DefaultInterval())
}
func TestBloat_SelectorOrdersByBloatBytes(t *testing.T) {
	s := cardinality.NewSelector(cardinality.Options{TopN: 1})
	selected, _ := s.Select(1, []cardinality.Candidate{{Key: "public.big", Primary: 20}, {Key: "public.small", Primary: 1}})
	r := bloatResult(selected, map[string]bloatRow{"public.big": {Schema: "public", Relation: "big", Bloat: 20}, "public.small": {Schema: "public", Relation: "small", Bloat: 1}})
	require.Equal(t, "big", r.Metrics[0].Labels["relname"])
}
func TestBloat_TimeoutIs120s(t *testing.T) {
	require.Equal(t, 120*time.Second, (&bloatEstimateCheck{}).Timeout())
}

// TestBloat_TopNIsConfigurable: checks.bloat_estimate.top_n reaches the
// selectors like the other relation checks' budgets do.
func TestBloat_TopNIsConfigurable(t *testing.T) {
	var c Check = &bloatEstimateCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	configurable, ok := c.(TopNConfigurable)
	require.True(t, ok, "bloat_estimate must honour top_n")
	configurable.SetTopN(7)
	require.Equal(t, 7, c.(*bloatEstimateCheck).selectors.TopN())
}

func bloatRowsResult(rows []bloatRow, now time.Time) Result {
	by := map[string]bloatRow{}
	c := []cardinality.Candidate{}
	for _, r := range rows {
		if r.Real < 1024*1024 || r.Tuples < 0 {
			continue
		}
		k := r.rowKey()
		by[k] = r
		c = append(c, cardinality.Candidate{Key: k, Primary: r.Bloat})
	}
	return bloatResult(c, by)
}

// TestBloat_UnreadableStatisticsArePartial: pg_stats hides a table from a role
// that cannot read its columns, so the estimate has no row for it. An empty
// result must not read as no bloat, so it is partial, and the count of hidden
// tables is reported rather than dropped.
func TestBloat_UnreadableStatisticsArePartial(t *testing.T) {
	t.Parallel()
	for _, unreadable := range []int64{0, 2} {
		conn := &mockConn{
			queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
				require.Equal(t, bloatUnreadableQuery, sql)
				return &mockRow{vals: []any{unreadable}}
			},
		}
		r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, unreadable, int64(metricValue(r, wire.BloatSkippedTablesMetric, map[string]string{})), "%d unreadable tables", unreadable)
		for _, m := range r.Metrics {
			require.Equal(t, wire.BloatSkippedTablesMetric, m.Name, "no estimate for a hidden table")
		}
		require.Equal(t, unreadable > 0, r.Partial, "%d unreadable tables", unreadable)
	}
}

// TestBloat_IndexRowsAreMeasuredByTheirOwnOID: an index row carries its
// table's name, and its exact size is read from the index's own oid. Keyed by
// table name, the index would overwrite its table, and its size would be the
// table's.
func TestBloat_IndexRowsAreMeasuredByTheirOwnOID(t *testing.T) {
	t.Parallel()
	table := []any{"public", "orders", "", "table", float64(4 << 20), float64(1 << 20), float64(1 << 20), 0.25, float64(100), uint32(10)}
	index := []any{"public", "orders", "orders_id_idx", "index", float64(4 << 20), float64(1 << 20), float64(1 << 20), 0.25, float64(100), uint32(20)}
	var probed []uint32
	conn := &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != bloatEstimateSQL {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{table, index}}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, args ...any) pgx.Row {
			switch sql {
			case bloatUnreadableQuery:
				return &mockRow{vals: []any{int64(0)}}
			case bloatSizeQuery:
				oid := args[0].(uint32)
				probed = append(probed, oid)
				return &mockRow{vals: []any{int64(3 << 20)}}
			}
			return &mockRow{vals: []any{int64(0)}}
		},
	}
	r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.ElementsMatch(t, []uint32{10, 20}, probed)
	require.Equal(t, float64(3<<20), metricValue(r, "pg_bloat_real_bytes", map[string]string{"schemaname": "public", "relname": "orders", "indexrelname": "orders_id_idx", "object_kind": "index", "method": "estimate"}))
	require.Equal(t, float64(3<<20), metricValue(r, "pg_bloat_real_bytes", map[string]string{"schemaname": "public", "relname": "orders", "indexrelname": "", "object_kind": "table", "method": "estimate"}))
	require.False(t, r.Partial)
}

// TestBloat_TablesAndIndexesHaveOwnBudgets: the top-N budget applies to each
// kind of row on its own, so a bloated index cannot take a table's place.
func TestBloat_TablesAndIndexesHaveOwnBudgets(t *testing.T) {
	t.Parallel()
	table := []any{"public", "t", "", "table", float64(2 << 20), float64(1 << 20), float64(1 << 20), 0.5, float64(100), uint32(1)}
	index := []any{"public", "t", "t_idx", "index", float64(2 << 20), float64(1 << 20), float64(1 << 20), 0.5, float64(100), uint32(2)}
	conn := &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != bloatEstimateSQL {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{table, index}}, nil
		},
		queryRowFunc: sizeProbe(bloatSizeQuery, 0, nil, []any{int64(2 << 20)}),
	}
	r, err := (&bloatEstimateCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 1})}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.NotZero(t, metricValue(r, "pg_bloat_bytes", map[string]string{"schemaname": "public", "relname": "t", "indexrelname": "", "object_kind": "table", "method": "estimate"}))
	require.NotZero(t, metricValue(r, "pg_bloat_bytes", map[string]string{"schemaname": "public", "relname": "t", "indexrelname": "t_idx", "object_kind": "index", "method": "estimate"}))
	require.False(t, r.Truncated)
}

// TestBloat_UnreadableTablesAreNamed: a positive count is followed by the
// names of the largest hidden tables, which the fact keeps for the readers.
func TestBloat_UnreadableTablesAreNamed(t *testing.T) {
	t.Parallel()
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			require.Equal(t, bloatUnreadableQuery, sql)
			return &mockRow{vals: []any{int64(2)}}
		},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != bloatUnreadableNamesQuery {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{{"app", "secret_big"}, {"public", "hidden"}}}, nil
		},
	}
	r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Len(t, r.Facts, 1)
	require.Equal(t, wire.BloatUnreadableFact, r.Facts[0].Kind)
	var value wire.BloatSkippedValue
	require.NoError(t, json.Unmarshal(r.Facts[0].ValueJSON, &value))
	require.Equal(t, int64(2), value.Count)
	require.Equal(t, []wire.BloatSkippedTable{{SchemaName: "app", RelName: "secret_big"}, {SchemaName: "public", RelName: "hidden"}}, value.Tables)
}

// TestBloat_UnreadableFactIsWrittenWhenNothingIsSkipped: a scrape that finds
// no hidden table still writes the fact, with count zero, so a database whose
// tables became readable does not keep its last count.
func TestBloat_UnreadableFactIsWrittenWhenNothingIsSkipped(t *testing.T) {
	t.Parallel()
	fact := bloatUnreadableFactFor(0, nil)
	require.Equal(t, wire.BloatUnreadableFact, fact.Kind)
	require.JSONEq(t, `{"count":0,"tables":[]}`, string(fact.ValueJSON))
}

func TestBloat_RowKeysKeepTablesAndIndexesApart(t *testing.T) {
	t.Parallel()
	table := bloatRow{Schema: "public", Relation: "orders", Kind: "table"}
	index := bloatRow{Schema: "public", Relation: "orders", Index: "orders_pkey", Kind: "index"}
	require.NotEqual(t, table.rowKey(), index.rowKey())
	require.Equal(t, "index public.orders_pkey", index.rowKey())
}
