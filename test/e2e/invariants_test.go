//go:build integration && e2e

package e2e

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/test/harness"
)

// The invariant checks run against the real, shipped migrations
// (internal/store/migrations/*.sql), which use TimescaleDB's
// create_hypertable(..., by_range(...)) — not against a hand-copied test
// schema. That distinction matters: every other integration test this
// session goes through internal/server/setup_test.go's hand-written schema
// (plain tables, no TimescaleDB), so store.Migrate() had never actually been
// exercised against a real TimescaleDB database before this file did —
// which immediately surfaced a real bug (see the migrate0001 fix in
// internal/store/migrations/0001_meta.sql: it collided with store.Migrate's
// own schema_migrations bootstrap). pgtest's plain postgres:15/18 images
// (used everywhere else for L2 agent-check tests) do not have the
// TimescaleDB extension at all, so they cannot run these migrations either —
// hence a dedicated timescale/timescaledb container here, matching decision
// D3's pinned image.
var (
	tsPool      *pgxpool.Pool
	tsContainer testcontainers.Container
	tsOnce      sync.Once
	tsErr       error
)

func timescaleFixturePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	tsOnce.Do(func() {
		ctx := context.Background()
		c, err := postgres.Run(ctx, "timescale/timescaledb:2.29.0-pg17",
			postgres.WithDatabase("pglens"),
			postgres.WithUsername("pglens"),
			postgres.WithPassword("pglens"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
		)
		if err != nil {
			tsErr = fmt.Errorf("start timescaledb: %w", err)
			return
		}
		tsContainer = c
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			tsErr = err
			return
		}
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			tsErr = err
			return
		}
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`); err != nil {
			tsErr = fmt.Errorf("create timescaledb extension: %w", err)
			return
		}
		if err := store.Migrate(ctx, pool); err != nil {
			tsErr = fmt.Errorf("migrate: %w", err)
			return
		}
		tsPool = pool
	})
	require.NoError(t, tsErr)
	return tsPool
}

// invariantsFixture truncates every table the invariant checks touch (so
// each test starts from a clean slate against the one shared migrated
// database — spinning up a fresh TimescaleDB container per test would be
// far slower) and seeds one tenant/agent/cluster/instance row so metric/
// event inserts satisfy their foreign keys.
func invariantsFixture(t *testing.T) (*pgxpool.Pool, uuid.UUID, int64) {
	t.Helper()
	pool := timescaleFixturePool(t)
	ctx := context.Background()

	for _, table := range []string{"metrics", "metrics_statements", "metrics_ash", "metrics_replication", "events", "instances", "clusters", "agents"} {
		_, err := pool.Exec(ctx, "TRUNCATE TABLE "+table+" CASCADE")
		require.NoError(t, err)
	}

	agentID := uuid.New()
	instID := uuid.New()
	const clusterID = int64(424242)

	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (cluster_id, id_source) VALUES ($1, 'manual')`, clusterID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO instances (instance_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier)
		VALUES ($1, $2, $3, 'test.local', 5432, 170000, 'primary', 'T0')`,
		instID, clusterID, agentID)
	require.NoError(t, err)

	return pool, instID, clusterID
}

// TestInvariant_I3_DuplicateSamplesRejectedByConstraint proves I-3 is
// enforced by the schema itself (metrics_dedup_idx UNIQUE(series_id, ts)),
// not merely by a Go-side check — matching migration 0002's own comment
// ("enforced by the storage engine rather than by hope"). A second insert of
// the same (series_id, ts) must fail at the database, and
// assertNoDuplicateSamples must find nothing to report on the resulting
// (necessarily duplicate-free) table.
func TestInvariant_I3_DuplicateSamplesRejectedByConstraint(t *testing.T) {
	pool, instID, clusterID := invariantsFixture(t)
	ctx := context.Background()
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	_, err := pool.Exec(ctx, `
		INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES ($1, $2, $3, 'test_metric', 999, 1.0)`, ts, clusterID, instID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES ($1, $2, $3, 'test_metric', 999, 2.0)`, ts, clusterID, instID)
	require.Error(t, err, "a duplicate (series_id, ts) must be rejected by metrics_dedup_idx")

	harness.AssertDBInvariants(ctx, t, pool)
}

