package check

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestTableStats_MetricNamesMatchColumns(t *testing.T) {
	r := tableStatsRow{Schema: "public", Relation: "orders", LastVacuum: timePtr(time.Unix(0, 0))}
	result := tableStatsResult([]cardinality.Candidate{{Key: "public.orders"}}, map[string]tableStatsRow{"public.orders": r}, time.Unix(10, 0))
	names := make(map[string]bool)
	for _, m := range result.Metrics {
		names[m.Name] = true
	}
	for _, name := range []string{"seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch", "n_tup_ins", "n_tup_upd", "n_tup_del", "n_tup_hot_upd", "n_live_tup", "n_dead_tup", "n_mod_since_analyze", "autovacuum_count", "autoanalyze_count", "relpages", "reltuples", "relfrozenxid_age", "total_bytes", "table_bytes", "toast_bytes", "last_vacuum_age_seconds"} {
		require.True(t, names["pg_table_"+name], name)
	}
}

func TestTableStats_NullLastAutovacuumEmitsNothing(t *testing.T) {
	r := tableStatsRow{Schema: "public", Relation: "fresh"}
	result := tableStatsResult([]cardinality.Candidate{{Key: "public.fresh"}}, map[string]tableStatsRow{"public.fresh": r}, time.Now())
	for _, m := range result.Metrics {
		require.NotEqual(t, "pg_table_last_autovacuum_age_seconds", m.Name)
	}
}

func TestTableStats_EmptyTableEmitsZeroDeadTupleRatio(t *testing.T) {
	r := tableStatsRow{Schema: "public", Relation: "empty"}
	result := tableStatsResult([]cardinality.Candidate{{Key: "public.empty"}}, map[string]tableStatsRow{"public.empty": r}, time.Now())
	require.Equal(t, 0.0, metricValue(result, "pg_table_dead_tuple_ratio", map[string]string{"schemaname": "public", "relname": "empty"}))
}

func TestTableStats_CounterGaugeClassification(t *testing.T) {
	r := tableStatsRow{Schema: "public", Relation: "orders", SeqScan: 1, NLiveTup: 2}
	result := tableStatsResult([]cardinality.Candidate{{Key: "public.orders"}}, map[string]tableStatsRow{"public.orders": r}, time.Now())
	kinds := map[string]pgtype.MetricKind{}
	for _, m := range result.Metrics {
		kinds[m.Name] = m.Kind
	}
	require.Equal(t, pgtype.KindCounter, kinds["pg_table_seq_scan"])
	require.Equal(t, pgtype.KindGauge, kinds["pg_table_n_live_tup"])
}

func TestTableStats_RequiresDatabaseScope(t *testing.T) {
	c := &tableStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 5*time.Minute, c.DefaultInterval())
	require.Equal(t, 30*time.Second, c.Timeout())
}

func TestTableStats_TruncatedFlagSetWhenSelectorTruncates(t *testing.T) {
	c := cardinality.NewSelector(cardinality.Options{TopN: 3, MaxKeys: 2})
	candidates := []cardinality.Candidate{{Key: "a", Primary: 3}, {Key: "b", Primary: 2}, {Key: "c", Primary: 1}}
	selected, truncated := c.Select(1, candidates)
	result := tableStatsResult(selected, map[string]tableStatsRow{"a": {}, "b": {}, "c": {}}, time.Now())
	result.Truncated = truncated
	require.True(t, result.Truncated)
}

func timePtr(t time.Time) *time.Time { return &t }
