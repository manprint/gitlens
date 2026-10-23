//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// INT-STORE-004: a migration run that outlasts the unlock budget still frees
// the migration lock. The unlock used to share the deadline taken when the lock
// was acquired, so a longer run made the unlock fail, and the session kept the
// lock while it sat idle in the pool for other replicas to wait on.
func TestMigrate_INT_STORE_004_LockFreedAfterLongRun(t *testing.T) {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "timescale/timescaledb:2.31.0-pg17",
		postgres.WithDatabase("test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
	)
	require.NoError(t, err, "start postgres container")
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "get connection string")
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "create pool")
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`)
	require.NoError(t, err, "create timescaledb extension")

	// The budget is far below the time a fresh run of every migration takes, so
	// the run outlasts it, while the unlock itself finishes well inside it.
	original := migrationUnlockTimeout
	migrationUnlockTimeout = 20 * time.Millisecond
	t.Cleanup(func() { migrationUnlockTimeout = original })

	started := time.Now()
	require.NoError(t, Migrate(ctx, pool))
	require.Greater(t, time.Since(started), migrationUnlockTimeout, "the run must outlast the unlock budget")

	// A new session, not a pooled one: the pool would hand back the connection
	// that may still hold the lock, and a session lock is re-entrant there.
	probe, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Close(context.Background()) })
	var acquired bool
	require.NoError(t, probe.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:migrations'))`).Scan(&acquired))
	require.True(t, acquired, "the migration lock must be free once Migrate returns")
}
