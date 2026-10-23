//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTBLOAT001_BoundedEstimateRuns(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, e := pool.Acquire(ctx)
		require.NoError(t, e)
		_, e = (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, e)
	})
}
func TestINTBLOAT002_ActivityCanRunAlongsideEstimate(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, e := pool.Acquire(ctx)
		require.NoError(t, e)
		_, e = (&activityCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, e)
	})
}
func TestINTBLOAT003_EmptySchemaDoesNotEmitUnboundedSeries(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, e := pool.Acquire(ctx)
		require.NoError(t, e)
		r, e := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, e)
		require.LessOrEqual(t, len(r.Metrics), 1000)
	})
}

// TestINTBLOAT005_BloatSkipsLockedTable: a table another session holds under
// ACCESS EXCLUSIVE has its exact size left unread. The estimate must not fail,
// the other table is still reported and the result is partial.
func TestINTBLOAT005_BloatSkipsLockedTable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT1)
		suffix := testSuffix()
		locked := fmt.Sprintf("pglens_bloat_locked_%d", suffix)
		open := fmt.Sprintf("pglens_bloat_open_%d", suffix)
		for _, name := range []string{locked, open} {
			_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer, payload text)", name))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
			// The estimate considers only tables of at least 1 MiB, and reads
			// pg_stats, which ANALYZE fills.
			_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT g, repeat('x', 100) FROM generate_series(1, 60000) g", name))
			require.NoError(t, err)
			_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", name))
			require.NoError(t, err)
		}
		holdLock(t, admin, fmt.Sprintf("LOCK TABLE %s IN ACCESS EXCLUSIVE MODE", locked))
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		labels := func(relname string) map[string]string {
			return map[string]string{"schemaname": "public", "relname": relname, "indexrelname": "", "object_kind": "table", "method": "estimate"}
		}
		require.NotEqual(t, float64(-1), metricValue(result, "pg_bloat_real_bytes", labels(open)))
		require.Equal(t, float64(-1), metricValue(result, "pg_bloat_real_bytes", labels(locked)))
		require.True(t, result.Partial)
	})
}

// TestINTBLOAT006_UnreadableStatisticsArePartial: the default monitoring role
// cannot SELECT a table it was never granted, so pg_stats hides the table and
// the estimate has no row for it. The result is partial rather than reading as
// no bloat.
func TestINTBLOAT006_UnreadableStatisticsArePartial(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_bloat_hidden_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer, payload text)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		// Above the estimate's 1 MiB floor, and analysed, so that only the
		// role's lack of privilege keeps it out of the result.
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT g, repeat('x', 100) FROM generate_series(1, 60000) g", name))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", name))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		labels := map[string]string{"schemaname": "public", "relname": name, "indexrelname": "", "object_kind": "table", "method": "estimate"}
		require.Equal(t, float64(-1), metricValue(result, "pg_bloat_real_bytes", labels))
		require.True(t, result.Partial)
	})
}
