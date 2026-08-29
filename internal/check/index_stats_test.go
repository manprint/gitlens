package check

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestIndexStats_MetricNamesMatchColumns(t *testing.T) {
	r := indexStatsRow{Schema: "public", Relation: "orders", Index: "orders_a_idx", Definition: "CREATE INDEX orders_a_idx ON public.orders USING btree (a)"}
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.orders_a_idx"}}, map[string]indexStatsRow{"public.orders_a_idx": r})
	names := map[string]bool{}
	for _, m := range result.Metrics {
		names[m.Name] = true
	}
	for _, n := range []string{"idx_scan", "idx_tup_read", "idx_tup_fetch", "idx_blks_read", "idx_blks_hit", "index_bytes", "is_unique", "is_primary", "is_valid"} {
		require.True(t, names["pg_index_"+n])
	}
}

func TestIndexStats_EmitsIndexDefFact(t *testing.T) {
	result := indexStatsResult([]cardinality.Candidate{{Key: "public.i"}}, map[string]indexStatsRow{"public.i": {Schema: "public", Relation: "t", Index: "i", Definition: "CREATE INDEX i ON public.t (a)"}})
	require.Len(t, result.Facts, 1)
	require.Equal(t, "index_def", result.Facts[0].Kind)
	require.Equal(t, "CREATE INDEX i ON public.t (a)", result.Facts[0].ValueText)
	require.NotEmpty(t, result.Facts[0].Labels["def_hash"])
}

func TestIndexStats_DefHashIgnoresIndexName(t *testing.T) {
	require.Equal(t, definitionHash("CREATE INDEX one ON t (a)"), definitionHash("CREATE INDEX two ON t (a)"))
}
func TestIndexStats_DefHashDistinguishesColumnOrder(t *testing.T) {
	require.NotEqual(t, definitionHash("CREATE INDEX i ON t (a,b)"), definitionHash("CREATE INDEX i ON t (b,a)"))
}
func TestIndexStats_DefHashIsWhitespaceInsensitive(t *testing.T) {
	require.Equal(t, definitionHash("CREATE INDEX i ON t (a,  b)"), definitionHash("create index x on t (a, b)"))
}
func TestIndexStats_BooleanColumnsAreZeroOrOne(t *testing.T) {
	require.Equal(t, float64(1), boolFloat(true))
	require.Equal(t, float64(0), boolFloat(false))
	require.Equal(t, pgtype.KindGauge, indexStatsResult([]cardinality.Candidate{{Key: "k"}}, map[string]indexStatsRow{"k": {}}).Metrics[5].Kind)
}
func TestIndexStats_RequiresDatabaseScope(t *testing.T) {
	c := &indexStatsCheck{selector: cardinality.NewSelector(cardinality.Options{TopN: 50})}
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, 5*time.Minute, c.DefaultInterval())
	require.Equal(t, 30*time.Second, c.Timeout())
}
