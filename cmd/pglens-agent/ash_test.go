package main

import (
	"testing"

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
