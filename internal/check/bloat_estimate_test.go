package check

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
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
	require.Equal(t, 6*time.Hour, (&bloatEstimateCheck{selector: cardinality.NewSelector(cardinality.Options{TopN: 1})}).DefaultInterval())
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

func bloatRowsResult(rows []bloatRow, now time.Time) Result {
	by := map[string]bloatRow{}
	c := []cardinality.Candidate{}
	for _, r := range rows {
		if r.Real < 1024*1024 || r.Tuples < 0 {
			continue
		}
		k := r.Schema + "." + r.Relation
		by[k] = r
		c = append(c, cardinality.Candidate{Key: k, Primary: r.Bloat})
	}
	return bloatResult(c, by)
}
