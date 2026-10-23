package cardinality_test

import (
	"math/rand"
	"testing"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/stretchr/testify/require"
)

func TestSelector_UnionOfBothRankings(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 2, MaxKeys: 10, Hysteresis: 5})
	// Key that is 1st by Secondary (calls) but far down by Primary (time)
	candidates := []cardinality.Candidate{
		{Key: "slow1", Primary: 100, Secondary: 1},
		{Key: "slow2", Primary: 90, Secondary: 2},
		{Key: "slow3", Primary: 80, Secondary: 3},
		{Key: "fast", Primary: 1, Secondary: 100}, // top by Secondary
	}
	kept, _ := sel.Select(1, candidates)
	keys := keysOf(kept)
	require.Contains(t, keys, "fast", "many-fast-queries case must be kept via Secondary ranking")
}

func TestSelector_DeterministicTieBreak(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 2, MaxKeys: 10, Hysteresis: 5})
	candidates := []cardinality.Candidate{
		{Key: "b", Primary: 10, Secondary: 1},
		{Key: "a", Primary: 10, Secondary: 2},
		{Key: "c", Primary: 10, Secondary: 3},
	}
	kept1, _ := sel.Select(1, candidates)
	// Shuffle input order
	shuffled := []cardinality.Candidate{
		{Key: "c", Primary: 10, Secondary: 3},
		{Key: "b", Primary: 10, Secondary: 1},
		{Key: "a", Primary: 10, Secondary: 2},
	}
	sel2 := cardinality.NewSelector(cardinality.Options{TopN: 2, MaxKeys: 10, Hysteresis: 5})
	kept2, _ := sel2.Select(1, shuffled)
	require.Equal(t, keysOf(kept1), keysOf(kept2), "deterministic ordering by Key")
}

func TestSelector_HysteresisRetains(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 2, MaxKeys: 10, Hysteresis: 5})
	// Cycle 1: include target key in topN
	c1 := []cardinality.Candidate{
		{Key: "keep", Primary: 100, Secondary: 100},
		{Key: "a", Primary: 90, Secondary: 90},
		{Key: "b", Primary: 80, Secondary: 80},
	}
	kept, _ := sel.Select(1, c1)
	require.Contains(t, keysOf(kept), "keep")
	// Cycles 2-6: keep is not in topN but should be retained via hysteresis
	for cycle := uint64(2); cycle <= 6; cycle++ {
		candidates := []cardinality.Candidate{
			{Key: "keep", Primary: 1, Secondary: 1}, // now low
			{Key: "a", Primary: 100, Secondary: 100},
			{Key: "b", Primary: 90, Secondary: 90},
			{Key: "c", Primary: 80, Secondary: 80},
			{Key: "d", Primary: 70, Secondary: 70},
		}
		kept, _ = sel.Select(cycle, candidates)
		require.Contains(t, keysOf(kept), "keep", "should retain in cycle %d", cycle)
	}
	// Cycle 7: should be dropped
	candidates := []cardinality.Candidate{
		{Key: "keep", Primary: 1, Secondary: 1},
		{Key: "a", Primary: 100, Secondary: 100},
		{Key: "b", Primary: 90, Secondary: 90},
		{Key: "c", Primary: 80, Secondary: 80},
		{Key: "d", Primary: 70, Secondary: 70},
	}
	kept, _ = sel.Select(7, candidates)
	require.NotContains(t, keysOf(kept), "keep", "should be dropped after hysteresis window")
}

func TestSelector_HysteresisDoesNotSelfRenew(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 1, MaxKeys: 10, Hysteresis: 5})
	// Cycle 1: fresh
	c1 := []cardinality.Candidate{
		{Key: "target", Primary: 100, Secondary: 100},
		{Key: "other", Primary: 90, Secondary: 90},
	}
	kept, _ := sel.Select(1, c1)
	require.Contains(t, keysOf(kept), "target")
	// Cycles 2-6: retained only, not fresh — should not renew
	for cycle := uint64(2); cycle <= 6; cycle++ {
		candidates := []cardinality.Candidate{
			{Key: "target", Primary: 1, Secondary: 1},
			{Key: "a", Primary: 100, Secondary: 100},
			{Key: "b", Primary: 90, Secondary: 90},
		}
		kept, _ = sel.Select(cycle, candidates)
		require.Contains(t, keysOf(kept), "target", "cycle %d", cycle)
	}
	// Cycle 7: must leave, proving no self-renew
	candidates := []cardinality.Candidate{
		{Key: "target", Primary: 1, Secondary: 1},
		{Key: "a", Primary: 100, Secondary: 100},
		{Key: "b", Primary: 90, Secondary: 90},
	}
	kept, _ = sel.Select(7, candidates)
	require.NotContains(t, keysOf(kept), "target", "convergence: retained key must not self-renew")
}

