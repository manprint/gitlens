//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTIDX001_IndexDefinitionHashes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_idx_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer,b integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_a ON %s(a)", table, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_a2 ON %s(a)", table, table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		hashes := map[string]bool{}
		for _, f := range result.Facts {
			if f.Labels["relname"] == table {
				hashes[f.Labels["def_hash"]] = true
			}
		}
		require.Len(t, hashes, 1)
	})
}

func TestINTIDX002_IndexStatsUnusedStartsAtZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_unused_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_i ON %s(a)", table, table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		found := false
		for _, m := range result.Metrics {
			if m.Name == "pg_index_idx_scan" && m.Labels["relname"] == table {
				found = true
				require.Equal(t, float64(0), m.Value)
			}
		}
		require.True(t, found)
	})
}

func TestINTIDX003_InvalidIndexReportsZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_invalid_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_i ON %s(a)", table, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("UPDATE pg_index SET indisvalid=false WHERE indexrelid='%s_i'::regclass", table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
	})
}

// TestINTIDX004_IndexStatsSkipsLockedIndex: an index another session holds
// under ACCESS EXCLUSIVE cannot have its size read. The scrape must not fail
// for it; the other index is still reported and the result is partial.
func TestINTIDX004_IndexStatsSkipsLockedIndex(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		suffix := testSuffix()
		table := fmt.Sprintf("pglens_idx_t_%d", suffix)
		locked := fmt.Sprintf("pglens_idx_locked_%d", suffix)
		open := fmt.Sprintf("pglens_idx_open_%d", suffix)
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,1000)", table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(a)", locked, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(a)", open, table))
		require.NoError(t, err)
		// Locking the table does not lock its indexes; REINDEX INDEX inside a
		// transaction holds ACCESS EXCLUSIVE on the index until the rollback.
		holdLock(t, admin, fmt.Sprintf("REINDEX INDEX %s", locked))
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		indexes := map[string]bool{}
		for _, f := range result.Facts {
			if f.Labels["relname"] == table {
				indexes[f.Labels["indexrelname"]] = true
			}
		}
		require.True(t, indexes[open])
		require.False(t, indexes[locked])
		require.True(t, result.Partial)
	})
}
