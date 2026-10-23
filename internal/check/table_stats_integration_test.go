//go:build integration

package check

import (
	"context"
	"fmt"
	"strings"
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

// TestINTTBL004_TableStatsSkipsLockedTable: a table another session holds
// under ACCESS EXCLUSIVE cannot have its size read. The scrape must not fail
// for it; the other tables are still reported and the result is partial.
func TestINTTBL004_TableStatsSkipsLockedTable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		suffix := testSuffix()
		locked := fmt.Sprintf("pglens_tbl_locked_%d", suffix)
		open := fmt.Sprintf("pglens_tbl_open_%d", suffix)
		for _, name := range []string{locked, open} {
			_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id integer)", name))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
			_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,100)", name))
			require.NoError(t, err)
			_, err = admin.Exec(ctx, fmt.Sprintf("DELETE FROM %s", name))
			require.NoError(t, err)
		}
		holdLock(t, admin, fmt.Sprintf("LOCK TABLE %s IN ACCESS EXCLUSIVE MODE", locked))
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&tableStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.NotEmpty(t, findTableMetric(result, open, "pg_table_total_bytes"))
		require.Empty(t, findTableMetric(result, locked, "pg_table_total_bytes"))
		require.True(t, result.Partial)
	})
}

// TestINTTBL005_TableStatsSkipsOtherSessionsTempTables: another session's
// temporary table is listed by pg_stat_user_tables under its pg_temp_N schema.
// It lasts only as long as that session, so it is not a user table to report.
func TestINTTBL005_TableStatsSkipsOtherSessionsTempTables(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		// The read-only monitoring role cannot create a temporary table, so the
		// other session is the superuser; the check itself runs as the role.
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_tmp_%d", testSuffix())
		// The temporary table belongs to this session, which stays open while
		// the check runs.
		session, err := admin.Acquire(ctx)
		require.NoError(t, err)
		defer session.Release()
		_, err = session.Exec(ctx, fmt.Sprintf("CREATE TEMP TABLE %s (id integer)", name))
		require.NoError(t, err)
		_, err = session.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,100)", name))
		require.NoError(t, err)

		// The session's statistics reach the shared views only when it flushes
		// them, so wait until the table is visible before checking the filter.
		observer, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer observer.Release()
		require.Eventually(t, func() bool {
			var seen int
			return observer.QueryRow(ctx, "SELECT count(*) FROM pg_stat_user_tables WHERE relname = $1", name).Scan(&seen) == nil && seen > 0
		}, 30*time.Second, 100*time.Millisecond)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&tableStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		for _, m := range result.Metrics {
			require.False(t, strings.HasPrefix(m.Labels["schemaname"], "pg_temp"), "temporary table %s of another session was reported", m.Labels["relname"])
		}
	})
}
