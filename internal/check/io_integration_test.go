//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTIO001_NonZeroIOIsLabeledAndReported(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		if pg.Version < pgtype.PG16 {
			t.Skip("pg_stat_io requires PostgreSQL 16")
		}
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE TABLE IF NOT EXISTS pglens_io_probe (id bigint, payload text)")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "INSERT INTO pglens_io_probe SELECT g, repeat('io', 100) FROM generate_series(1, 1000) g")
		require.NoError(t, err)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		result, err := (&ioCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
		for _, metric := range result.Metrics {
			if metric.Name == "pg_io_reads" || metric.Name == "pg_io_hits" {
				require.NotEmpty(t, metric.Labels["backend_type"])
				require.Equal(t, pgtype.KindCounter, metric.Kind)
			}
		}
	})
}

func TestINTIO002_TimingStateIsExplicit(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		if pg.Version < pgtype.PG16 {
			t.Skip("pg_stat_io requires PostgreSQL 16")
		}
		pg.Lock(t)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
		require.NoError(t, err)
		result, err := (&ioCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.NotEqual(t, -1.0, metricValue(result, "pg_io_timing_enabled", nil))
	})
}
