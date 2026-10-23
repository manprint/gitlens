package delta

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
)

// Observation is one counter sample.
type Observation struct {
	Key        pgtype.SeriesKey
	TS         time.Time
	Value      float64
	StatsReset *time.Time // from pg_stat_database.stats_reset et al; nil when source has none
}

// Point is a derived rate.
type Point struct {
	Key  pgtype.SeriesKey
	TS   time.Time
	Rate float64 // units per second
	// Seconds is the interval the rate was measured over: Rate * Seconds is
	// the increase observed in it.
	Seconds float64
}

// ResetReason explains why a reset was detected.
type ResetReason string

const (
	ResetStatsChanged         ResetReason = "stats_reset_changed"
	ResetCounterWentBackwards ResetReason = "counter_went_backwards"
)

// ResetEvent is emitted when a counter reset is detected. No Point is emitted for the same observation.
type ResetEvent struct {
	Key    pgtype.SeriesKey
	TS     time.Time
	Reason ResetReason
}

// Options configures the engine.
type Options struct {
	MaxGap time.Duration // default 10m
}

type state struct {
	ts         time.Time
	value      float64
	statsReset *time.Time
}

// Engine converts cumulative counters to rates with reset detection.
// It handles counters only. Gauges must never reach Observe.
type Engine struct {
	mu     sync.Mutex
	prev   map[pgtype.SeriesKey]state
	maxGap time.Duration
}

// New creates an Engine.
func New(opts Options) *Engine {
	maxGap := opts.MaxGap
	if maxGap == 0 {
		maxGap = 10 * time.Minute
	}
	return &Engine{
		prev:   make(map[pgtype.SeriesKey]state),
		maxGap: maxGap,
	}
}

// Observe records one counter observation and returns the derived rate point,
// the detected reset, or neither. It never returns both.
func (e *Engine) Observe(o Observation) (*Point, *ResetEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.observe(o)
}

// ObserveUndoable is Observe, and also returns the function that reverts the
// baseline it wrote. The revert changes only a baseline that still holds what
// Observe wrote, so it never discards a later observation of the same series.
func (e *Engine) ObserveUndoable(o Observation) (*Point, *ResetEvent, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	before, hadBefore := e.prev[o.Key]
	pt, ev := e.observe(o)
	written := e.prev[o.Key]
	return pt, ev, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if cur, ok := e.prev[o.Key]; !ok || cur != written {
			return
		}
		if hadBefore {
			e.prev[o.Key] = before
		} else {
			delete(e.prev, o.Key)
		}
	}
}

// observe is Observe for a caller that holds e.mu.
func (e *Engine) observe(o Observation) (*Point, *ResetEvent) {
	prev, ok := e.prev[o.Key]
	if !ok {
		// Rule 1: no previous observation
		cp := copyStatsReset(o.StatsReset)
		e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, nil
	}

	// Rule 2: duplicate or out-of-order sample must not corrupt baseline
	if !o.TS.After(prev.ts) {
		// A sample more than maxGap behind the baseline is not out of order: the
		// clock that stamped the baseline has stepped since, and a forward step
		// that was corrected leaves the baseline ahead of every real sample. Left
		// alone, the baseline would drop each real sample until the clock caught up
		// with it, so the baseline moves to this sample, with no rate point. A
		// duplicate, or a sample only slightly late, keeps the baseline as it is.
		if prev.ts.Sub(o.TS) > e.maxGap {
			cp := copyStatsReset(o.StatsReset)
			e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		}
		return nil, nil
	}

	// Rule 3: stats_reset changed
	if o.StatsReset != nil && prev.statsReset != nil && !o.StatsReset.Equal(*prev.statsReset) {
		cp := copyStatsReset(o.StatsReset)
		e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, &ResetEvent{Key: o.Key, TS: o.TS, Reason: ResetStatsChanged}
	}

	// Rule 4: counter went backwards
	if o.Value < prev.value {
		cp := copyStatsReset(o.StatsReset)
		e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, &ResetEvent{Key: o.Key, TS: o.TS, Reason: ResetCounterWentBackwards}
	}

	// Rule 5: gap exceeds MaxGap
	if o.TS.Sub(prev.ts) > e.maxGap {
		cp := copyStatsReset(o.StatsReset)
		e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, nil
	}

	// Rule 6: normal rate
	dt := o.TS.Sub(prev.ts).Seconds()
	rate := (o.Value - prev.value) / dt
	cp := copyStatsReset(o.StatsReset)
	e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
	return &Point{Key: o.Key, TS: o.TS, Rate: rate, Seconds: dt}, nil
}

// Evict drops series whose last observation is older than the cutoff and
// returns how many were removed. Bounds memory when instances disappear.
func (e *Engine) Evict(before time.Time) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for k, s := range e.prev {
		if s.ts.Before(before) {
			delete(e.prev, k)
			n++
		}
	}
	return n
}

// CountByInstance returns the number of distinct series currently tracked
// per instance — every (metric, database, labels) combination this engine
// has ever Observe()'d and not yet Evict()'d. This is the cardinality
// budget's own real number (I-8): how many distinct series an instance is
// actually reporting right now.
func (e *Engine) CountByInstance() map[uuid.UUID]int {
	e.mu.Lock()
	defer e.mu.Unlock()
	counts := make(map[uuid.UUID]int)
	for k := range e.prev {
		counts[k.Instance]++
	}
	return counts
}

func copyStatsReset(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}
