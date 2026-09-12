package check

import (
	"sync"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
)

// scopedSelectors keeps one cardinality.Selector — and one cycle counter —
// per (instance, database) a check is scheduled for.
//
// Checks are package-level singletons: check.Register stores one instance per
// name and the scheduler hands that same pointer to every (target, database)
// entry it creates. A ScopeDatabase check on an agent watching two instances
// with five monitored databases each therefore runs ten interleaved scrape
// loops through one struct. Keeping a single Selector there means:
//
//   - Key collision. The keys are queryids, or "schema.relation" names, which
//     are only unique *within* one database. `public.users` in db_a and
//     `public.users` in db_b were one entry in one lastSeen map, so whichever
//     database scraped last decided both databases' hysteresis.
//   - Hysteresis collapse. Select's grace period is measured in cycles, and
//     the counter advanced once per *scrape* rather than once per scrape *of
//     this scope*. With N scopes a 5-cycle grace period expires after 5/N of
//     its intended cycles, so a key that is genuinely stable in its own
//     database is dropped and re-added, flapping the reported series.
//   - Wrong truncation. MaxKeys is a per-report budget; applying it across
//     the union of every scope's keys reported "truncated" on results that
//     were complete, and hid real truncation on results that were not.
//
// None of this is visible on a single-target, single-database agent, which is
// what every fixture and every unit test used.
type scopedSelectors struct {
	mu    sync.Mutex
	opts  cardinality.Options
	scope map[string]*scopeState
}

type scopeState struct {
	selector *cardinality.Selector
	cycle    uint64
	lastUsed time.Time
}

// scopeIdleTTL is how long an unused scope is kept before its selector state
// is discarded. Databases come and go (a dropped database, a shifting
// db_budget top-N), and each abandoned scope would otherwise pin its
// selector's lastSeen map for the lifetime of the agent process.
const scopeIdleTTL = 6 * time.Hour

func newScopedSelectors(opts cardinality.Options) *scopedSelectors {
	return &scopedSelectors{opts: opts, scope: make(map[string]*scopeState)}
}

// SetTopN reconfigures the cardinality budget and discards all accumulated
// per-scope state, matching the previous single-selector behaviour. It is
// called from configureChecks before scheduling starts.
func (s *scopedSelectors) SetTopN(topN int) {
	if topN <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opts.TopN = topN
	s.scope = make(map[string]*scopeState)
}

// next returns the selector for t's scope together with this scope's own next
// cycle number.
func (s *scopedSelectors) next(t Target) (*cardinality.Selector, uint64) {
	key := scopeKey(t)
	now := scopeNow(t)

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.scope[key]
	if !ok {
		st = &scopeState{selector: cardinality.NewSelector(s.opts)}
		s.scope[key] = st
	}
	st.cycle++
	st.lastUsed = now
	s.pruneLocked(now)
	return st.selector, st.cycle
}

func (s *scopedSelectors) pruneLocked(now time.Time) {
	if len(s.scope) < 2 {
		return
	}
	for k, st := range s.scope {
		if !st.lastUsed.IsZero() && now.Sub(st.lastUsed) > scopeIdleTTL {
			delete(s.scope, k)
		}
	}
}

// scopeKey identifies the (instance, database) whose cardinality state this
// is. The instance is included because two targets of the same agent
// routinely monitor identically named databases ("postgres", "app"), and a
// relation or queryid is only unique within one database of one instance.
func scopeKey(t Target) string {
	return t.InstanceID().String() + "\x00" + t.Database()
}

// textCacheKey identifies the (cluster, database) a query-text cache belongs
// to. See the comment at its only use in stat_statements.go for why this is a
// different scope from scopeKey.
func textCacheKey(t Target) string {
	return t.ClusterID().String() + "\x00" + t.Database()
}

func scopeNow(t Target) time.Time {
	if clk := t.Clock(); clk != nil {
		return clk.Now()
	}
	return time.Now()
}
