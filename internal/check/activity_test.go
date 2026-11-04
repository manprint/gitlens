package check

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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

// activityMock answers the activity scrape: the grouped counts by the query
// they come from, empty sets for the other queries.
func activityMock(groups map[string][]activityGroup, connections []activityRow) *mockConn {
	return &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			key := ""
			switch {
			case strings.Contains(sql, "wait_event_type") && strings.Contains(sql, "max_xact_age"):
				var rows [][]any
				for _, r := range connections {
					rows = append(rows, []any{*r.State, *r.WaitEventType, r.Count, r.MaxXactAge, r.MaxIdleInTxn})
				}
				return &mockRows{rows: rows}, nil
			case strings.Contains(sql, "row_number() OVER"):
				return &mockRows{}, nil
			case strings.Contains(sql, "THEN 'pglens' ELSE"):
				key = "origin"
			case strings.Contains(sql, "COALESCE(usename"):
				key = "user"
			case strings.Contains(sql, "host(client_addr)"):
				key = "client"
			case strings.Contains(sql, "COALESCE(application_name, '')"):
				key = "application"
			case strings.Contains(sql, "COALESCE(datname"):
				key = "database"
			}
			var rows [][]any
			for _, g := range groups[key] {
				rows = append(rows, []any{g.Label, g.Value})
			}
			return &mockRows{rows: rows}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(sql, "pg_prepared_xacts"):
				return &mockRow{vals: []any{float64(0), float64(0)}}
			case strings.Contains(sql, "datfrozenxid"):
				return &mockRow{vals: []any{float64(0)}}
			}
			return &mockRow{vals: []any{"100"}}
		},
	}
}

func scrapeActivity(t *testing.T, c *activityCheck, groups map[string][]activityGroup, connections []activityRow) Result {
	t.Helper()
	res, err := c.Scrape(context.Background(), maintenanceTarget(activityMock(groups, connections)))
	require.NoError(t, err)
	return res
}

func metricsNamed(res Result, name string) map[string]float64 {
	out := map[string]float64{}
	for _, m := range res.Metrics {
		if m.Name != name {
			continue
		}
		for _, v := range m.Labels {
			out[v] = m.Value
		}
		if len(m.Labels) == 0 {
			out[""] = m.Value
		}
	}
	return out
}

func TestActivity_ByApplicationEnabledByDefault(t *testing.T) {
	c := newActivityCheck()
	require.True(t, c.ByApplicationEnabled())
	res := scrapeActivity(t, c, map[string][]activityGroup{"application": {{Label: "api", Value: 3}}}, nil)
	require.Equal(t, map[string]float64{"api": 3}, metricsNamed(res, "pg_connections_by_application"))
}

// ACT-002: an explicit false hides only the per-application counts.
func TestActivity_ByApplicationExplicitFalse(t *testing.T) {
	c := newActivityCheck()
	c.SetByApplication(false)
	require.False(t, c.ByApplicationEnabled())
	res := scrapeActivity(t, c, map[string][]activityGroup{
		"application": {{Label: "api", Value: 3}},
		"database":    {{Label: "app", Value: 3}},
		"user":        {{Label: "u", Value: 3}},
		"client":      {{Label: "10.0.0.1", Value: 3}},
		"origin":      {{Label: "application", Value: 3}},
	}, nil)
	require.Empty(t, metricsNamed(res, "pg_connections_by_application"))
	for _, name := range []string{"pg_connections_by_database", "pg_connections_by_user", "pg_connections_by_client", "pg_connections_by_origin"} {
		require.NotEmpty(t, metricsNamed(res, name), name)
	}
}

// ACT-001: by_user and by_client keep the top 10 and sum the rest into other.
func TestActivity_ByUserAndClientAreBounded(t *testing.T) {
	var users, clients []activityGroup
	for i := 0; i < 13; i++ {
		users = append(users, activityGroup{Label: string(rune('a' + i)), Value: float64(100 - i)})
		clients = append(clients, activityGroup{Label: "10.0.0." + string(rune('a'+i)), Value: float64(100 - i)})
	}
	res := scrapeActivity(t, newActivityCheck(), map[string][]activityGroup{"user": users, "client": clients}, nil)
	for name, label := range map[string]string{"pg_connections_by_user": "usename", "pg_connections_by_client": "client_addr"} {
		got := 0
		var other float64
		for _, m := range res.Metrics {
			if m.Name != name {
				continue
			}
			got++
			require.Contains(t, m.Labels, label)
			if m.Labels[label] == "other" {
				other = m.Value
			}
		}
		require.Equal(t, 11, got, name)
		require.Equal(t, float64(90+89+88), other, name)
	}
}

