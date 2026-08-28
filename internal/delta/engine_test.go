package delta_test

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/delta"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func mkKey(metric string) pgtype.SeriesKey {
	return pgtype.SeriesKey{
		Metric:   metric,
		Instance: uuid.New(),
		Database: "app",
		Labels:   "",
	}
}

func TestEngine_Rule1_FirstObservationNoPointNoEvent(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("pg_xact_commit_total")
	now := time.Now()
	pt, ev := e.Observe(delta.Observation{Key: key, TS: now, Value: 100})
	require.Nil(t, pt)
	require.Nil(t, ev)
}

func TestEngine_Rule2_DuplicateTimestampLeavesBaselineIntact(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("pg_xact_commit_total")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
	// duplicate with 999 should not corrupt baseline
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0, Value: 999})
	require.Nil(t, pt)
	require.Nil(t, ev)
	pt, ev = e.Observe(delta.Observation{Key: key, TS: t0.Add(60 * time.Second), Value: 160})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule2_OutOfOrderIgnored(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0.Add(-10 * time.Second), Value: 999})
	pt, _ := e.Observe(delta.Observation{Key: key, TS: t0.Add(60 * time.Second), Value: 160})
	require.NotNil(t, pt)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule3_StatsResetChanged(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := t0.Add(-time.Hour)
	r2 := t0.Add(30 * time.Second)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100, StatsReset: &r1})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 5, StatsReset: &r2})
	require.Nil(t, pt)
	require.NotNil(t, ev)
	require.Equal(t, delta.ResetStatsChanged, ev.Reason)
	// next observation resumes from 5
	pt, ev = e.Observe(delta.Observation{Key: key, TS: t0.Add(2 * time.Minute), Value: 65, StatsReset: &r2})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule3_StatsResetUnchangedIsNotAReset(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := t0.Add(-time.Hour)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100, StatsReset: &r1})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 160, StatsReset: &r1})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule3_NilStatsResetNeverTriggers(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100, StatsReset: nil})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 160, StatsReset: nil})
	require.NotNil(t, pt)
	require.Nil(t, ev)
}

func TestEngine_Rule4_CounterWentBackwards(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := t0.Add(-time.Hour)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100, StatsReset: &r1})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 4, StatsReset: &r1})
	require.Nil(t, pt)
	require.NotNil(t, ev)
	require.Equal(t, delta.ResetCounterWentBackwards, ev.Reason)
}

func TestEngine_Rule5_GapExceedsMaxGap(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{MaxGap: 10 * time.Minute})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(11 * time.Minute), Value: 200})
	require.Nil(t, pt)
	require.Nil(t, ev)
	// baseline moved, next point from new baseline
	pt, ev = e.Observe(delta.Observation{Key: key, TS: t0.Add(12 * time.Minute), Value: 260})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule5_GapAtBoundary(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{MaxGap: 10 * time.Minute})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(10 * time.Minute), Value: 700})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Rule6_FlatCounterYieldsZeroNotNil(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
	pt, ev := e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 100})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.Equal(t, 0.0, pt.Rate)
}

func TestEngine_Rule6_LargeCounterPrecision(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := float64(1 << 53)
	_, _ = e.Observe(delta.Observation{Key: key, TS: t0, Value: v})
	pt, _ := e.Observe(delta.Observation{Key: key, TS: t0.Add(60 * time.Second), Value: v + 60})
	require.NotNil(t, pt)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_NeverEmitsBothPointAndEvent(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := t0
	r2 := t0.Add(time.Hour)
	cases := []delta.Observation{
		{Key: key, TS: t0, Value: 100, StatsReset: &r1},
		{Key: key, TS: t0.Add(time.Minute), Value: 160, StatsReset: &r1},
		{Key: key, TS: t0.Add(2 * time.Minute), Value: 5, StatsReset: &r2},
		{Key: key, TS: t0.Add(3 * time.Minute), Value: 200, StatsReset: &r2},
		{Key: key, TS: t0.Add(4 * time.Minute), Value: 100, StatsReset: &r2},  // backwards
		{Key: key, TS: t0.Add(20 * time.Minute), Value: 500, StatsReset: &r2}, // gap
	}
	for _, o := range cases {
		pt, ev := e.Observe(o)
		require.False(t, pt != nil && ev != nil, "both point and event for %v", o)
	}
}

func TestEngine_NeverEmitsNegativeRate(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	key := mkKey("m")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r1 := t0
	// Include resets to ensure never negative
	cases := []struct {
		ts    time.Time
		val   float64
		reset *time.Time
	}{
		{t0, 100, &r1},
		{t0.Add(time.Minute), 160, &r1},
		{t0.Add(2 * time.Minute), 4, &r1}, // reset
		{t0.Add(3 * time.Minute), 10, &r1},
		{t0.Add(4 * time.Minute), 10, &r1}, // flat
	}
	for _, c := range cases {
		pt, _ := e.Observe(delta.Observation{Key: key, TS: c.ts, Value: c.val, StatsReset: c.reset})
		if pt != nil {
			require.GreaterOrEqual(t, pt.Rate, 0.0)
		}
	}
}

func TestEngine_SeriesAreIndependent(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	k1 := pgtype.SeriesKey{Metric: "m", Instance: uuid.New(), Database: "a", Labels: "x"}
	k2 := pgtype.SeriesKey{Metric: "m", Instance: uuid.New(), Database: "a", Labels: "y"}
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: k1, TS: t0, Value: 100})
	_, _ = e.Observe(delta.Observation{Key: k2, TS: t0, Value: 100})
	// reset on k1
	pt, ev := e.Observe(delta.Observation{Key: k1, TS: t0.Add(time.Minute), Value: 4})
	require.Nil(t, pt)
	require.NotNil(t, ev)
	// k2 normal
	pt, ev = e.Observe(delta.Observation{Key: k2, TS: t0.Add(time.Minute), Value: 160})
	require.NotNil(t, pt)
	require.Nil(t, ev)
	require.InDelta(t, 1.0, pt.Rate, 1e-9)
}