func TestSelector_ReentryResetsHysteresis(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 1, MaxKeys: 10, Hysteresis: 5})
	c1 := []cardinality.Candidate{
		{Key: "target", Primary: 100, Secondary: 100},
	}
	kept, _ := sel.Select(1, c1)
	require.Contains(t, keysOf(kept), "target")
	// Drop to retained for 2 cycles
	for cycle := uint64(2); cycle <= 3; cycle++ {
		candidates := []cardinality.Candidate{
			{Key: "target", Primary: 1, Secondary: 1},
			{Key: "a", Primary: 100, Secondary: 100},
		}
		kept, _ = sel.Select(cycle, candidates)
		require.Contains(t, keysOf(kept), "target")
	}
	// Re-enter fresh at cycle 4
	candidates := []cardinality.Candidate{
		{Key: "target", Primary: 200, Secondary: 200},
		{Key: "a", Primary: 100, Secondary: 100},
	}
	kept, _ = sel.Select(4, candidates)
	require.Contains(t, keysOf(kept), "target")
	// Should survive another 5 cycles of being retained
	for cycle := uint64(5); cycle <= 9; cycle++ {
		candidates := []cardinality.Candidate{
			{Key: "target", Primary: 1, Secondary: 1},
			{Key: "a", Primary: 100, Secondary: 100},
		}
		kept, _ = sel.Select(cycle, candidates)
		require.Contains(t, keysOf(kept), "target", "after reentry cycle %d", cycle)
	}
}

func TestSelector_MaxKeysTruncates(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 200, MaxKeys: 200, Hysteresis: 5})
	candidates := make([]cardinality.Candidate, 0, 500)
	for i := 0; i < 500; i++ {
		candidates = append(candidates, cardinality.Candidate{
			Key:       "key-" + itoa(i),
			Primary:   float64(500 - i),
			Secondary: float64(i),
		})
	}
	kept, truncated := sel.Select(1, candidates)
	require.Len(t, kept, 200)
	require.True(t, truncated)
	// Highest Primary should be kept
	require.Equal(t, "key-0", kept[0].Key)
}

func TestSelector_ConfiguredTopNIsHonoured(t *testing.T) {
	t.Parallel()
	// The two rankings are disjoint: the 150 highest Primary and the 150
	// highest Secondary name 300 different keys, which is what top_n = 150
	// asks for. The default cap of 200 used to cut 100 of them every cycle.
	sel := cardinality.NewSelector(cardinality.Options{TopN: 150, Hysteresis: 5})
	candidates := make([]cardinality.Candidate, 0, 400)
	for i := 0; i < 150; i++ {
		candidates = append(candidates,
			cardinality.Candidate{Key: "p-" + itoa(i), Primary: float64(1000 - i), Secondary: 0},
			cardinality.Candidate{Key: "s-" + itoa(i), Primary: 0, Secondary: float64(1000 - i)},
		)
	}
	for i := 0; i < 100; i++ {
		candidates = append(candidates, cardinality.Candidate{Key: "o-" + itoa(i), Primary: 0, Secondary: 0})
	}
	for cycle := uint64(1); cycle <= 3; cycle++ {
		kept, truncated := sel.Select(cycle, candidates)
		require.False(t, truncated, "cycle %d", cycle)
		require.Len(t, kept, 300, "cycle %d", cycle)
	}
}

func TestSelector_FreshKeysOutrankRetainedHistory(t *testing.T) {
	t.Parallel()
	// In cycle 2 the top two by Primary are new keys. h1 and h2 are retained
	// history with a high Primary, and s1 and s2 are fresh by Secondary with a
	// low Primary. With a cap of 4 the history must be cut, not the fresh keys.
	sel := cardinality.NewSelector(cardinality.Options{TopN: 2, MaxKeys: 4, Hysteresis: 5})
	sel.Select(1, []cardinality.Candidate{
		{Key: "h1", Primary: 100, Secondary: 0},
		{Key: "h2", Primary: 99, Secondary: 0},
		{Key: "s1", Primary: 2, Secondary: 100},
		{Key: "s2", Primary: 1, Secondary: 99},
	})
	kept, truncated := sel.Select(2, []cardinality.Candidate{
		{Key: "n1", Primary: 200, Secondary: 0},
		{Key: "n2", Primary: 190, Secondary: 0},
		{Key: "h1", Primary: 100, Secondary: 0},
		{Key: "h2", Primary: 99, Secondary: 0},
		{Key: "s1", Primary: 2, Secondary: 100},
		{Key: "s2", Primary: 1, Secondary: 99},
	})
	require.False(t, truncated)
	require.ElementsMatch(t, []string{"n1", "n2", "s1", "s2"}, keysOf(kept))
}

