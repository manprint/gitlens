package check

import (
	"context"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func stringp(v string) *string { return &v }

func scrapeFunctionStatsForTest(t *testing.T, c *functionStatsCheck, setting string, plFunctions int64, stats [][]any) Result {
	t.Helper()
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			require.Equal(t, functionTrackingQuery, sql)
			return &mockRow{vals: []any{setting, plFunctions}}
		},
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			require.Equal(t, functionStatsQuery, sql)
			return &mockRows{rows: stats}, nil
		},
	}
	r, err := c.Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func TestFunctionStats_Requirements(t *testing.T) {
	c := &functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	require.Equal(t, "function_stats", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 5*time.Minute, c.DefaultInterval())
}

// Reading statistics must never reset them, for pglens or anyone else.
func TestFunctionStats_QueriesNeverReset(t *testing.T) {
	reset := regexp.MustCompile(`(?i)\bpg_stat_reset`)
	require.False(t, reset.MatchString(functionTrackingQuery))
	require.False(t, reset.MatchString(functionStatsQuery))
	require.False(t, reset.MatchString(securityDefinerQuery))
}

// Overloads are separate series: with the name alone, f(integer) and
// f(text) would be one series written twice for the same instant.
func TestFunctionStats_OverloadsAreSeparateSeries(t *testing.T) {
	c := &functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	r := scrapeFunctionStatsForTest(t, c, "pl", 2, [][]any{
		{uint32(10), "public", "f", "x integer", "plpgsql", int64(5), 50.0, 40.0},
		{uint32(11), "public", "f", "x text", "plpgsql", int64(7), 70.0, 70.0},
	})
	series := map[string]float64{}
	for _, m := range r.Metrics {
		key := m.Name + "|" + pgtype.CanonicalLabels(m.Labels)
		_, dup := series[key]
		require.False(t, dup, "series %s reported twice", key)
		series[key] = m.Value
	}
	require.Len(t, series, 7)
	require.Equal(t, 70.0, series["pg_function_self_time_ms_total|"+pgtype.CanonicalLabels(map[string]string{
		"schemaname": "public", "function": "f", "arguments": "x text", "language": "plpgsql"})])
	require.Equal(t, 2.0, series["pg_functions_tracked|"])
	require.False(t, r.Partial)
	require.Equal(t, []Fact{{Kind: functionTrackingFactKind, Key: "track_functions", ValueText: "pl",
		Labels: map[string]string{"tracked": "2", "pl_functions": "2"}}}, r.Facts)
}

// Without tracking the view is empty: the fact says so, rather than the
// check reporting nothing.
func TestFunctionStats_UntrackedStillReportsTheSetting(t *testing.T) {
	c := &functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50})}
	r := scrapeFunctionStatsForTest(t, c, "none", 3, nil)
	require.Equal(t, "none", r.Facts[0].ValueText)
	require.Equal(t, "0", r.Facts[0].Labels["tracked"])
	require.Equal(t, "3", r.Facts[0].Labels["pl_functions"])
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_functions_tracked", Value: 0, Kind: pgtype.KindGauge})
}

// The functions with the most self time and the most calls are reported;
// the rest make the result a deliberate subset.
func TestFunctionStats_TopNIsPartial(t *testing.T) {
	c := &functionStatsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 2})}
	var stats [][]any
	for i := range 6 {
		stats = append(stats, []any{uint32(100 + i), "public", fmt.Sprintf("f%d", i), "", "plpgsql", int64(i), float64(i), float64(10 - i)})
	}
	r := scrapeFunctionStatsForTest(t, c, "pl", 6, stats)
	require.True(t, r.Partial)
	require.False(t, r.Truncated)
	names := map[string]bool{}
	for _, m := range r.Metrics {
		if m.Labels["function"] != "" {
			names[m.Labels["function"]] = true
		}
	}
	// f0 and f1 by self time, f4 and f5 by calls.
	require.Equal(t, map[string]bool{"f0": true, "f1": true, "f4": true, "f5": true}, names)
}

func TestFunctionSecurity_Requirements(t *testing.T) {
	c := &functionSecurityCheck{}
	require.Equal(t, "function_security", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
}

func TestFunctionSecurity_Scrape(t *testing.T) {
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		require.Equal(t, securityDefinerQuery, sql)
		require.Equal(t, []any{securityDefinerFactLimit + 1}, args)
		return &mockRows{rows: [][]any{
			{"public.grant_all(r text)", "public", "grant_all", "r text", "function", "plpgsql", "postgres", true, (*string)(nil), true},
			{"app.audit()", "app", "audit", "", "procedure", "sql", "app_owner", false, stringp("app, pg_temp"), false},
			{"app.lookup(k integer)", "app", "lookup", "k integer", "function", "plpgsql", "app_owner", false, stringp(`"$user", public`), true},
		}}, nil
	}}
	r, err := (&functionSecurityCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.False(t, r.Truncated)
	require.Len(t, r.Facts, 3)
	exposed := r.Facts[0]
	require.Equal(t, "unpinned", exposed.ValueText)
	require.Equal(t, "public.grant_all(r text)", exposed.Key)
	require.Equal(t, map[string]string{
		"schemaname": "public", "name": "grant_all", "arguments": "r text", "kind": "function", "language": "plpgsql",
		"owner": "postgres", "owner_superuser": "true", "public_execute": "true", "search_path": "", "pg_temp_last": "false",
	}, exposed.Labels)
	require.Equal(t, "pinned", r.Facts[1].ValueText)
	require.Equal(t, "true", r.Facts[1].Labels["pg_temp_last"])
	require.Equal(t, "false", r.Facts[2].Labels["pg_temp_last"])
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_security_definer_functions_unpinned", Value: 1, Kind: pgtype.KindGauge})
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_security_definer_functions_exposed", Value: 1, Kind: pgtype.KindGauge})
}

func TestFunctionSecurity_TruncatedAboveLimit(t *testing.T) {
	var rows [][]any
	for i := range securityDefinerFactLimit + 1 {
		rows = append(rows, []any{fmt.Sprintf("public.f%d()", i), "public", fmt.Sprintf("f%d", i), "", "function", "sql", "o", false, (*string)(nil), false})
	}
	conn := &mockConn{queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) { return &mockRows{rows: rows}, nil }}
	r, err := (&functionSecurityCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.True(t, r.Truncated)
	require.Len(t, r.Facts, securityDefinerFactLimit)
}

func TestPgTempLast(t *testing.T) {
	for path, want := range map[string]bool{
		"app, pg_temp":     true,
		`app, "pg_temp"`:   true,
		"pg_temp, app":     false,
		"app":              false,
		`""`:               false,
		`"$user", public`:  false,
		"app,pg_temp":      true,
		"pg_catalog, app ": false,
	} {
		require.Equal(t, want, pgTempLast(path), path)
	}
}
