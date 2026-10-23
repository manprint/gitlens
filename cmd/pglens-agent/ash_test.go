package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/ash"
)

// An idle window still ticked: it must reach the server as a zero-count
// record carrying its ticks, or its ticks never reach the average's denominator.
func TestASHWindowToMetrics_IdleWindowKeepsTicks(t *testing.T) {
	t.Parallel()
	metrics := ashWindowToMetrics(ash.Window{Ticks: 10})
	require.Len(t, metrics, 1)
	require.Equal(t, "ash_samples", metrics[0].Name)
	require.Zero(t, metrics[0].Value)
	require.Equal(t, "10", metrics[0].Labels["window_ticks"])
	require.Equal(t, "10", metrics[0].Labels["window_seconds"])
}

// A window where every tick failed has no evidence either way: it stays a gap.
func TestASHWindowToMetrics_FailedWindowIsGap(t *testing.T) {
	t.Parallel()
	require.Nil(t, ashWindowToMetrics(ash.Window{}))
}

// A window whose every tick failed reaches the server as a check error. Its
// absence would read as an idle instance, or as no agent at all.
func TestASHWindowResult_TotalFailureIsCheckError(t *testing.T) {
	t.Parallel()
	res, ok := ashWindowResult(ash.Window{FailedTicks: 10}, time.Unix(100, 0), errors.New("connection reset"))
	require.True(t, ok)
	require.Equal(t, "ash", res.Check)
	require.Empty(t, res.Metrics)
	require.Contains(t, res.Error, "none of the 10 sampling ticks")
	require.Contains(t, res.Error, "connection reset")
}

// A window where some ticks succeeded keeps its metrics and carries no error:
// window_ticks already says how much of the window was sampled.
func TestASHWindowResult_PartialFailureKeepsMetrics(t *testing.T) {
	t.Parallel()
	res, ok := ashWindowResult(ash.Window{Ticks: 4, FailedTicks: 6}, time.Unix(100, 0), errors.New("timeout"))
	require.True(t, ok)
	require.Empty(t, res.Error)
	require.Equal(t, "4", res.Metrics[0].Labels["window_ticks"])
}

// A window where no tick ran at all has nothing to report.
func TestASHWindowResult_NoTicksNoResult(t *testing.T) {
	t.Parallel()
	_, ok := ashWindowResult(ash.Window{}, time.Unix(100, 0), nil)
	require.False(t, ok)
}

// An idle window still ticked: it reports zero samples, not an error.
func TestASHWindowResult_IdleWindowIsZeroNotError(t *testing.T) {
	t.Parallel()
	res, ok := ashWindowResult(ash.Window{Ticks: 10}, time.Unix(100, 0), nil)
	require.True(t, ok)
	require.Empty(t, res.Error)
	require.Len(t, res.Metrics, 1)
	require.Zero(t, res.Metrics[0].Value)
}
