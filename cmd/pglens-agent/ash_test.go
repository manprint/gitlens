package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/ash"
	"github.com/manprint/pglens/internal/clock"
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

// A window reports the error of its own failed tick. The error is taken with the
// tick and closed with the window, so a window with no failed tick reports none,
// and an error from an earlier window does not carry over.
func TestASHTicks_WindowReportsItsOwnFailure(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	ticks := &ashTicks{agg: ash.New(clk, ashWindow, ashMaxKeys)}
	errA := errors.New("statement timeout")
	errB := errors.New("connection reset")

	ticks.tick(false, errA)
	ticks.tick(false, errB)
	w, err := ticks.flush(clk.Now())
	require.Equal(t, 2, w.FailedTicks)
	require.ErrorIs(t, err, errB, "the window reports its latest failed tick")

	ticks.tick(true, nil)
	w, err = ticks.flush(clk.Now())
	require.Equal(t, 1, w.Ticks)
	require.NoError(t, err, "a window with no failed tick reports none")

	ticks.tick(false, errA)
	w, err = ticks.flush(clk.Now())
	require.Equal(t, 0, w.Ticks)
	require.ErrorIs(t, err, errA)
}

// ashMetricsFor runs rows through the dimension filter of cfg, one window of
// the aggregator and the wire conversion.
func ashMetricsFor(t *testing.T, cfg agent.CheckConfig, rows ...ash.SampleRow) []map[string]string {
	t.Helper()
	agg := ash.New(clock.NewFake(time.Unix(0, 0)), ashWindow, ashMaxKeys)
	add := ashRowFilter(cfg, agg.Add)
	for _, r := range rows {
		add(r)
	}
	agg.TickSucceeded()
	var labels []map[string]string
	for _, m := range ashWindowToMetrics(agg.Flush(time.Unix(10, 0))) {
		labels = append(labels, m.Labels)
	}
	return labels
}

func dimRow(user, addr, app string) ash.SampleRow {
	return ash.SampleRow{Datname: "app", State: "active", WaitEventType: "CPU", WaitEvent: "CPU",
		Usename: user, ClientAddr: addr, ApplicationName: app}
}

// ASH-D-004: by default a row with values carries all three labels; an empty
// value carries none.
func TestASHD004_DefaultEmitsAllDimensions(t *testing.T) {
	labels := ashMetricsFor(t, agent.CheckConfig{}, dimRow("alice", "10.0.0.7", "shop"))
	require.Len(t, labels, 1)
	require.Equal(t, "alice", labels[0]["usename"])
	require.Equal(t, "10.0.0.7", labels[0]["client_addr"])
	require.Equal(t, "shop", labels[0]["application_name"])

	labels = ashMetricsFor(t, agent.CheckConfig{}, dimRow("", "", ""))
	for _, k := range []string{"usename", "client_addr", "application_name"} {
		require.NotContains(t, labels[0], k)
	}
}

// ASH-D-005: dimensions [usename] keeps only usename; rows that differ only by
// client_addr merge into one metric.
func TestASHD005_OnlyListedDimensions(t *testing.T) {
	cfg := agent.CheckConfig{Dimensions: &[]string{"usename"}}
	labels := ashMetricsFor(t, cfg, dimRow("alice", "10.0.0.7", "shop"), dimRow("alice", "10.0.0.8", "shop"))
	require.Len(t, labels, 1)
	require.Equal(t, "alice", labels[0]["usename"])
	require.NotContains(t, labels[0], "client_addr")
	require.NotContains(t, labels[0], "application_name")
}

// ASH-D-007: dimensions [] turns all three off.
func TestASHD007_EmptyListTurnsAllOff(t *testing.T) {
	labels := ashMetricsFor(t, agent.CheckConfig{Dimensions: &[]string{}}, dimRow("alice", "10.0.0.7", "shop"))
	require.Len(t, labels, 1)
	for _, k := range []string{"usename", "client_addr", "application_name"} {
		require.NotContains(t, labels[0], k)
	}
}
