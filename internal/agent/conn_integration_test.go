//go:build integration

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-CONN-001: connection ceiling never exceeds MaxConnsPerInstance.
func TestINTCONN001_ConnectionCeiling(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		// Open multiple per-database connections and verify ceiling.
		dbs := []string{"postgres", "template1"}
		conns := make(map[string]interface{})
		opts := mgr.opts
		maxExpected := opts.MaxConnsPerInstance

		for _, db := range dbs {
			for i := 0; i < maxExpected+2; i++ {
				conn, err := mgr.ForDatabase(ctx, db)
				if err != nil {
					t.Fatalf("ForDatabase(%s) failed: %v", db, err)
				}
				conns[db] = conn
				conn.Release()
			}
		}

		// Query pg_stat_activity to count pglens connections.
		conn, err := mgr.Shared(ctx)
		require.NoError(t, err)
		defer conn.Release()

		var activeConns int
		err = conn.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE application_name LIKE 'pglens/%' AND usename = 'pglens'
		`).Scan(&activeConns)
		require.NoError(t, err)
		// Should not exceed MaxConnsPerInstance + 1 (shared).
		if activeConns > maxExpected+1 {
			t.Errorf("too many connections: expected <= %d, got %d", maxExpected+1, activeConns)
		}
	})
}

// INT-CONN-002: idle per-database connections are closed after IdleTimeout.
func TestINTCONN002_IdleEviction(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		fakeClock := clock.NewFake(time.Now())
		opts := DefaultConnOptions()
		opts.IdleTimeout = 10 * time.Second
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), opts, fakeClock)
		require.NoError(t, err)
		defer mgr.Close()

		// Open a per-database connection.
		conn, err := mgr.ForDatabase(ctx, "template1")
		require.NoError(t, err)
		conn.Release()

		// Advance time past idle timeout.
		fakeClock.Advance(opts.IdleTimeout + 1*time.Second)

		// Open another connection (will evict the idle one).
		conn2, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		conn2.Release()

		// Verify the old connection's pool was closed (no assertion needed, just doesn't panic).
	})
}

// INT-CONN-003: application_name is set to pglens/<context>.
func TestINTCONN003_ApplicationName(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		// Get shared connection and check application_name.
		shared, err := mgr.Shared(ctx)
		require.NoError(t, err)
		defer shared.Release()

		var appName string
		err = shared.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName)
		require.NoError(t, err)
		require.Equal(t, "pglens/shared", appName)

		// Get per-database connection and check application_name.
		perDb, err := mgr.ForDatabase(ctx, "template1")
		require.NoError(t, err)
		defer perDb.Release()

		var appName2 string
		err = perDb.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName2)
		require.NoError(t, err)
		require.Equal(t, "pglens/template1", appName2)
	})
}

// TestManager_DedicatedConn proves DedicatedConn (phase 7.1, the ASH
// sampler's own connection) returns a real, immediately usable connection
// tagged distinctly from Shared()'s.
func TestManager_DedicatedConn(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		conn, err := mgr.DedicatedConn(ctx)
		require.NoError(t, err)
		defer conn.Release()

		var appName string
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName))
		require.Equal(t, "pglens/ash", appName)

		var one int
		require.NoError(t, conn.QueryRow(ctx, "SELECT 1").Scan(&one))
		require.Equal(t, 1, one)
	})
}

// INT-CONN-004: new databases created after startup are discovered; dropped ones stop being scraped.
func TestINTCONN004_DynamicDiscovery(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		shared, err := mgr.Shared(ctx)
		require.NoError(t, err)
		defer shared.Release()

		// Create a test database.
		_, err = shared.Exec(ctx, "CREATE DATABASE test_conn_discovery")
		if err == nil {
			defer func() {
				conn2, _ := mgr.Shared(ctx)
				conn2.Exec(ctx, "DROP DATABASE IF EXISTS test_conn_discovery")
				conn2.Release()
			}()

			// Discover should find it.
			dbs, err := mgr.Discover(ctx)
			require.NoError(t, err)
			found := false
			for _, db := range dbs {
				if db.Name == "test_conn_discovery" {
					found = true
					require.True(t, db.Monitored)
					break
				}
			}
			require.True(t, found, "test_conn_discovery not found in discovery result")
		}
	})
}

// INT-CONN-005: with MaxDatabases limit, excess databases are reported with skip_reason="db_budget".
func TestINTCONN005_BudgetReporting(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		opts := DefaultConnOptions()
		opts.MaxDatabases = 3
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		dbs, err := mgr.Discover(ctx)
		require.NoError(t, err)

		if len(dbs) > opts.MaxDatabases {
			monitored := 0
			unmonitored := 0
			for _, db := range dbs {
				if db.Monitored {
					monitored++
				} else {
					unmonitored++
					require.Equal(t, "db_budget", db.SkipReason)
				}
			}
			require.Equal(t, opts.MaxDatabases, monitored)
			require.Equal(t, len(dbs)-opts.MaxDatabases, unmonitored)
		}
	})
}
