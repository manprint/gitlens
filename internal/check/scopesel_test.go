package check

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

func scopeTarget(instance uuid.UUID, database string, clk clock.Clock) *SimpleTarget {
	return &SimpleTarget{
		InstanceIDValue: pgtype.InstanceID(instance),
		DatabaseValue:   database,
		ClockValue:      clk,
	}
}

// TestScopedSelectors_CycleIsPerScope — checks are package-level singletons
// shared by every (target, database) the scheduler creates an entry for. A
// single cycle counter advanced once per scrape rather than once per scrape
// *of this scope*, so cardinality.Selector's hysteresis grace period expired
// N times too fast on an agent watching N scopes.
func TestScopedSelectors_CycleIsPerScope(t *testing.T) {
	t.Parallel()
	s := newScopedSelectors(cardinality.Options{TopN: 5})
	a := uuid.New()
	b := uuid.New()

	for i := uint64(1); i <= 3; i++ {
		if _, cycle := s.next(scopeTarget(a, "app", clock.System())); cycle != i {
			t.Fatalf("instance a/app cycle = %d, want %d", cycle, i)
		}
	}
	if _, cycle := s.next(scopeTarget(a, "reporting", clock.System())); cycle != 1 {
		t.Errorf("a second database of the same instance starts at cycle %d, want 1", cycle)
	}
	if _, cycle := s.next(scopeTarget(b, "app", clock.System())); cycle != 1 {
		t.Errorf("an identically named database on another instance starts at cycle %d, want 1", cycle)
	}
}

// TestScopedSelectors_SelectorIsPerScope — the selector's lastSeen map is
// keyed by queryid or "schema.relation", which are unique only within one
// database of one instance. Sharing one selector let whichever scope scraped
// last decide every other scope's retention.
func TestScopedSelectors_SelectorIsPerScope(t *testing.T) {
	t.Parallel()
	s := newScopedSelectors(cardinality.Options{TopN: 5})
	a := uuid.New()

	selA, _ := s.next(scopeTarget(a, "app", clock.System()))
	selB, _ := s.next(scopeTarget(a, "reporting", clock.System()))
	if selA == selB {
		t.Fatal("two databases of the same instance share one cardinality selector")
	}
	selAAgain, _ := s.next(scopeTarget(a, "app", clock.System()))
	if selA != selAAgain {
		t.Error("the same scope was handed two different selectors")
	}
}

// TestScopedSelectors_SetTopNResetsEveryScope preserves the previous
// single-selector behaviour: configureChecks applies the configured budget
// before scheduling starts and no accumulated state should survive it.
func TestScopedSelectors_SetTopNResetsEveryScope(t *testing.T) {
	t.Parallel()
	s := newScopedSelectors(cardinality.Options{TopN: 5})
	a := uuid.New()
	s.next(scopeTarget(a, "app", clock.System()))
	s.next(scopeTarget(a, "app", clock.System()))

	s.SetTopN(11)
	if _, cycle := s.next(scopeTarget(a, "app", clock.System())); cycle != 1 {
		t.Errorf("cycle after SetTopN = %d, want 1", cycle)
	}
	s.mu.Lock()
	got := s.opts.TopN
	s.mu.Unlock()
	if got != 11 {
		t.Errorf("TopN = %d, want 11", got)
	}
}

// TestScopedSelectors_PrunesIdleScopes — a dropped database, or one that
// falls out of the db_budget top-N, must not pin its selector's lastSeen map
// for the lifetime of the agent process.
func TestScopedSelectors_PrunesIdleScopes(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := newScopedSelectors(cardinality.Options{TopN: 5})
	a := uuid.New()

	s.next(scopeTarget(a, "transient", clk))
	s.next(scopeTarget(a, "kept", clk))

	clk.Advance(scopeIdleTTL + time.Minute)
	s.next(scopeTarget(a, "kept", clk))

	s.mu.Lock()
	n := len(s.scope)
	_, transientStillThere := s.scope[scopeKey(scopeTarget(a, "transient", clk))]
	s.mu.Unlock()
	if transientStillThere {
		t.Errorf("idle scope was not pruned (%d scopes retained)", n)
	}
}

// TestScopedSelectors_PrefilterKeepsTheSelection: index_stats and table_stats
// read from PostgreSQL only the top prefilterLimit rows of each ranking plus
// the retained keys. Under a workload that reshuffles every ranking on every
// cycle, those narrowed candidates must yield the very selection, and the
// same truncation flag, as the full candidate list.
func TestScopedSelectors_PrefilterKeepsTheSelection(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7))
	narrowed := newScopedSelectors(cardinality.Options{TopN: 20})
	full := cardinality.NewSelector(cardinality.Options{TopN: 20})
	target := scopeTarget(uuid.New(), "app", clock.System())
	all := make([]cardinality.Candidate, 5000)
	for i := range all {
		all[i].Key = fmt.Sprintf("public.k%05d", i)
	}
	for cycle := uint64(1); cycle <= 40; cycle++ {
		for i := range all {
			// Few distinct values, so ties at every rank exercise the key order.
			all[i].Primary = float64(rng.Intn(400))
			all[i].Secondary = float64(rng.Intn(400))
		}
		in := sqlPrefilter(all, narrowed.prefilterLimit(), narrowed.retained(target))
		rng.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		selector, scopeCycle := narrowed.next(target)
		if scopeCycle != cycle {
			t.Fatalf("scope cycle = %d, want %d", scopeCycle, cycle)
		}
		got, gotTruncated := selector.Select(scopeCycle, in)
		selector.Forget(scopeCycle)
		want, wantTruncated := full.Select(cycle, all)
		full.Forget(cycle)
		if fmt.Sprint(candidateKeys(got)) != fmt.Sprint(candidateKeys(want)) || gotTruncated != wantTruncated {
			t.Fatalf("cycle %d: prefiltered selection %v (truncated %v), full selection %v (truncated %v)",
				cycle, candidateKeys(got), gotTruncated, candidateKeys(want), wantTruncated)
		}
	}
}

// sqlPrefilter is the candidate narrowing indexStatsQuery and tableStatsQuery
// do in SQL: the top limit by each ranking, score descending then key in byte
// order (COLLATE "C"), and every retained key.
func sqlPrefilter(all []cardinality.Candidate, limit int, retained []string) []cardinality.Candidate {
	picked := make(map[string]bool)
	for _, score := range []func(cardinality.Candidate) float64{
		func(c cardinality.Candidate) float64 { return c.Primary },
		func(c cardinality.Candidate) float64 { return c.Secondary },
	} {
		ranked := append([]cardinality.Candidate(nil), all...)
		sort.Slice(ranked, func(i, j int) bool {
			if score(ranked[i]) != score(ranked[j]) {
				return score(ranked[i]) > score(ranked[j])
			}
			return ranked[i].Key < ranked[j].Key
		})
		for _, c := range ranked[:min(limit, len(ranked))] {
			picked[c.Key] = true
		}
	}
	for _, k := range retained {
		picked[k] = true
	}
	var out []cardinality.Candidate
	for _, c := range all {
		if picked[c.Key] {
			out = append(out, c)
		}
	}
	return out
}

func candidateKeys(cs []cardinality.Candidate) []string {
	keys := make([]string, len(cs))
	for i, c := range cs {
		keys[i] = c.Key
	}
	return keys
}