// SORIGIN-002: both origins are always emitted, zero included, and sum to the
// connection total.
func TestSORIGIN002_OriginMetricsEmitBothOrigins(t *testing.T) {
	res := scrapeActivity(t, newActivityCheck(),
		map[string][]activityGroup{"origin": {{Label: "application", Value: 4}}},
		[]activityRow{{State: strPtr("idle"), WaitEventType: strPtr("Client"), Count: 4}})
	got := metricsNamed(res, "pg_connections_by_origin")
	require.Equal(t, map[string]float64{"application": 4, "pglens": 0}, got)
	require.Equal(t, got["application"]+got["pglens"], metricValue(res, "pg_connections_used", nil))

	res = scrapeActivity(t, newActivityCheck(), nil, nil)
	require.Equal(t, map[string]float64{"application": 0, "pglens": 0}, metricsNamed(res, "pg_connections_by_origin"))

	res = scrapeActivity(t, newActivityCheck(),
		map[string][]activityGroup{"origin": {{Label: "pglens", Value: 2}, {Label: "application", Value: 5}}},
		[]activityRow{{State: strPtr("idle"), WaitEventType: strPtr("Client"), Count: 7}})
	got = metricsNamed(res, "pg_connections_by_origin")
	require.Equal(t, map[string]float64{"application": 5, "pglens": 2}, got)
	require.Equal(t, 7.0, metricValue(res, "pg_connections_used", nil))
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

// TestActivity_WaitingRatioIgnoresIdleSessions keeps idle pooled connections,
// which all wait on ClientRead, from reading as contention.
func TestActivity_WaitingRatioIgnoresIdleSessions(t *testing.T) {
	str := func(s string) *string { return &s }
	rows := []activityRow{
		{State: str("idle"), WaitEventType: str("Client"), Count: 90},
		{State: str("active"), WaitEventType: str("CPU"), Count: 3},
		{State: str("active"), WaitEventType: str("Lock"), Count: 1},
	}
	if got := waitingRatio(rows); got != 0.25 {
		t.Fatalf("waitingRatio = %v, want 0.25 (1 of 4 active sessions)", got)
	}
	if got := waitingRatio(rows[:1]); got != 0 {
		t.Fatalf("waitingRatio with only idle sessions = %v, want 0", got)
	}
}

// A sub-query that fails fails the scrape. Its value read as zero before, and
// a zero frozen-XID age reads as a healthy cluster while the wraparound rule
// has nothing to judge.
func TestActivity_FailedSubQueryFailsTheScrape(t *testing.T) {
	t.Parallel()
	boom := errors.New("canceling statement due to statement timeout")
	for _, tc := range []struct{ name, query, queryRow string }{
		{"control", "", ""},
		{"database counts", "SELECT COALESCE(datname, '')", ""},
		{"application counts", "SELECT COALESCE(application_name, '')", ""},
		{"xmin holders", "row_number() OVER", ""},
		{"state ages", "COALESCE(state, 'unknown'), COALESCE(max(", ""},
		{"max_connections", "", "current_setting('max_connections')"},
		{"prepared transactions", "", "FROM pg_prepared_xacts"},
		{"frozen XID age", "", "FROM pg_database"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := &mockConn{
				queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
					if tc.query != "" && strings.Contains(sql, tc.query) {
						return nil, boom
					}
					return &mockRows{}, nil
				},
				queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
					switch {
					case tc.queryRow != "" && strings.Contains(sql, tc.queryRow):
						return &mockRow{err: boom}
					case strings.Contains(sql, "pg_prepared_xacts"):
						return &mockRow{vals: []any{float64(0), float64(0)}}
					case strings.Contains(sql, "datfrozenxid"):
						return &mockRow{vals: []any{float64(0)}}
					}
					return &mockRow{vals: []any{"100"}}
				},
			}
			_, err := (&activityCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
			if tc.query == "" && tc.queryRow == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, boom)
		})
	}
}
