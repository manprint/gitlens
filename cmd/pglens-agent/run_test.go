package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

// TestAgentSelfResult_ShipsTheBufferDropRate — pglens_samples_dropped_rate is
// the metric the agent_buffer_full Tier 0 rule compares, and before this
// nothing produced it anywhere in the system.
func TestAgentSelfResult_ShipsTheBufferDropRate(t *testing.T) {
	pusher := agent.NewPusher("http://unused.invalid", "token", clock.System())
	now := time.Unix(1000, 0)

	r := agentSelfResult(pusher, now)
	require.Equal(t, "agent_self", r.Check)
	require.Equal(t, now, r.TS)
	require.Len(t, r.Metrics, 1)
	require.Equal(t, "pglens_samples_dropped_rate", r.Metrics[0].Name)
	require.Equal(t, "gauge", r.Metrics[0].Kind)
	require.Zero(t, r.Metrics[0].Value)
	// A gauge, so the alert rule also sees the sample that resolves it.
	require.Empty(t, r.Error)
	require.Empty(t, r.SkipReason)
}

// A nil pusher must not panic: the result is simply empty.
func TestAgentSelfResult_NilPusher(t *testing.T) {
	r := agentSelfResult(nil, time.Unix(1, 0))
	require.Equal(t, "agent_self", r.Check)
	require.Empty(t, r.Metrics)
}

func TestHostWireResult_OmitsUnavailableFieldsAndDerivesFreeRatio(t *testing.T) {
	total := uint64(1000)
	free := uint64(250)
	now := time.Unix(1000, 0)

	r := hostWireResult(host.Sample{DiskTotalBytes: &total, DiskFreeBytes: &free, Source: "cgroup_v2"}, now)
	require.Equal(t, "host", r.Check)
	require.Equal(t, now, r.TS)

	got := map[string]float64{}
	for _, m := range r.Metrics {
		require.Equal(t, "gauge", m.Kind, m.Name)
		got[m.Name] = m.Value
	}
	require.Equal(t, 1000.0, got["host_disk_total_bytes"])
	require.Equal(t, 250.0, got["host_disk_free_bytes"])
	// host_disk_free_ratio is what the seeded disk.free_low rule compares.
	require.Equal(t, 0.25, got["host_disk_free_ratio"])
	require.Equal(t, 2.0, got["host_metrics_source"])
	require.NotContains(t, got, "host_mem_total_bytes", "an unavailable field must be omitted, not reported as zero")
}

func TestHostWireResult_NoDiskTotalMeansNoRatio(t *testing.T) {
	zero := uint64(0)
	free := uint64(250)
	r := hostWireResult(host.Sample{DiskTotalBytes: &zero, DiskFreeBytes: &free, Source: "host"}, time.Unix(1, 0))
	for _, m := range r.Metrics {
		require.NotEqual(t, "host_disk_free_ratio", m.Name, "a zero total would make the ratio a division by zero")
	}
}

// TestCheckDatabaseSummary_AppliesTheAgentSelection — the check command
// used to call every connectable database monitored, while the agent
// excludes managed-service databases and keeps only the ten busiest.
func TestCheckDatabaseSummary_AppliesTheAgentSelection(t *testing.T) {
	candidates := []agent.DatabaseCandidate{{Name: "rdsadmin", XactCommit: 9000}}
	for i := range 11 {
		candidates = append(candidates, agent.DatabaseCandidate{Name: fmt.Sprintf("app%02d", i), XactCommit: int64(1000 - i)})
	}
	databases := agent.SelectDatabases(candidates, agent.DefaultConnOptions())

	require.Equal(t, "12 found, 10 monitored (1 excluded; 1 skipped: 1 db_budget), default selection",
		formatDatabaseSummary(len(candidates), databases))
	require.Equal(t, "1 found, 1 monitored, default selection",
		formatDatabaseSummary(1, agent.SelectDatabases(candidates[1:2], agent.DefaultConnOptions())))
}

// TestReportScheduled_MonitoredMeansCollected — the selection re-runs every
// envelope, the schedule does not: a database created after startup is not
// collected, and one pushed out of the budget after startup still is.
func TestReportScheduled_MonitoredMeansCollected(t *testing.T) {
	scheduled := map[string]bool{"app": true, "old": true}
	got := reportScheduled([]wire.Database{
		{Name: "app", Monitored: true},
		{Name: "created_later", Monitored: true},
		{Name: "old", Monitored: false, SkipReason: "db_budget"},
		{Name: "never", Monitored: false, SkipReason: "db_budget"},
	}, scheduled)

	require.Equal(t, []wire.Database{
		{Name: "app", Monitored: true},
		{Name: "created_later", Monitored: false, SkipReason: "discovered_after_start"},
		{Name: "old", Monitored: true},
		{Name: "never", Monitored: false, SkipReason: "db_budget"},
	}, got)
}
