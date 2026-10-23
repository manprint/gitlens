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
	return observe(e.prev, e.maxGap, o)
}

// observe applies the rules to one observation, moving the baseline in
// baselines when a rule says the observation becomes the new baseline.
func observe(baselines map[pgtype.SeriesKey]state, maxGap time.Duration, o Observation) (*Point, *ResetEvent) {
	prev, ok := baselines[o.Key]
	if !ok {
		// Rule 1: no previous observation
		cp := copyStatsReset(o.StatsReset)
		baselines[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, nil
	}

	// Rule 2: duplicate or out-of-order sample must not corrupt baseline
	if !o.TS.After(prev.ts) {
		return nil, nil
	}

	// Rule 3: stats_reset changed
	if o.StatsReset != nil && prev.statsReset != nil && !o.StatsReset.Equal(*prev.statsReset) {
		cp := copyStatsReset(o.StatsReset)
		baselines[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, &ResetEvent{Key: o.Key, TS: o.TS, Reason: ResetStatsChanged}
	}

	// Rule 4: counter went backwards
	if o.Value < prev.value {
		cp := copyStatsReset(o.StatsReset)
		baselines[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, &ResetEvent{Key: o.Key, TS: o.TS, Reason: ResetCounterWentBackwards}
	}

	// Rule 5: gap exceeds MaxGap
	if o.TS.Sub(prev.ts) > maxGap {
		cp := copyStatsReset(o.StatsReset)
		baselines[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
		return nil, nil
	}

	// Rule 6: normal rate
	dt := o.TS.Sub(prev.ts).Seconds()
	rate := (o.Value - prev.value) / dt
	cp := copyStatsReset(o.StatsReset)
	baselines[o.Key] = state{ts: o.TS, value: o.Value, statsReset: cp}
	return &Point{Key: o.Key, TS: o.TS, Rate: rate, Seconds: dt}, nil
}

// Batch stages the baselines that one envelope moves. Its observations are
// computed against the engine's state, overlaid with the batch's own earlier
// observations, but the engine itself changes only on Commit. The caller
// commits once the transaction that stores the points has committed. An
// envelope whose transaction rolls back therefore leaves the baselines where
// they were: its retry is observed as the interval it really is, and is not
// dropped as a replay of a sample that was never stored.
//
// Two attempts of one envelope can overlap when a client gives up on a slow
// request and sends it again. Neither has committed when the other observes, so
// both see the same baseline and both report the same reset. The rate points
// are stored with ON CONFLICT DO NOTHING and come out once. The reset events
// are not, so the caller's event insert can store that reset twice.
type Batch struct {
	e      *Engine
	staged map[pgtype.SeriesKey]state
}

// Begin starts a batch for one envelope.
func (e *Engine) Begin() *Batch {
	return &Batch{e: e, staged: make(map[pgtype.SeriesKey]state)}
}

// Observe is Engine.Observe with the baseline staged in the batch.
func (b *Batch) Observe(o Observation) (*Point, *ResetEvent) {
	b.e.mu.Lock()
	defer b.e.mu.Unlock()
	if _, ok := b.staged[o.Key]; !ok {
		if s, ok := b.e.prev[o.Key]; ok {
			b.staged[o.Key] = s
		}
	}
	return observe(b.staged, b.e.maxGap, o)
}

// Commit moves the staged baselines into the engine. A key that a later
// envelope has already moved past the staged sample keeps its newer baseline,
// so a late commit never steps a baseline backwards.
func (b *Batch) Commit() {
	b.e.mu.Lock()
	defer b.e.mu.Unlock()
	for k, s := range b.staged {
		if cur, ok := b.e.prev[k]; ok && !s.ts.After(cur.ts) {
			continue
		}
		b.e.prev[k] = s
	}
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

// CountByInstance returns, per instance, how many series the engine still
// holds a baseline for: every (metric, database, labels) combination Observe
// has seen and Evict has not yet dropped. That is the engine's own memory, not
// the cardinality budget (I-8). The budget is the per-instance count of series
// in the latest envelope, which the pipeline reports as pglens_series_total.
// Nothing in production calls this method.
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
