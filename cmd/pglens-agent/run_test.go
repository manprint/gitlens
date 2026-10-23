package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

// unreachableManager is a target whose server refuses every connection, so a
// readiness wait on it runs until its window ends.
func unreachableManager(t *testing.T, name string) *agent.Manager {
	t.Helper()
	dsn := "postgres://pglens@127.0.0.1:1/postgres?sslmode=disable"
	mgr, err := agent.NewManager(context.Background(), name, dsn, agent.DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	t.Cleanup(mgr.Close)
	return mgr
}

// TestWaitForTargets_UnreachableTargetsWaitTogether: three targets that never
// become readable take one readiness window, not one window each. Waited for in
// turn, a 1s window takes at least 3s.
func TestWaitForTargets_UnreachableTargetsWaitTogether(t *testing.T) {
	managers := []*agent.Manager{
		unreachableManager(t, "down-a"),
		unreachableManager(t, "down-b"),
		unreachableManager(t, "down-c"),
	}

	start := time.Now()
	waitForTargets(context.Background(), managers, time.Second)

	require.Less(t, time.Since(start), 2*time.Second)
}

// TestWaitForTarget_EndsWhenContextIsCancelled: a wait ends as soon as its
// context is cancelled, rather than running out its whole window.
func TestWaitForTarget_EndsWhenContextIsCancelled(t *testing.T) {
	mgr := unreachableManager(t, "down")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	start := time.Now()
	waitForTarget(ctx, mgr, 30*time.Second)

	require.Less(t, time.Since(start), 5*time.Second)
}

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

// TestFlushEnvelopeBoundsAnUnreachableTarget: a flush must not hold an
// unreachable target for longer than its refresh timeout. The shutdown flush
// runs under the stop grace of the container runtime, which is 10s by default,
// so a hung target that also waited out the capability refresh and the
// extension check would outlast it.
func TestFlushEnvelopeBoundsAnUnreachableTarget(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	target := newHungTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := agent.NewManager(ctx, "hung", target.dsn, agent.DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	t.Cleanup(mgr.Close)

	buf, err := buffer.Open(filepath.Join(t.TempDir(), "buffer"), buffer.Options{MaxSize: 1 << 20, MaxAge: time.Hour, Clock: clock.System()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = buf.Close() })
	pusher := agent.NewPusher("http://127.0.0.1:1/", "token", clock.System())
	pusher.SetBuffer(buf)

	var mu sync.Mutex
	pending := map[string][]wire.Result{"hung": {{Check: "probe", TS: time.Now()}}}
	began := time.Now()
	flushEnvelope(ctx, []*agent.Manager{mgr}, &mu, pending, &edgeStates{byName: map[string]edgeState{}}, map[string]bool{}, map[string]map[string]bool{}, pusher)
	require.Less(t, time.Since(began), 10*time.Second, "a flush waited longer than the stop grace for one unreachable target")
}
