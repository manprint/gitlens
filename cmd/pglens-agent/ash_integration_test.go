//go:build integration

package main

import (
	"context"
	"testing"
	"time"

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
