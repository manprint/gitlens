package ash

import (
	"sort"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/clock"
)

// otherKey is the bucket every key beyond maxKeys-1 folds into. It is
// structurally distinct from any real key: no real Datname/WaitEventType/
// WaitEvent/State is ever the literal string "other" on all four fields at
// once in a query that also has no query id (guaranteed by construction: a
// real "other" would require the sampled row to spell out those exact
// strings on every one of four independent SQL COALESCE defaults, which the
// COALESCE defaults never produce together).
var otherKey = Key{Datname: "", WaitEventType: "other", WaitEvent: "other", State: "other"}

// Key identifies one distinct (datname, wait_event_type, wait_event, state,
// query_id) combination observed by the sampler within a window.
type Key struct {
	Datname       string
	WaitEventType string
	WaitEvent     string
	State         string
	QueryID       int64 // 0 means absent; HasQueryID says whether it is real
	HasQueryID    bool
}

// Bucket is one distinct key's sample count within a window.
type Bucket struct {
	Key     Key
	Samples int
}

// Window is one closed aggregation period.
type Window struct {
	Start, End time.Time
	Ticks      int // sampling ticks that actually succeeded
	Buckets    []Bucket
	Truncated  bool
}

// Aggregator folds 1-second samples into fixed-length windows, capping
// cardinality while conserving the total sample count exactly.
type Aggregator struct {
	clk        clock.Clock
	window     time.Duration
	maxKeys    int
	windowFunc func() time.Time // overridable in tests; defaults to clk.Now

	mu      sync.Mutex
	start   time.Time
	started bool
	counts  map[Key]int
	ticks   int
}

// New creates an Aggregator that closes a window every window duration and
// caps each window to at most maxKeys distinct buckets (the maxKeys-1 largest
// plus one "other" bucket for the remainder).
func New(clk clock.Clock, window time.Duration, maxKeys int) *Aggregator {
	if maxKeys < 1 {
		maxKeys = 1
	}
	return &Aggregator{
		clk:     clk,
		window:  window,
		maxKeys: maxKeys,
		counts:  make(map[Key]int),
	}
}

// Add records one sample. datname/state/wet/we are the raw sampler fields
// (already NULL-coalesced upstream); queryID is nil when compute_query_id is
// off or the row carried no query id.
func (a *Aggregator) Add(datname, state, wet, we string, queryID *int64) {
	k := Key{Datname: datname, WaitEventType: wet, WaitEvent: we, State: state}
	if queryID != nil {
		k.QueryID = *queryID
		k.HasQueryID = true
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureStarted()
	a.counts[k]++
}

// TickFailed records that a sampling tick did not succeed (timed out or
// errored); it must not be counted in the window's Ticks denominator.
func (a *Aggregator) TickFailed() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureStarted()
	// A failed tick is still a tick attempt for window bookkeeping purposes,
	// but per phase_08.md 7.2 the denominator is ticks that actually
	// happened (succeeded); a failed tick therefore contributes 0 to Ticks.
}

// TickSucceeded records that a sampling tick completed successfully, whether
// or not it returned any rows (an instance with zero active sessions still
// contributes a successful tick to the denominator).
func (a *Aggregator) TickSucceeded() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureStarted()
	a.ticks++
}

func (a *Aggregator) ensureStarted() {
	if !a.started {
		a.start = a.now()
		a.started = true
	}
}

func (a *Aggregator) now() time.Time {
	if a.windowFunc != nil {
		return a.windowFunc()
	}
	return a.clk.Now()
}

// Flush closes the current window and returns it, resetting internal state
// for the next window. Calling Flush on an empty (never-Added-to) window
// still returns a valid Window with Ticks and zero Buckets, never nil.
func (a *Aggregator) Flush(now time.Time) Window {
	a.mu.Lock()
	defer a.mu.Unlock()

	start := a.start
	if !a.started {
		start = now
	}

	w := Window{Start: start, End: now, Ticks: a.ticks}
	w.Buckets, w.Truncated = capConserved(a.counts, a.maxKeys)

	a.counts = make(map[Key]int)
	a.ticks = 0
	a.start = now
	a.started = true

	return w
}

// capConserved returns at most maxKeys buckets: the maxKeys-1 largest by
// count, plus (if anything remains) one "other" bucket folding the rest. The
// sum of returned Samples always equals the sum of counts — the conservation
// invariant.
func capConserved(counts map[Key]int, maxKeys int) ([]Bucket, bool) {
	if len(counts) == 0 {
		return nil, false
	}

	buckets := make([]Bucket, 0, len(counts))
	for k, n := range counts {
		buckets = append(buckets, Bucket{Key: k, Samples: n})
	}

	if len(buckets) <= maxKeys {
		sortBuckets(buckets)
		return buckets, false
	}

	sortBuckets(buckets)
	keep := maxKeys - 1
	if keep < 0 {
		keep = 0
	}
	kept := buckets[:keep]
	overflow := buckets[keep:]

	otherTotal := 0
	for _, b := range overflow {
		otherTotal += b.Samples
	}

	out := make([]Bucket, 0, keep+1)
	out = append(out, kept...)
	if otherTotal > 0 {
		out = append(out, Bucket{Key: otherKey, Samples: otherTotal})
	}
	return out, true
}

// sortBuckets orders by Samples descending, then by a deterministic
// tie-break on the key fields so two runs over the same counts agree.
func sortBuckets(b []Bucket) {
	sort.Slice(b, func(i, j int) bool {
		if b[i].Samples != b[j].Samples {
			return b[i].Samples > b[j].Samples
		}
		return keyLess(b[i].Key, b[j].Key)
	})
}

func keyLess(a, b Key) bool {
	if a.Datname != b.Datname {
		return a.Datname < b.Datname
	}
	if a.WaitEventType != b.WaitEventType {
		return a.WaitEventType < b.WaitEventType
	}
	if a.WaitEvent != b.WaitEvent {
		return a.WaitEvent < b.WaitEvent
	}
	if a.State != b.State {
		return a.State < b.State
	}
	if a.HasQueryID != b.HasQueryID {
		return !a.HasQueryID
	}
	return a.QueryID < b.QueryID
}
