//go:build integration

package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTPROC001: against a real server, two host intervals ship the
// postmaster and the agent's own backend with their sessions. It needs the
// container's processes in the test's /proc (tests on the Docker host).
func TestINTPROC001_RealProcessesJoinSessions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mgr, err := agent.NewManager(ctx, "proc-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		src := host.NewCollector("/proc", "/sys")
		backend, err := mgr.BackendPID(ctx)
		require.NoError(t, err)
		if name, err := src.ProcessName(backend); err != nil || !isPostgresProcess(name) {
			t.Skipf("container processes not visible in /proc (pid %d: %q, %v)", backend, name, err)
		}

		var log bytes.Buffer
		tr := &processTracker{target: "proc-target", src: src, q: managerProcessQuerier{mgr}, topN: 20, warn: newHourlyWarner(&log, time.Now)}
		t0 := time.Now()
		_, shipped := tr.tick(ctx, t0)
		require.False(t, shipped, "the first interval is a baseline")
		time.Sleep(time.Second)
		r, shipped := tr.tick(ctx, t0.Add(time.Second))
		require.True(t, shipped, log.String())
		require.Equal(t, wire.HostProcessesCheck, r.Check)

		types := map[string]bool{}
		var own bool
		for _, f := range r.Facts {
			require.NoError(t, f.Validate())
			types[f.Labels["backend_type"]] = true
			if f.Key == itoa(backend) {
				own = f.Labels["backend_type"] == "client backend" && f.Labels["datname"] == "postgres"
			}
		}
		require.True(t, types["postmaster"], "postmaster missing: %v", types)
		require.True(t, types["checkpointer"], "checkpointer missing: %v", types)
		require.True(t, own, "the agent's backend is joined to its session")
		require.Empty(t, log.String())
	})
}
