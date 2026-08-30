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

func TestActivity_ConnectionsUsedRatio(t *testing.T) {
	res := activityCheckResult([]activityRow{{State: strPtr("active"), WaitEventType: strPtr("CPU"), Count: 25}}, "100", false, nil, nil, nil, 0, 0, 0)
	for _, m := range res.Metrics {
		if m.Name == "pg_connections_used_ratio" {
			require.Equal(t, .25, m.Value)
			return
		}
	}
	t.Fatal("connection ratio missing")
}

func TestActivity_StateAgeClampedAtZero(t *testing.T) {
	res := activityCheckResult(nil, "", false, nil, nil, []activityGroup{{Label: "active", Value: -0.000019}}, 0, 0, 0)
	require.Equal(t, 0.0, metricValue(res, "pg_max_state_age_seconds", map[string]string{"state": "active"}))
}

func TestActivity_ByDatabaseIsBounded(t *testing.T) {
	rows := make([]activityGroup, 12)
	for i := range rows {
		rows[i] = activityGroup{Label: string(rune('a' + i)), Value: 1}
	}
	metrics := boundedActivityMetrics("pg_connections_by_database", "datname", rows)
	require.Len(t, metrics, 11)
}

func TestActivity_ByApplicationDisabledByDefault(t *testing.T) {
	res := activityCheckResult(nil, "", false, nil, []activityGroup{{Label: "api", Value: 1}}, nil, 0, 0, 0)
	for _, m := range res.Metrics {
		require.NotEqual(t, "pg_connections_by_application", m.Name)
	}
}

func TestActivity_PreparedXactsZeroWhenEmpty(t *testing.T) {
	res := activityCheckResult(nil, "", false, nil, nil, nil, 0, 0, 0)
	require.Equal(t, 0.0, metricValue(res, "pg_prepared_xacts", nil))
	require.Equal(t, 0.0, metricValue(res, "pg_oldest_prepared_xact_seconds", nil))
}

func TestActivity_MaxDatfrozenxidAge(t *testing.T) {
	res := activityCheckResult(nil, "", false, nil, nil, nil, 0, 0, 123)
	require.Equal(t, 123.0, metricValue(res, "pg_max_datfrozenxid_age", nil))
}

func TestActivity_ExistingMetricsUnchanged(t *testing.T) {
	res := buildActivityResult([]activityRow{{State: strPtr("active"), WaitEventType: strPtr("CPU"), Count: 1}}, "10")
	for _, name := range []string{"pg_backends", "pg_max_xact_age_seconds", "pg_max_idle_in_transaction_seconds", "pg_connections_used", "pg_connections_limit"} {
		found := false
		for _, m := range res.Metrics {
			if m.Name == name {
				found = true
				break
			}
		}
		require.True(t, found, name)
	}
}

func strPtr(s string) *string { return &s }
