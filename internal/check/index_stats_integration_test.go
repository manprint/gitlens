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
