package check

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildInstanceInfoResult(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sysID := "7381927364512345678"
	addr := "127.0.0.1"
	port := 5432
	res := buildInstanceInfoResult(&sysID, false, 160004, start, &addr, &port, now)
	require.NotEmpty(t, res.Metrics)
	found := false
	for _, m := range res.Metrics {
		if m.Name == "pg_up" {
			found = true
			require.Equal(t, 1.0, m.Value)
			require.Equal(t, "system_identifier", m.Labels["cluster_id_source"])
		}
	}
	require.True(t, found)

	// Test with nil sysID (manual fallback)
	res2 := buildInstanceInfoResult(nil, true, 160004, start, nil, nil, now)
	foundManual := false
	for _, m := range res2.Metrics {
		if m.Labels["cluster_id_source"] == "manual" {
			foundManual = true
		}
	}
	require.True(t, foundManual)

	// Test with empty result (no addr/port)
	require.NotEmpty(t, res2.Metrics)

	// Test uptime negative handling
	futureStart := now.Add(time.Hour)
	res3 := buildInstanceInfoResult(&sysID, false, 160004, futureStart, &addr, &port, now)
	for _, m := range res3.Metrics {
		if m.Name == "pg_uptime_seconds" {
			require.GreaterOrEqual(t, m.Value, 0.0)
		}
	}
}

func TestIsPermissionDenied(t *testing.T) {
	t.Parallel()
	require.True(t, isPermissionDenied(errWithCode("42501")))
	require.True(t, isPermissionDenied(errWithMsg("permission denied")))
	require.False(t, isPermissionDenied(nil))
}

func errWithCode(code string) error {
	return &testError{msg: "ERROR: insufficient_privilege (SQLSTATE " + code + ")"}
}
func errWithMsg(msg string) error {
	return &testError{msg: msg}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
