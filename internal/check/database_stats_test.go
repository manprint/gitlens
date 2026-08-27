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
	require.Len(t, res.Metrics, 16) // 8 metrics per db *2
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
