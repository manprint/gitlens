package check

import (
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