func TestEngine_Evict(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	k1 := mkKey("m1")
	k2 := mkKey("m2")
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = e.Observe(delta.Observation{Key: k1, TS: t0, Value: 100})
	_, _ = e.Observe(delta.Observation{Key: k2, TS: t0.Add(5 * time.Minute), Value: 100})
	n := e.Evict(t0.Add(3 * time.Minute))
	require.Equal(t, 1, n)
	// k1 evicted, should behave as rule1
	pt, ev := e.Observe(delta.Observation{Key: k1, TS: t0.Add(10 * time.Minute), Value: 200})
	require.Nil(t, pt)
	require.Nil(t, ev)
	// k2 still present
	pt, ev = e.Observe(delta.Observation{Key: k2, TS: t0.Add(6 * time.Minute), Value: 160})
	require.NotNil(t, pt)
	require.Nil(t, ev)
}

func TestEngine_CountByInstance(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	instA := uuid.New()
	instB := uuid.New()
	now := time.Now()

	keyA1 := pgtype.SeriesKey{Metric: "m1", Instance: instA, Database: "app"}
	keyA2 := pgtype.SeriesKey{Metric: "m2", Instance: instA, Database: "app"}
	keyB1 := pgtype.SeriesKey{Metric: "m1", Instance: instB, Database: "app"}

	require.Empty(t, e.CountByInstance())

	_, _ = e.Observe(delta.Observation{Key: keyA1, TS: now, Value: 1})
	_, _ = e.Observe(delta.Observation{Key: keyA2, TS: now, Value: 1})
	_, _ = e.Observe(delta.Observation{Key: keyB1, TS: now, Value: 1})

	counts := e.CountByInstance()
	require.Equal(t, 2, counts[instA])
	require.Equal(t, 1, counts[instB])

	n := e.Evict(now.Add(time.Second))
	require.Equal(t, 3, n)
	require.Empty(t, e.CountByInstance())
}

func TestEngine_Concurrent(t *testing.T) {
	t.Parallel()
	e := delta.New(delta.Options{})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := pgtype.SeriesKey{Metric: "m", Instance: uuid.New(), Database: "a", Labels: ""}
			// Use distinct keys per goroutine to avoid interference, but still concurrent map access
			t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			e.Observe(delta.Observation{Key: key, TS: t0, Value: 100})
			e.Observe(delta.Observation{Key: key, TS: t0.Add(time.Minute), Value: 160})
		}(i)
	}
	wg.Wait()
}

func FuzzEngine_NoNegativeRateNoPanic(f *testing.F) {
	f.Add(int64(100), int64(160), int64(0))
	f.Fuzz(func(t *testing.T, v1, v2 int64, sec int64) {
		e := delta.New(delta.Options{})
		key := pgtype.SeriesKey{Metric: "m", Instance: uuid.Nil, Database: "", Labels: ""}
		t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		pt1, _ := e.Observe(delta.Observation{Key: key, TS: t0, Value: float64(v1)})
		if pt1 != nil {
			require.GreaterOrEqual(t, pt1.Rate, 0.0)
		}
		ts := t0.Add(time.Duration(sec%1000) * time.Second)
		if ts.After(t0) {
			pt2, _ := e.Observe(delta.Observation{Key: key, TS: ts, Value: float64(v2)})
			if pt2 != nil {
				require.GreaterOrEqual(t, pt2.Rate, 0.0)
			}
		}
		_ = math.MaxFloat64 // ensure import used
	})
}
