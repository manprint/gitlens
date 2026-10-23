//go:build integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTASH010_DedicatedConnectionReconnects kills the ASH sampler's
// backend: the next samples must come from a fresh connection instead of
// failing until the agent is restarted.
func TestINTASH010_DedicatedConnectionReconnects(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := agent.NewManager(ctx, "ash-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		clk := clock.NewFake(time.Now())
		conn := &ashConn{mgr: mgr, base: ctx, clk: clk}
		defer conn.Close()

		samplePID := func() (int32, error) {
			rows, err := conn.Query(ctx, "SELECT pg_backend_pid()")
			if err != nil {
				return 0, err
			}
			defer rows.Close()
			var pid int32
			for rows.Next() {
				if err := rows.Scan(&pid); err != nil {
					return 0, err
				}
			}
			return pid, rows.Err()
		}

		first, err := samplePID()
		require.NoError(t, err)

		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err = admin.Exec(ctx, "SELECT pg_terminate_backend($1)", first)
		require.NoError(t, err)

		// The terminated backend fails the sample that discovers it...
		require.Eventually(t, func() bool {
			_, err := samplePID()
			return err != nil
		}, 5*time.Second, 50*time.Millisecond)

		// ...and once the backoff has passed a new connection takes over.
		clk.Advance(ashReconnectBackoff)
		second, err := samplePID()
		require.NoError(t, err)
		require.NotEqual(t, first, second, "a fresh backend serves the sampler")

		var timeout string
		rows, err := conn.Query(ctx, "SHOW statement_timeout")
		require.NoError(t, err)
		for rows.Next() {
			require.NoError(t, rows.Scan(&timeout))
		}
		rows.Close()
		require.Equal(t, "400ms", timeout, "a slow sample is cancelled by the server, not by closing the connection")
	})
}

// TestINTASH011_ClosedConnectionIsNotReopened covers a sampler tick that is
// still running when the connection is closed: its late query must fail
// without dialling again, so no pool slot is left held for Close to wait on.
func TestINTASH011_ClosedConnectionIsNotReopened(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := agent.NewManager(ctx, "ash-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)

		clk := clock.NewFake(time.Now())
		conn := &ashConn{mgr: mgr, base: ctx, clk: clk}

		rows, err := conn.Query(ctx, "SELECT 1")
		require.NoError(t, err)
		rows.Close()
		conn.Close()

		// The base context stays alive, so only the closed flag can refuse
		// the dial. The rows are closed again when the query is not refused,
		// so the failure is reported instead of hanging the pool close below.
		late, err := conn.Query(ctx, "SELECT 1")
		if late != nil {
			late.Close()
		}
		require.ErrorIs(t, err, errASHClosed)

		closed := make(chan struct{})
		go func() {
			mgr.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			t.Fatal("manager close waits for a connection the closed sampler reacquired")
		}
	})
}

// TestINTASH012_ShutdownStopsASHBeforeClosingPools holds the sampler's dedicated
// connection and runs the shutdown sequence. The stop must release that
// connection before the manager closes its pools, or the close waits forever.
func TestINTASH012_ShutdownStopsASHBeforeClosingPools(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := agent.NewManager(ctx, "ash-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)

		clk := clock.NewFake(time.Now())
		conn := &ashConn{mgr: mgr, base: ctx, clk: clk}
		rows, err := conn.Query(ctx, "SELECT 1")
		require.NoError(t, err)
		rows.Close()

		closed := make(chan struct{})
		go func() {
			closeTargets([]func(){conn.Close}, []*agent.Manager{mgr})
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(10 * time.Second):
			// Release the held connection so the blocked close returns and the
			// failing test does not leak a goroutine.
			conn.Close()
			<-closed
			t.Fatal("shutdown waits for the ASH connection that its stop should have released")
		}
	})
}

// INT-ASH-006 (agent side): the dedicated connection notices a role that cannot
// see other users' sessions and records it, and only a role that can see them
// leaves the flag clear.
func TestINTASH006_ConnRecordsHiddenSessions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ash_plain') THEN CREATE ROLE ash_plain LOGIN PASSWORD 'test'; END IF; END $$`)
		require.NoError(t, err)

		mgr, err := agent.NewManager(ctx, "ash-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		plainCfg, err := pgxpool.ParseConfig(pg.DSN("postgres", pgtest.RoleSuperuser))
		require.NoError(t, err)
		plainCfg.ConnConfig.User = "ash_plain"
		plainCfg.ConnConfig.Password = "test"
		plainPool, err := pgxpool.NewWithConfig(ctx, plainCfg)
		require.NoError(t, err)
		defer plainPool.Close()
		plainConn, err := plainPool.Acquire(ctx)
		require.NoError(t, err)
		defer plainConn.Release()

		hidden := &ashConn{mgr: mgr, base: ctx, clk: clock.NewFake(time.Now())}
		defer hidden.Close()
		hidden.warnIfSessionsHidden(ctx, plainConn)
		require.True(t, hidden.warnedHidden)
		hidden.warnIfSessionsHidden(ctx, plainConn) // already recorded: nothing more to do
		require.True(t, hidden.warnedHidden)

		visibleConn, err := mgr.DedicatedConn(ctx)
		require.NoError(t, err)
		defer visibleConn.Release()
		visible := &ashConn{mgr: mgr, base: ctx, clk: clock.NewFake(time.Now())}
		defer visible.Close()
		visible.warnIfSessionsHidden(ctx, visibleConn)
		require.False(t, visible.warnedHidden)
	})
}

// INT-ASH-009: a target's first dedicated connection logs its compute_query_id
// setting, once, so an operator can tell why a sample carries no queryid.
func TestINTASH009_ConnLogsQueryIDSetting(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		mgr, err := agent.NewManager(ctx, "ash-target", pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), nil)
		require.NoError(t, err)
		defer mgr.Close()

		conn, err := mgr.DedicatedConn(ctx)
		require.NoError(t, err)
		defer conn.Release()

		logged := &ashConn{mgr: mgr, base: ctx, clk: clock.NewFake(time.Now())}
		defer logged.Close()
		logged.logQueryIDSetting(ctx, conn)
		require.True(t, logged.loggedQueryID)
	})
}
