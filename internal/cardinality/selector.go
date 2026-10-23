package cardinality

import (
	"sort"
	"sync"
)

// Candidate is a candidate series key with ranking metrics.
type Candidate struct {
	Key       string  // opaque, e.g. the decimal queryid
	Primary   float64 // ranking metric 1, e.g. total_exec_time
	Secondary float64 // ranking metric 2, e.g. calls
}

// Options configures the Selector.
type Options struct {
	TopN       int // per ranking metric; default 50
	Hysteresis int // cycles a key is retained after dropping out; default 5
	MaxKeys    int // hard cap after the union; default 200, or 2*TopN when larger
}

func (o *Options) defaults() {
	if o.TopN == 0 {
		o.TopN = 50
	}
	if o.Hysteresis == 0 {
		o.Hysteresis = 5
	}
	if o.MaxKeys == 0 {
		// The two top-N rankings can name up to 2*TopN distinct keys. A cap
		// below that cuts keys the configured top_n asked for, so it grows
		// with TopN instead of staying at 200.
		o.MaxKeys = max(200, 2*o.TopN)
	}
}

// Selector picks the keys to report for each cycle.
type Selector struct {
	mu       sync.Mutex
	opts     Options
	lastSeen map[string]uint64
}

// NewSelector creates a Selector.
func NewSelector(opts Options) *Selector {
	opts.defaults()
	return &Selector{
		opts:     opts,
		lastSeen: make(map[string]uint64),
	}
}

// Select picks the keys to report for this cycle. cycle is a monotonically
// increasing counter supplied by the caller. truncated reports whether MaxKeys
// forced a fresh key out — the caller must propagate it to Result.Truncated so
// the UI can say "N not shown" instead of implying full coverage. Retained keys
// trimmed to fit are not reported as truncated: they were kept only for
// stability, and a churning workload fills the cap with them by design.
func (s *Selector) Select(cycle uint64, in []Candidate) (kept []Candidate, truncated bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(in) == 0 {
		return nil, false
	}

	// Map for quick lookup of candidates by key
	byKey := make(map[string]Candidate, len(in))
	for _, c := range in {
		byKey[c.Key] = c
	}

	// Step 1: fresh = (top TopN by Primary) ∪ (top TopN by Secondary)
	freshSet := make(map[string]bool)
	// TopN by Primary
	sortedPrimary := make([]Candidate, len(in))
	copy(sortedPrimary, in)
	sort.Slice(sortedPrimary, func(i, j int) bool {
		if sortedPrimary[i].Primary != sortedPrimary[j].Primary {
			return sortedPrimary[i].Primary > sortedPrimary[j].Primary
		}
		return sortedPrimary[i].Key < sortedPrimary[j].Key
	})
	for i := 0; i < len(sortedPrimary) && i < s.opts.TopN; i++ {
		freshSet[sortedPrimary[i].Key] = true
	}
	// TopN by Secondary
	sortedSecondary := make([]Candidate, len(in))
	copy(sortedSecondary, in)
	sort.Slice(sortedSecondary, func(i, j int) bool {
		if sortedSecondary[i].Secondary != sortedSecondary[j].Secondary {
			return sortedSecondary[i].Secondary > sortedSecondary[j].Secondary
		}
		return sortedSecondary[i].Key < sortedSecondary[j].Key
	})
	for i := 0; i < len(sortedSecondary) && i < s.opts.TopN; i++ {
		freshSet[sortedSecondary[i].Key] = true
	}

	// Step 2: retained = keys in in that are not in fresh but whose lastSeen is >= cycle - Hysteresis
	// Note: cycle is uint64, need to handle underflow for early cycles
	var hysteresisThreshold uint64
	if cycle > uint64(s.opts.Hysteresis) {
		hysteresisThreshold = cycle - uint64(s.opts.Hysteresis)
	} else {
		hysteresisThreshold = 0
	}

	retainedSet := make(map[string]bool)
	for _, c := range in {
		if freshSet[c.Key] {
			continue
		}
		if last, ok := s.lastSeen[c.Key]; ok && last >= hysteresisThreshold {
			retainedSet[c.Key] = true
		}
	}

	// Step 3: fresh and retained keys, each ordered by Primary descending then Key ascending
	var freshList, retainedList []Candidate
	for key := range freshSet {
		freshList = append(freshList, byKey[key])
	}
	for key := range retainedSet {
		retainedList = append(retainedList, byKey[key])
	}
	sortByRank(freshList)
	sortByRank(retainedList)

	// Step 4: fit the cap. The fresh keys are what the configured top-N asked
	// for, so they are cut only when they alone exceed MaxKeys. Retained keys
	// are history and fill whatever room the fresh keys leave; with the default
	// top-N they fill the cap on a churning workload, and they are the ones
	// dropped then. Cutting the union by Primary alone let a retained key with
	// a higher Primary push out a fresh key.
	if len(freshList) > s.opts.MaxKeys {
		truncated = true
		freshList = freshList[:s.opts.MaxKeys]
	}
	if room := s.opts.MaxKeys - len(freshList); len(retainedList) > room {
		retainedList = retainedList[:room]
	}
	keptList := append(freshList, retainedList...)
	sortByRank(keptList)

	// Step 5: Update lastSeen for every key in fresh ONLY
	for key := range freshSet {
		s.lastSeen[key] = cycle
	}

	return keptList, truncated
}

// sortByRank orders candidates by Primary descending, then Key ascending, so
// the same input always yields the same selection.
func sortByRank(cs []Candidate) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Primary != cs[j].Primary {
			return cs[i].Primary > cs[j].Primary
		}
		return cs[i].Key < cs[j].Key
	})
}

// Forget drops retention state for keys not seen within the window, bounding
// memory when a workload changes shape.
func (s *Selector) Forget(cycle uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var threshold uint64
	if cycle > uint64(s.opts.Hysteresis) {
		threshold = cycle - uint64(s.opts.Hysteresis)
	} else {
		return 0 // nothing old enough to forget early
	}
	removed := 0
	for k, last := range s.lastSeen {
		if last < threshold {
			delete(s.lastSeen, k)
			removed++
		}
	}
	return removed
}
