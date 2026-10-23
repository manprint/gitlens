//go:build integration

package check

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
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
		// The hidden table is counted and named, never dropped silently.
		require.GreaterOrEqual(t, metricValue(result, wire.BloatSkippedTablesMetric, map[string]string{}), float64(1))
		require.Len(t, result.Facts, 1)
		var value wire.BloatSkippedValue
		require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &value))
		require.Contains(t, value.Tables, wire.BloatSkippedTable{SchemaName: "public", RelName: name})
	})
}

// TestINTBLOAT007_IndexBloatIsEstimated: a btree index on a table that lost
// most of its rows keeps its pages, so its estimate is bloated, while the same
// index freshly built is not. The table is kept from autovacuum so that only
// the DELETE and the ANALYZE shape the numbers.
func TestINTBLOAT007_IndexBloatIsEstimated(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		reader := pg.Pool(t, "app", pgtest.RoleT1)
		suffix := testSuffix()
		table := fmt.Sprintf("pglens_bloat_idx_%d", suffix)
		index := fmt.Sprintf("pglens_bloat_idx_id_%d", suffix)
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer, payload text) WITH (autovacuum_enabled = false)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		// About 2 MiB of btree leaves: above the estimate's 1 MiB floor.
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT g, repeat('x', 100) FROM generate_series(1, 100000) g", table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (id)", index, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", table))
		require.NoError(t, err)

		indexRatio := func() float64 {
			conn, err := reader.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			result, err := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierExplain})
			require.NoError(t, err)
			labels := map[string]string{"schemaname": "public", "relname": table, "indexrelname": index, "object_kind": "index", "method": "estimate"}
			require.NotEqual(t, float64(-1), metricValue(result, "pg_bloat_real_bytes", labels), "the fresh index must get an estimate")
			return metricValue(result, "pg_bloat_ratio", labels)
		}

		require.Less(t, indexRatio(), 0.15, "a freshly built index is not bloated")

		// Deleting nine rows in ten leaves the index's pages in place: VACUUM
		// does not return them, and only the table's live count shrinks.
		_, err = admin.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE id %% 10 <> 0", table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", table))
		require.NoError(t, err)
		require.Greater(t, indexRatio(), 0.5, "most of the index is dead space")
	})
}
