//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	storeTestPool      *pgxpool.Pool
	storeTestContainer testcontainers.Container
)

func getStoreTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if storeTestPool != nil {
		return storeTestPool
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "timescale/timescaledb:2.29.0-pg17",
		postgres.WithDatabase("test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2)),
	)
	require.NoError(t, err, "start postgres container")
	storeTestContainer = c

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "get connection string")

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "create pool")

	_, err = pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS timescaledb`)
	require.NoError(t, err, "create timescaledb extension")
	require.NoError(t, Migrate(ctx, pool), "apply store migrations")

	storeTestPool = pool
	return pool
}

// INT-STORE-003: inserting the same (series_id, ts) twice raises a unique violation,
// and with ON CONFLICT DO NOTHING writes exactly one row. Invariant I-3 proven at the storage layer.
func TestWriteMetrics_INT_STORE_003_UniquenessEnforced(t *testing.T) {
	pool := getStoreTestPool(t)
	ctx := context.Background()

	// Truncate to start clean
	_, _ = pool.Exec(ctx, "TRUNCATE metrics")

	now := time.Now()
	iid := uuid.New()
	seriesID := int64(12345)

	// First: demonstrate that a raw INSERT without ON CONFLICT raises unique violation
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`,
		now, "default", int64(1), iid, "postgres", "test_metric", "{}", seriesID, 1.5)
	require.NoError(t, err, "first raw insert should succeed")
	err = tx.Commit(ctx)
	require.NoError(t, err)

	// Verify one row inserted
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics WHERE series_id=$1 AND ts=$2", seriesID, now).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "should have exactly one row after first insert")

	// Second raw insert with same (series_id, ts) should fail with unique violation
	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO metrics (ts, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id, value)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`,
		now, "default", int64(1), iid, "postgres", "test_metric_2", "{}", seriesID, 2.5)
	require.Error(t, err, "second raw insert with same series_id,ts should fail with unique violation")
	_ = tx.Rollback(ctx)

	// Verify row count unchanged
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics WHERE series_id=$1 AND ts=$2", seriesID, now).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "row count should be unchanged after failed insert")

	// Now test with WriteMetrics - it uses ON CONFLICT DO NOTHING and should succeed
	row := MetricRow{
		TS:         now,
		TenantID:   "default",
		ClusterID:  1,
		InstanceID: iid,
		Datname:    "postgres",
		Metric:     "test_metric_3",
		Labels:     map[string]string{"c": "d"},
		SeriesID:   seriesID,
		Value:      3.5,
	}

	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	err = WriteMetrics(ctx, tx, []MetricRow{row})
	require.NoError(t, err, "WriteMetrics with ON CONFLICT should succeed")
	err = tx.Commit(ctx)
	require.NoError(t, err)

	// Verify row count is still 1 (ON CONFLICT DO NOTHING prevents duplicate)
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics WHERE series_id=$1 AND ts=$2", seriesID, now).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "ON CONFLICT DO NOTHING should keep row count at 1")
}
