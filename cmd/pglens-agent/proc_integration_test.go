//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTPROC001: against a real server, two host intervals ship the
// postmaster and the agent's own backend with their sessions. The shared
// pgtest containers run in their own PID namespace, so their processes are
// not in the test's /proc and those cases skip; the host-pid case runs
// PostgreSQL in the Docker host's PID namespace and must not skip.
func TestINTPROC001_RealProcessesJoinSessions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		checkRealProcesses(t, pg.DSN("postgres", pgtest.RoleT0), true)
	})
	t.Run("host-pid", func(t *testing.T) {
		ctx := context.Background()
		c, err := postgres.Run(ctx, pgtest.ImageFor(18),
			postgres.WithDatabase("postgres"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("postgres"),
			testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) { hc.PidMode = "host" }),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Terminate(context.Background()) })
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		require.NoError(t, err)
		checkRealProcesses(t, dsn, false)
	})
}

func checkRealProcesses(t *testing.T, dsn string, mayBeHidden bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := agent.NewManager(ctx, "proc-target", dsn, agent.DefaultConnOptions(), nil)
	require.NoError(t, err)
	defer mgr.Close()

	src := host.NewCollector("/proc", "/sys")
	backend, err := mgr.BackendPID(ctx)
	require.NoError(t, err)
	if name, err := src.ProcessName(backend); err != nil || !isPostgresProcess(name) {
		if !mayBeHidden {
			t.Fatalf("backend pid %d not visible in /proc: %q, %v", backend, name, err)
		}
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
		// Only session labels and numbers: no command line, no path.
		for k, v := range f.Labels {
			require.Contains(t, []string{"pid", "backend_type", "datname", "usename", "application_name", "state", "queryid"}, k)
			require.False(t, strings.HasPrefix(v, "/"), "label %s is a path: %q", k, v)
		}
		var body map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(f.ValueJSON, &body))
		for k := range body {
			require.Contains(t, []string{"cpu_ratio", "rss_bytes", "read_bytes_per_s", "write_bytes_per_s"}, k)
		}
		if f.Key == itoa(backend) {
			own = f.Labels["backend_type"] == "client backend" && f.Labels["datname"] == "postgres"
		}
	}
	require.True(t, types["postmaster"], "postmaster missing: %v", types)
	require.True(t, types["checkpointer"], "checkpointer missing: %v", types)
	require.True(t, own, "the agent's backend is joined to its session")
	require.Empty(t, log.String())
}
