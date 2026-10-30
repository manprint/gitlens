//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newMigrationTestPool starts an empty TimescaleDB and returns a pool on it.
func newMigrationTestPool(t *testing.T) (context.Context, string, *pgxpool.Pool) {
	t.Helper()
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
	return ctx, dsn, pool
}

// requireMigrationLockFree polls for the migration lock from a new session each
// time, not a pooled one: a pooled session may still hold the lock, and a
// session lock is re-entrant there. The backend releases a closed session's
// lock asynchronously, so one immediate try can miss it.
func requireMigrationLockFree(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	require.Eventually(t, func() bool {
		probe, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return false
		}
		defer func() { _ = probe.Close(ctx) }()
		var acquired bool
		if err := probe.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:migrations'))`).Scan(&acquired); err != nil {
			return false
		}
		return acquired
	}, 10*time.Second, 100*time.Millisecond, "the migration lock must be free once Migrate returns")
}

// INT-STORE-004: a migration run that outlasts the unlock budget still frees
// the migration lock. The unlock used to share the deadline taken when the lock
// was acquired, so a longer run made the unlock fail, and the session kept the
// lock while it sat idle in the pool for other replicas to wait on.
func TestMigrate_INT_STORE_004_LockFreedAfterLongRun(t *testing.T) {
	ctx, dsn, pool := newMigrationTestPool(t)

	// The budget is far below the time a fresh run of every migration takes, so
	// the run outlasts it, while the unlock itself finishes well inside it.
	original := migrationUnlockTimeout
	migrationUnlockTimeout = 20 * time.Millisecond
	t.Cleanup(func() { migrationUnlockTimeout = original })

	started := time.Now()
	require.NoError(t, Migrate(ctx, pool))
	require.Greater(t, time.Since(started), migrationUnlockTimeout, "the run must outlast the unlock budget")
	requireMigrationLockFree(t, ctx, dsn)
}

// INT-STORE-005: an unlock that fails must not leave the lock with a pooled
// session. With no time left for the unlock, it fails, and the connection is
// closed, which ends the session and releases the lock.
func TestMigrate_INT_STORE_005_LockFreedWhenUnlockFails(t *testing.T) {
	ctx, dsn, pool := newMigrationTestPool(t)

	original := migrationUnlockTimeout
	migrationUnlockTimeout = 0
	t.Cleanup(func() { migrationUnlockTimeout = original })

	require.NoError(t, Migrate(ctx, pool))
	requireMigrationLockFree(t, ctx, dsn)
}

// MIG-NAME-001: the name migrations apply on a fresh store, apply again over
// themselves, and leave the display_name column and both unique indexes in
// place. Two clusters may not share a name differing only in case, while any
// number of clusters may have none.
func TestMIGNAME001(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)

	require.NoError(t, Migrate(ctx, pool))
	require.NoError(t, Migrate(ctx, pool), "a second run must be a no-op")

	// Each file must also be safe to run over itself: run it directly, then
	// forget it was recorded so Migrate applies it once more.
	for _, name := range []string{"0022_cluster_name_unique.sql", "0023_instance_display_name.sql"} {
		data, err := migrationFS.ReadFile("migrations/" + name)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(data))
		require.NoError(t, err, "re-apply %s", name)
	}
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version IN (22, 23)`)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, pool))
	var recorded int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version IN (22, 23)`).Scan(&recorded))
	require.Equal(t, 2, recorded)

	var nullable string
	require.NoError(t, pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name='instances' AND column_name='display_name' AND data_type='text'`).Scan(&nullable))
	require.Equal(t, "YES", nullable)

	for index, want := range map[string]string{
		"clusters_name_uniq":          "ON public.clusters USING btree (tenant_id, lower(name)) WHERE (name IS NOT NULL)",
		"instances_display_name_uniq": "ON public.instances USING btree (tenant_id, lower(display_name)) WHERE (display_name IS NOT NULL)",
	} {
		var def string
		require.NoError(t, pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname=$1`, index).Scan(&def), index)
		require.Contains(t, def, "CREATE UNIQUE INDEX "+index, index)
		require.Contains(t, def, want, index)
	}

	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source, name) VALUES ('default', 1, 'manual', NULL), ('default', 2, 'manual', NULL), ('default', 3, 'manual', 'Prod')`)
	require.NoError(t, err, "unnamed clusters are not constrained")
	_, err = pool.Exec(ctx, `UPDATE clusters SET name='prod' WHERE cluster_id=1`)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code)
	require.Equal(t, "clusters_name_uniq", pgErr.ConstraintName)
}
