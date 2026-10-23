//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTRUN001_ShutdownReturnsWhileASHHoldsAConnection is the SIGTERM path
// with the default config: the ASH sampler holds its dedicated connection when
// shutdown starts, and closing the target must not wait for it to be released.
func TestINTRUN001_ShutdownReturnsWhileASHHoldsAConnection(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mgr, err := agent.NewManager(ctx, "shutdown-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		stop, enabled := startASH(ctx, mgr, "shutdown-target", agent.CheckConfig{}, clock.System(), &mu, pending)
		require.True(t, enabled, "ASH must be enabled for the default config")

		// The dedicated connection is acquired on the sampler's first tick, so
		// wait until the server shows it before shutting down.
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		require.Eventually(t, func() bool {
			var n int
			err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name = 'pglens/ash'").Scan(&n)
			return err == nil && n > 0
		}, 10*time.Second, 50*time.Millisecond)

		cancel()
		done := make(chan struct{})
		go func() {
			closeTargets([]func(){stop}, []*agent.Manager{mgr})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			// Release the sampler so the blocked Close can return and the
			// container can be restored for the next test.
			stop()
			<-done
			t.Fatal("shutdown blocked on the ASH connection: closeTargets must stop the samplers before closing the managers")
		}
	})
}

// readEnvelope takes the one envelope the flush queued into buf.
func readEnvelope(t *testing.T, buf *buffer.Buffer) wire.Envelope {
	t.Helper()
	data, ack, err := buf.Next()
	require.NoError(t, err, "a flush must queue an envelope")
	require.NoError(t, ack(context.Background()))
	var env wire.Envelope
	require.NoError(t, json.Unmarshal(data, &env))
	return env
}

// newFlushBuffer opens the disk buffer a flush queues into, and a pusher that
// writes to it.
func newFlushBuffer(t *testing.T) (*buffer.Buffer, *agent.Pusher) {
	t.Helper()
	buf, err := buffer.Open(t.TempDir(), buffer.Options{MaxSize: 1 << 20, MaxAge: time.Hour, Clock: clock.System()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = buf.Close() })
	pusher := agent.NewPusher("http://127.0.0.1:1/", "token", clock.System())
	pusher.SetBuffer(buf)
	return buf, pusher
}

// TestINTRUN002_FlushSendsAQuietTargetAsAHeartbeat: a reachable target with no
// results this cycle is still sent, so the server keeps its last_seen fresh.
// Before, the flush left such a target out, and a target whose checks were all
// silent went stale after 90 seconds although the agent could reach it.
func TestINTRUN002_FlushSendsAQuietTargetAsAHeartbeat(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := agent.NewManager(ctx, "quiet", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)
		buf, pusher := newFlushBuffer(t)

		var mu sync.Mutex
		flushEnvelope(ctx, []*agent.Manager{mgr}, &mu, map[string][]wire.Result{}, &edgeStates{byName: map[string]edgeState{}}, map[string]bool{}, map[string]map[string]bool{}, pusher)

		env := readEnvelope(t, buf)
		require.Len(t, env.Instances, 1)
		require.Equal(t, "quiet", env.Instances[0].TargetName)
		require.NotEmpty(t, env.Instances[0].InstanceID)
		require.Empty(t, env.Instances[0].Results, "a heartbeat carries no results, so it reports no synthetic check rate")
	})
}

// TestINTRUN003_FlushOmitsATargetWithoutIdentity: a target that answers but
// whose identity cannot be read has no cluster or instance id to report. It is
// left out as an unreachable target is, and its skip waits for the next cycle.
// The identity path is set with t.Setenv, which only a test that is not parallel
// may use, so this test is not run under pgtest.ForEach.
func TestINTRUN003_FlushOmitsATargetWithoutIdentity(t *testing.T) {
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			primary, _ := pgtest.PrimaryStandby(t, major)
			ctx := context.Background()
			corrupt := filepath.Join(t.TempDir(), "corrupt.json")
			require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o600))
			t.Setenv("PGLENS_IDENTITY_PATH", corrupt)

			mgr, err := agent.NewManager(ctx, "unidentified", primary.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
			require.NoError(t, err)
			t.Cleanup(mgr.Close)
			buf, pusher := newFlushBuffer(t)

			var mu sync.Mutex
			pending := map[string][]wire.Result{"unidentified": {
				{Check: "replication_streaming", TS: time.Now(), SkipReason: "role primary not allowed"},
				{Check: "locks", TS: time.Now()},
			}}
			flushEnvelope(ctx, []*agent.Manager{mgr}, &mu, pending, &edgeStates{byName: map[string]edgeState{}}, map[string]bool{}, map[string]map[string]bool{}, pusher)

			_, _, err = buf.Next()
			require.ErrorIs(t, err, io.EOF, "an instance without identity must not be sent, and an envelope without one is not queued")
			require.Len(t, pending["unidentified"], 1, "only the skip is kept for the next cycle")
			require.Equal(t, "replication_streaming", pending["unidentified"][0].Check)
		})
	}
}

// TestINTRUN004_SuperuserTargetIsWarnedAtStart: the start-up warning names the
// target whose monitoring role is a superuser, and no other target.
func TestINTRUN004_SuperuserTargetIsWarnedAtStart(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		su, err := agent.NewManager(ctx, "su-target", pg.DSN("postgres", pgtest.RoleSuperuser), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(su.Close)
		t0, err := agent.NewManager(ctx, "t0-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(t0.Close)

		var out bytes.Buffer
		warnSuperuserTargets(ctx, &out, []*agent.Manager{su, t0})
		require.Contains(t, out.String(), "warning: target su-target connects as a superuser")
		require.False(t, strings.Contains(out.String(), "t0-target"), "the monitoring role must not be reported: %s", out.String())
	})
}
