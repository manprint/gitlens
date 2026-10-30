package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// WIRE-COV-004: the agent reports check_intervals from the schedule it built,
// with the configured interval where one is set and the check's default
// elsewhere, a database-scoped check once for all its databases; and it
// reports the same agent_started_at on every flush.
func TestWIRECOV004_AgentFillsCoverageFromSchedule(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`server: {url: http://localhost:8080, token: test}
identity_path: /tmp/id
buffer: {path: /tmp/buf}
targets: [{name: t1, dsn: "postgres://pglens@127.0.0.1:1/postgres?sslmode=disable"}]
checks: {settings: {interval: 10m}}
`), 0o600))
	cfg, err := agent.LoadConfig(path)
	require.NoError(t, err)
	mgr := unreachableManager(t, "t1")

	gate := targetGate{
		role: pgtype.RolePrimary, version: pgtype.PGVersion(180000), tier: pgtype.TierExtension,
		exts: map[string]bool{"pg_stat_statements": true, "pgstattuple": true, "postgis": true}, databases: map[string]bool{"app": true, "crm": true},
	}
	want := map[string]int{}
	for _, c := range check.All() {
		req := c.Requires()
		if ok, _ := req.Supports(gate.role, gate.version, gate.tier, gate.exts); !ok {
			continue
		}
		interval := c.DefaultInterval()
		if c.Name() == "settings" {
			interval = 10 * time.Minute
		}
		want[c.Name()] = int(interval / time.Second)
	}
	require.Equal(t, 600, want["settings"], "the test gate schedules the settings check")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	schedules := newScheduleSet(ctx, cfg, clock.System(), []*agent.Manager{mgr}, &mu, map[string][]wire.Result{}, map[string]map[string]bool{})
	schedules.schedMu.Lock()
	schedules.gates = map[string]targetGate{"t1": gate}
	require.NoError(t, schedules.buildLocked())
	schedules.schedMu.Unlock()
	t.Cleanup(schedules.stop)

	started := time.Date(2026, 10, 30, 8, 0, 0, 0, time.FixedZone("CET", 3600))
	coverage := agentCoverage{startedAt: started, intervals: schedules.checkIntervals}
	mu.Lock()
	first := coverage.snapshot()
	mu.Unlock()
	require.Equal(t, want, first.intervals["t1"])

	mu.Lock()
	second := coverage.snapshot()
	mu.Unlock()
	require.Equal(t, started.UTC(), *first.started())
	require.Equal(t, time.UTC, first.started().Location())
	require.Equal(t, *first.started(), *second.started(), "the start is the same on every push")
	require.Nil(t, agentCoverage{}.started(), "a zero start is not reported")
	require.Nil(t, agentCoverage{}.snapshot().intervals["t1"])
}
