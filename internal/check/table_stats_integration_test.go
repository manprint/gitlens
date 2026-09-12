//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTTBL001_TableStatsDeadTuples(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_tbl_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,100)", name))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("DELETE FROM %s", name))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&tableStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.NotEmpty(t, findTableMetric(result, name, "pg_table_n_dead_tup"))
	})
}

func TestINTTBL002_TableStatsVacuumAge(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_vac_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("VACUUM (ANALYZE) %s", name))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&tableStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.NotEmpty(t, findTableMetric(result, name, "pg_table_last_vacuum_age_seconds"))
	})
}

func TestINTTBL003_TableStatsNeverAutovacuumed(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_fresh_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&tableStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Empty(t, findTableMetric(result, name, "pg_table_last_autovacuum_age_seconds"))
	})
}

func cardinalityForIntegration() *scopedSelectors {
	return newScopedSelectors(cardinality.Options{TopN: 50})
}

func findTableMetric(result Result, relation, name string) []pgtype.Metric {
	var found []pgtype.Metric
	for _, metric := range result.Metrics {
		if metric.Name == name && metric.Labels["relname"] == relation {
			found = append(found, metric)
		}
	}
	return found
}

func testSuffix() int64 { return time.Now().UnixNano() % 1000000000 }
