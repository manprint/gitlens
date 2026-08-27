package server

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/stretchr/testify/require"
)

func TestIsStaleUp_Inclusive(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	threshold := 90 * time.Second
	last := now.Add(-90 * time.Second)
	require.True(t, isStaleUp(last, now, threshold))
	last = now.Add(-91 * time.Second)
	require.False(t, isStaleUp(last, now, threshold))
	require.True(t, isStaleUp(now.Add(-30*time.Second), now, threshold))
	require.True(t, isStaleUp(now.Add(10*time.Second), now, threshold))
}

func TestNewStaleness_Defaults(t *testing.T) {
	t.Parallel()
	s := NewStaleness(nil, nil)
	require.NotNil(t, s)
	require.NotNil(t, s.clock)
	require.Equal(t, 30*time.Second, s.interval)
	require.NotNil(t, s.instanceUp)
	require.NotNil(t, s.clusterPrimarySeen)
	require.NotNil(t, s.clusterNoPrimaryEmitted)
	fc := clock.NewFake(time.Now())
	s2 := NewStaleness(nil, fc)
	require.Equal(t, fc, s2.clock)
}

func TestStaleness_Threshold(t *testing.T) {
	t.Parallel()
	s := NewStaleness(nil, nil)
	require.Equal(t, 90*time.Second, s.threshold())
	s.interval = 10 * time.Second
	require.Equal(t, 30*time.Second, s.threshold())
	s.interval = 0
	require.Equal(t, 90*time.Second, s.threshold())
}

func TestBoolToFloat(t *testing.T) {
	t.Parallel()
	require.Equal(t, 1.0, boolToFloat(true))
	require.Equal(t, 0.0, boolToFloat(false))
}

func TestGaugeVec(t *testing.T) {
	t.Parallel()
	g := newGaugeVec()
	g.Set("a", 1.5)
	require.Equal(t, 1.5, g.Get("a"))
	g.Set("a", 2.5)
	require.Equal(t, 2.5, g.Get("a"))
	require.Equal(t, 0.0, g.Get("missing"))
}

func TestCounterVec(t *testing.T) {
	t.Parallel()
	c := &counterVec{vals: make(map[string]float64)}
	c.Inc("a")
	require.Equal(t, 1.0, c.Get("a"))
	c.Add("a", 2)
	require.Equal(t, 3.0, c.Get("a"))
	c.Inc("b")
	require.Equal(t, 1.0, c.Get("b"))
}

func TestCounter(t *testing.T) {
	t.Parallel()
	c := &counter{}
	c.Inc()
	require.Equal(t, 1.0, c.Get())
	c.Add(5)
	require.Equal(t, 6.0, c.Get())
	c.Inc()
	require.Equal(t, 7.0, c.Get())
}

func TestGauge(t *testing.T) {
	t.Parallel()
	g := &gauge{}
	g.Set(3)
	require.Equal(t, 3.0, g.Get())
	g.Add(2)
	require.Equal(t, 5.0, g.Get())
	g.Set(0)
	require.Equal(t, 0.0, g.Get())
}

func TestIncHelpers(t *testing.T) {
	t.Parallel()
	IncIngestEnvelopes("ok")
	require.GreaterOrEqual(t, pglensIngestEnvelopesTotal.Get("ok"), 1.0)
	IncIngestRejected("bad")
	require.GreaterOrEqual(t, pglensIngestRejectedTotal.Get("bad"), 1.0)
	IncSamplesTooOld()
	require.GreaterOrEqual(t, pglensSamplesTooOldTotal.Get(), 1.0)
	ObserveAgentClockSkew(5.5)
	require.Equal(t, 5.5, pglensAgentClockSkewSeconds.Get())
	SetSeriesTotal("inst1", 100)
	require.Equal(t, 100.0, pglensSeriesTotalGauge.Get("inst1"))
	IncCardinalityTruncated()
	require.GreaterOrEqual(t, pglensCardinalityTruncatedTotal.Get(), 1.0)
	pglensAgentLastSeenGauge.Set("x", 123)
	require.Equal(t, 123.0, pglensAgentLastSeenGauge.Get("x"))
	pglensUpGauge.Set("y", 1)
	require.Equal(t, 1.0, pglensUpGauge.Get("y"))
	pglensIngestEnvelopesTotal.Add("ok", 2)
	require.GreaterOrEqual(t, pglensIngestEnvelopesTotal.Get("ok"), 3.0)
	pglensUpGauge.Add("y", 1)
}

func TestStaleness_Evaluate_NilPool(t *testing.T) {
	t.Parallel()
	s := NewStaleness(nil, clock.NewFake(time.Now()))
	require.NoError(t, s.Evaluate(context.Background()))
	require.NoError(t, s.ensureLeader(context.Background()))
	require.NoError(t, s.emitEvent(context.Background(), "t", nil, nil, nil))
}

func TestStaleness_EmitEvent_NilPool_NoPanic(t *testing.T) {
	t.Parallel()
	s := NewStaleness(nil, nil)
	require.NoError(t, s.emitEvent(context.Background(), "agent_down", nil, nil, map[string]any{"a": "b"}))
	require.NoError(t, s.emitEvent(context.Background(), "x", nil, nil, nil))
	err := s.emitEvent(context.Background(), "t", nil, nil, map[string]any{"bad": make(chan int)})
	require.NoError(t, err)
}

func TestStaleness_StartStop_Idempotence(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(time.Now())
	s := NewStaleness(nil, fc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	s.Start(ctx)
	s.Stop()
	s.Stop()
	s.Start(ctx)
}

func TestStaleness_EnsureLeader_NilPool(t *testing.T) {
	t.Parallel()
	s := NewStaleness(nil, nil)
	require.NoError(t, s.ensureLeader(context.Background()))
}
