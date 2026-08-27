package delta

import (
	"sync"
	"time"

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

	prev, ok := e.prev[o.Key]
	if !ok {
		// Rule 1: no previous observation
		cp := copyStatsReset(o.StatsReset)
		e.prev[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, nil
	}

	// Rule 2: duplicate or out-of-order sample must not corrupt baseline
	if !o.TS.After(prev.ts) {
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
	return &Point{Key: o.Key, TS: o.TS, Rate: rate}, nil
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

func copyStatsReset(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}
