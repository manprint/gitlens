package check

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildDatabaseStatsResult(t *testing.T) {
	t.Parallel()
	// Empty result
	res := buildDatabaseStatsResult(nil)
	require.Empty(t, res.Metrics)
	require.Nil(t, res.StatsReset)

	// Single row
	now := time.Now()
	rows := []dbStatRow{
		{Datname: "app", XactCommit: 100, XactRollback: 5, BlksRead: 10, BlksHit: 90, Deadlocks: 1, TempBytes: 1024, Conflicts: 0, Numbackends: 5, StatsReset: &now},
		{Datname: "postgres", XactCommit: 50, XactRollback: 2, BlksRead: 5, BlksHit: 40, Deadlocks: 0, TempBytes: 512, Conflicts: 1, Numbackends: 2, StatsReset: nil},
	}
	res = buildDatabaseStatsResult(rows)
	require.Len(t, res.Metrics, 20) // 8 legacy metrics + 2 lifetime ratios per db
	require.NotNil(t, res.StatsReset)
	require.Equal(t, now, *res.StatsReset)

	// Check labels
	found := false
	for _, m := range res.Metrics {
		if m.Name == "pg_xact_commit_total" && m.Labels["database"] == "app" {
			require.Equal(t, 100.0, m.Value)
			found = true
		}
	}
	require.True(t, found)

	// Zero value row
	zeroRows := []dbStatRow{
		{Datname: "test", XactCommit: 0, XactRollback: 0, BlksRead: 0, BlksHit: 0, Deadlocks: 0, TempBytes: 0, Conflicts: 0, Numbackends: 0, StatsReset: nil},
	}
	res = buildDatabaseStatsResult(zeroRows)
	require.NotEmpty(t, res.Metrics)
}

func TestDatabaseStats_RollbackRatio(t *testing.T) {
	res := buildDatabaseStatsResult([]dbStatRow{{Datname: "app", XactCommit: 75, XactRollback: 25}})
	for _, m := range res.Metrics {
		if m.Name == "pg_xact_rollback_ratio" {
			require.Equal(t, .25, m.Value)
			return
		}
	}
	t.Fatal("rollback ratio missing")
}

func TestDatabaseStats_HitRatio(t *testing.T) {
	res := buildDatabaseStatsResult([]dbStatRow{{Datname: "app", BlksRead: 25, BlksHit: 75}})
	for _, m := range res.Metrics {
		if m.Name == "pg_blks_hit_ratio" {
			require.Equal(t, .75, m.Value)
			return
		}
	}
	t.Fatal("hit ratio missing")
}

func TestDatabaseStats_RatiosOmittedWhenNoTraffic(t *testing.T) {
	res := buildDatabaseStatsResult([]dbStatRow{{Datname: "app"}})
	for _, m := range res.Metrics {
		require.NotEqual(t, "pg_xact_rollback_ratio", m.Name)
		require.NotEqual(t, "pg_blks_hit_ratio", m.Name)
	}
}

func TestDatabaseStats_ExistingMetricsUnchanged(t *testing.T) {
	res := buildDatabaseStatsResult([]dbStatRow{{Datname: "app", XactCommit: 1, BlksHit: 1}})
	for _, name := range []string{"pg_xact_commit_total", "pg_xact_rollback_total", "pg_blks_read_total", "pg_blks_hit_total", "pg_deadlocks_total", "pg_temp_bytes_total", "pg_conflicts_total", "pg_numbackends"} {
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