// TestInvariant_I2_NegativeRateDetected plants a negative value in metrics
// (nothing in the schema prevents this — unlike I-3, this is a Go-side
// check) and proves assertNoNegativeRates reports it.
func TestInvariant_I2_NegativeRateDetected(t *testing.T) {
	pool, instID, clusterID := invariantsFixture(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES (now(), $1, $2, 'bad_metric', 1000, -5.0)`, clusterID, instID)
	require.NoError(t, err)

	spy := &spyT{T: t}
	harness.AssertDBInvariants(ctx, spy, pool)
	require.True(t, spy.failed, "expected I-2 to report the planted negative rate")
}

// TestInvariant_CleanFixturePasses proves I-1/I-2/I-4 all report nothing on
// a fixture with no planted violations — an invariant check that always
// fails is exactly as useless as one that never does.
func TestInvariant_CleanFixturePasses(t *testing.T) {
	pool, instID, clusterID := invariantsFixture(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES (now(), $1, $2, 'good_metric', 1001, 3.5)`, clusterID, instID)
	require.NoError(t, err)

	harness.AssertDBInvariants(ctx, t, pool) // must not fail
}

// TestInvariant_I4_OrphanMetricDetected plants a metrics row for an
// instance_id that does not exist in instances (no FK stops this — a
// deliberate schema choice for hypertable write throughput) and proves
// assertNoOrphanMetrics reports it.
func TestInvariant_I4_OrphanMetricDetected(t *testing.T) {
	pool, _, clusterID := invariantsFixture(t)
	ctx := context.Background()

	orphanInstID := uuid.New() // deliberately never inserted into instances
	_, err := pool.Exec(ctx, `
		INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES (now(), $1, $2, 'orphan_metric', 1002, 1.0)`, clusterID, orphanInstID)
	require.NoError(t, err)

	spy := &spyT{T: t}
	harness.AssertDBInvariants(ctx, spy, pool)
	require.True(t, spy.failed, "expected I-4 to report the orphan instance_id")
}

// TestInvariant_I1_ClusterIDChangedEventDetected plants a cluster_id_changed
// event (the real signal internal/server/inventory.go emits on a mismatch)
// and proves assertClusterIDStable reports it.
func TestInvariant_I1_ClusterIDChangedEventDetected(t *testing.T) {
	pool, instID, clusterID := invariantsFixture(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO events (ts, type, cluster_id, instance_id, payload)
		VALUES (now(), 'cluster_id_changed', $1, $2, '{"reason":"test"}')`, clusterID, instID)
	require.NoError(t, err)

	spy := &spyT{T: t}
	harness.AssertDBInvariants(ctx, spy, pool)
	require.True(t, spy.failed, "expected I-1 to report the planted cluster_id_changed event")
}

// spyT wraps a *testing.T to observe whether Errorf/Fatalf was called,
// without actually failing the enclosing test — the planted-violation tests
// above need to assert "the invariant check itself would have failed" as a
// value, not have that failure propagate and abort the test that is proving
// detection works.
type spyT struct {
	*testing.T
	failed bool
}

func (s *spyT) Errorf(format string, args ...any) {
	s.failed = true
	s.Logf("(expected) "+format, args...)
}

func (s *spyT) Fatalf(format string, args ...any) {
	s.failed = true
	s.Logf("(expected, fatal) "+format, args...)
	// Do not call s.T.FailNow(): the caller (AssertDBInvariants) must stop
	// its own execution the same way a real *testing.T would, but the outer
	// test that is verifying detection must keep running to check spy.failed.
	panic(spyStop{})
}

type spyStop struct{}
