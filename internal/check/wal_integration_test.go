//go:build integration

package check

import (
	"context"
	"strconv"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTWAL001_PrimaryLSNAdvancesWithWAL(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE TABLE IF NOT EXISTS pglens_wal_probe (id bigint, payload text)")
		require.NoError(t, err)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		target := &registryTestTarget{conn: conn, version: pg.Version, database: "app", role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly}
		before, err := (&walCheck{}).Scrape(ctx, target)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "INSERT INTO pglens_wal_probe SELECT g, repeat('wal', 100) FROM generate_series(1, 1000) g")
		require.NoError(t, err)
		// Force the newly generated WAL into a distinct segment boundary before
		// the second scrape; this keeps the assertion deterministic on tiny test
		// containers whose insert may fit inside the current WAL page.
		_, err = admin.Exec(ctx, "SELECT pg_switch_wal()")
		require.NoError(t, err)
		conn2, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		after, err := (&walCheck{}).Scrape(ctx, &registryTestTarget{conn: conn2, version: pg.Version, database: "app", role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.Greater(t, metricValue(after, "pg_wal_lsn_bytes", nil), metricValue(before, "pg_wal_lsn_bytes", nil))
	})
}

func TestINTWAL002_StandbyReplayLSNIsAvailable(t *testing.T) {
	for _, major := range pgtest.Versions() {
		major := major
		t.Run("pg"+strconv.Itoa(major), func(t *testing.T) {
			primary, standby := pgtest.PrimaryStandby(t, major)
			pool := standby.Pool(t, "app", pgtest.RoleT0)
			conn, err := pool.Acquire(context.Background())
			require.NoError(t, err)
			result, err := (&walCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: standby.Version, database: "app", role: pgtype.RoleStandby, permTier: pgtype.TierReadOnly})
			require.NoError(t, err)
			require.NotNil(t, primary)
			require.GreaterOrEqual(t, metricValue(result, "pg_wal_stats_columns_available", nil), 1.0)
		})
	}
}

func TestINTWAL003_MetricNamesMatchMeasuredColumns(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
		require.NoError(t, err)
		result, err := (&walCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
		for _, metric := range result.Metrics {
			require.NotEqual(t, "pg_wal_pg_stat_wal", metric.Name)
		}
	})
}
