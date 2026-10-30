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

// MIG-ADV-001: the advisor settings migrations (11.4) apply on a fresh store
// and again over themselves, and leave both tables keyed by tenant with the
// tenant defaulting to 'default'.
func TestMIGADV001(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)

	require.NoError(t, Migrate(ctx, pool))
	for _, name := range []string{"0024_advisor_rule_overrides.sql", "0025_server_settings.sql"} {
		data, err := migrationFS.ReadFile("migrations/" + name)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(data))
		require.NoError(t, err, "re-apply %s", name)
	}
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version IN (24, 25)`)
	require.NoError(t, err)
	require.NoError(t, Migrate(ctx, pool))
	var recorded int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version IN (24, 25)`).Scan(&recorded))
	require.Equal(t, 2, recorded)

	_, err = pool.Exec(ctx, `INSERT INTO advisor_rule_overrides (rule_id, enabled) VALUES ('index.unused', false)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO advisor_rule_overrides (rule_id, enabled) VALUES ('index.unused', true)`)
	require.Error(t, err, "one override per tenant and rule")
	var tenant string
	var enabled bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT tenant_id, enabled FROM advisor_rule_overrides WHERE rule_id='index.unused'`).Scan(&tenant, &enabled))
	require.Equal(t, "default", tenant)
	require.False(t, enabled)

	_, err = pool.Exec(ctx, `INSERT INTO server_settings (key, value) VALUES ('window.unused_index', '3600'::jsonb)`)
	require.NoError(t, err)
	var seconds int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT tenant_id, (value #>> '{}')::bigint FROM server_settings WHERE key='window.unused_index'`).Scan(&tenant, &seconds))
	require.Equal(t, "default", tenant)
	require.Equal(t, int64(3600), seconds)
	_, err = pool.Exec(ctx, `INSERT INTO server_settings (key, value) VALUES ('window.unused_index', NULL)`)
	require.Error(t, err, "a value is required")
}

// DL-MIG-INT-001: on a fresh store db.deadlock_rate ends up firing on any
// deadlock (threshold 0, no duration). Run again over a rule an operator tuned,
// 0026 leaves the rule as it is; run over a rule still at the 0006 seed, it
// repairs it.
func TestDLMIGINT001_DeadlockRuleDefaultsToAnyDeadlock(t *testing.T) {
	ctx, _, pool := newMigrationTestPool(t)
	require.NoError(t, Migrate(ctx, pool))

	rule := func() (string, float64, int) {
		var comparator string
		var threshold float64
		var forSeconds int
		require.NoError(t, pool.QueryRow(ctx, `SELECT comparator, threshold, for_seconds FROM alert_rules WHERE tenant_id='default' AND rule_id='db.deadlock_rate'`).Scan(&comparator, &threshold, &forSeconds))
		return comparator, threshold, forSeconds
	}
	rerun := func() {
		t.Helper()
		_, err := pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version = 26`)
		require.NoError(t, err)
		require.NoError(t, Migrate(ctx, pool))
	}
	comparator, threshold, forSeconds := rule()
	require.Equal(t, "gt", comparator)
	require.Equal(t, 0.0, threshold)
	require.Equal(t, 0, forSeconds)

	_, err := pool.Exec(ctx, `UPDATE alert_rules SET threshold=0.1, for_seconds=600 WHERE rule_id='db.deadlock_rate'`)
	require.NoError(t, err)
	rerun()
	_, threshold, forSeconds = rule()
	require.Equal(t, 0.1, threshold, "a tuned rule keeps its threshold")
	require.Equal(t, 600, forSeconds, "a tuned rule keeps its duration")

	_, err = pool.Exec(ctx, `UPDATE alert_rules SET threshold=0.1, for_seconds=300 WHERE rule_id='db.deadlock_rate'`)
	require.NoError(t, err)
	rerun()
	_, threshold, forSeconds = rule()
	require.Equal(t, 0.0, threshold, "the seeded rule is repaired")
	require.Equal(t, 0, forSeconds)
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
