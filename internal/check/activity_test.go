package check

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildActivityResult(t *testing.T) {
	t.Parallel()
	// Empty result set
	res := buildActivityResult(nil, "")
	require.NotEmpty(t, res.Metrics) // should still have max_xact and max_idle
	found := false
	for _, m := range res.Metrics {
		if m.Name == "pg_backends" {
			found = true
		}
	}
	require.False(t, found) // empty rows => no pg_backends
	// Check max metrics are 0
	for _, m := range res.Metrics {
		if m.Name == "pg_max_xact_age_seconds" {
			require.Equal(t, 0.0, m.Value)
		}
	}

	// NULL state and wait_event_type
	rows := []activityRow{
		{State: nil, WaitEventType: nil, Count: 2, MaxXactAge: 10, MaxIdleInTxn: 5},
		{State: strPtr("active"), WaitEventType: strPtr("Lock"), Count: 1, MaxXactAge: 20, MaxIdleInTxn: 0},
	}
	res = buildActivityResult(rows, "100")
	require.NotEmpty(t, res.Metrics)
	// Check that NULL became unknown and CPU
	hasUnknown := false
	hasCPU := false
	for _, m := range res.Metrics {
		if m.Name == "pg_backends" {
			if m.Labels["state"] == "unknown" {
				hasUnknown = true
			}
			if m.Labels["wait_event_type"] == "CPU" {
				hasCPU = true
			}
		}
	}
	require.True(t, hasUnknown)
	require.True(t, hasCPU)
	// Check max values
	for _, m := range res.Metrics {
		if m.Name == "pg_max_xact_age_seconds" {
			require.Equal(t, 20.0, m.Value)
		}
		if m.Name == "pg_max_idle_in_transaction_seconds" {
			require.Equal(t, 5.0, m.Value)
		}
		if m.Name == "pg_connections_limit" {
			require.Equal(t, 100.0, m.Value)
		}
	}
}

func strPtr(s string) *string { return &s }