// TestSelector_DefaultTopNUnderChurnIsNotTruncated: at the default options a
// workload whose top-N rotates every cycle keeps the fresh keys of the last
// Hysteresis cycles, and those retained keys fill past the default cap of 200
// within a few cycles. That is history being trimmed, not a budget too small
// for the configured top-N, so truncated stays false and every fresh key is
// reported.
func TestSelector_DefaultTopNUnderChurnIsNotTruncated(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{})
	for cycle := uint64(1); cycle <= 24; cycle++ {
		candidates := make([]cardinality.Candidate, 0, 1000)
		fresh := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			// The Primary top-50 is a window that slides 25 keys per cycle, and
			// the Secondary top-50 is the same 50 keys every cycle.
			pos := ((i-25*int(cycle))%1000 + 1000) % 1000
			key := "k-" + itoa(i)
			candidates = append(candidates, cardinality.Candidate{Key: key, Primary: float64(1000 - pos), Secondary: float64(1000 - i)})
			if pos < 50 || i < 50 {
				fresh[key] = true
			}
		}
		kept, truncated := sel.Select(cycle, candidates)
		require.False(t, truncated, "cycle %d", cycle)
		reported := make(map[string]bool)
		for _, key := range keysOf(kept) {
			reported[key] = true
		}
		for key := range fresh {
			require.True(t, reported[key], "cycle %d: fresh key %s not reported", cycle, key)
		}
	}
}

func itoa(i int) string {
	// simple
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestSelector_NoTruncationFlagWhenUnderCap(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 50, MaxKeys: 200, Hysteresis: 5})
	candidates := []cardinality.Candidate{
		{Key: "a", Primary: 10, Secondary: 1},
		{Key: "b", Primary: 9, Secondary: 2},
	}
	kept, truncated := sel.Select(1, candidates)
	require.False(t, truncated)
	require.Len(t, kept, 2)
}

func TestSelector_EmptyInput(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 50, MaxKeys: 200, Hysteresis: 5})
	kept, truncated := sel.Select(1, nil)
	require.Empty(t, kept)
	require.False(t, truncated)
}

func TestSelector_5000CandidatesStaysBounded(t *testing.T) {
	t.Parallel()
	sel := cardinality.NewSelector(cardinality.Options{TopN: 50, MaxKeys: 200, Hysteresis: 5})
	r := rand.New(rand.NewSource(1))
	t.Logf("seed 1")
	for cycle := uint64(1); cycle <= 200; cycle++ {
		var candidates []cardinality.Candidate
		for i := 0; i < 5000; i++ {
			candidates = append(candidates, cardinality.Candidate{
				Key:       "key-" + itoa(i),
				Primary:   r.Float64() * 1000,
				Secondary: r.Float64() * 1000,
			})
		}
		kept, _ := sel.Select(cycle, candidates)
		require.LessOrEqual(t, len(kept), 200)
		if cycle%50 == 0 {
			sel.Forget(cycle)
		}
	}
}

func TestBudget_Admit(t *testing.T) {
	t.Parallel()
	b := cardinality.NewBudget(10)
	admitted, truncated := b.Admit(0, 5)
	require.Equal(t, 5, admitted)
	require.False(t, truncated)

	admitted, truncated = b.Admit(8, 5)
	require.Equal(t, 2, admitted)
	require.True(t, truncated)

	admitted, truncated = b.Admit(10, 5)
	require.Equal(t, 0, admitted)
	require.True(t, truncated)

	admitted, truncated = b.Admit(0, 0)
	require.Equal(t, 0, admitted)
	require.False(t, truncated)

	admitted, truncated = b.Admit(5, 5)
	require.Equal(t, 5, admitted)
	require.False(t, truncated)
}

func FuzzSelector_NeverExceedsMaxKeys(f *testing.F) {
	f.Add(int64(10), int64(5), int64(100))
	f.Fuzz(func(t *testing.T, n, topN, maxKeys int64) {
		if topN <= 0 || topN > 100 {
			return
		}
		if maxKeys <= 0 || maxKeys > 500 {
			return
		}
		if n < 0 || n > 1000 {
			return
		}
		sel := cardinality.NewSelector(cardinality.Options{TopN: int(topN), MaxKeys: int(maxKeys), Hysteresis: 5})
		var candidates []cardinality.Candidate
		for i := int64(0); i < n; i++ {
			candidates = append(candidates, cardinality.Candidate{
				Key:       "k-" + itoa(int(i)),
				Primary:   float64(i),
				Secondary: float64(n - i),
			})
		}
		kept, _ := sel.Select(1, candidates)
		require.LessOrEqual(t, len(kept), int(maxKeys))
	})
}

func keysOf(cands []cardinality.Candidate) []string {
	var out []string
	for _, c := range cands {
		out = append(out, c.Key)
	}
	return out
}
