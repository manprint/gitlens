//go:build integration

package check

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTACT001_ActivityDatabaseAndRatio(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
		foundRatio := false
		for _, metric := range result.Metrics {
			if metric.Name == "pg_connections_used_ratio" {
				foundRatio = true
				require.GreaterOrEqual(t, metric.Value, 0.0)
				require.LessOrEqual(t, metric.Value, 1.0)
			}
		}
		require.True(t, foundRatio)
	})
}

func TestINTACT002_PreparedTransactionGaugesAreAlwaysPresent(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		require.NotEqual(t, -1.0, metricValue(result, "pg_prepared_xacts", nil))
		require.NotEqual(t, -1.0, metricValue(result, "pg_oldest_prepared_xact_seconds", nil))
	})
}

func TestINTACT004_ActivityMetricNamesAcrossVersions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(context.Background())
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(context.Background(), target)
		require.NoError(t, err)
		for _, name := range []string{"pg_max_xact_age_seconds", "pg_max_idle_in_transaction_seconds", "pg_prepared_xacts", "pg_oldest_prepared_xact_seconds", "pg_max_datfrozenxid_age"} {
			found := false
			for _, m := range result.Metrics {
				if m.Name == name {
					found = true
				}
			}
			require.True(t, found, name)
		}
	})
}

// TestINTACT005_IdleInTransactionCountsAbortedSessions: a session whose
// transaction failed and that was left idle keeps the transaction open, so it
// is reported as 'idle in transaction (aborted)'. It is an idle-in-transaction
// session for the maximum that the advisor and the UI read.
func TestINTACT005_IdleInTransactionCountsAbortedSessions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		aborted, err := pool.Acquire(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = aborted.Exec(context.Background(), "ROLLBACK")
			aborted.Release()
		})
		_, err = aborted.Exec(ctx, "BEGIN")
		require.NoError(t, err)
		_, err = aborted.Exec(ctx, "SELECT 1/0")
		require.Error(t, err)
		time.Sleep(1500 * time.Millisecond)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", instanceID: uuid.New(), clusterID: 1, role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		result, err := (&activityCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		require.GreaterOrEqual(t, metricValue(result, "pg_max_idle_in_transaction_seconds", nil), 1.0)
	})
}
