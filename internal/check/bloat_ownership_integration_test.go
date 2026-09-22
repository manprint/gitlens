//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-BLOAT-004 covers pg_stats visibility. pg_stats only shows columns the
// role may SELECT, so a Tier 0 role (pg_monitor alone) sees no statistics for
// application tables. The estimate used to fall back to a tuple width of
// zero there and report nearly the whole heap as bloat; a table without
// visible statistics must now get no estimate, and a role that can read the
// statistics must get a realistic one.
func TestINTBLOAT004_EstimateNeedsVisibleStatistics(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		name := fmt.Sprintf("pglens_bloat_owner_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (i int, payload text)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT g, repeat('payload', 32) FROM generate_series(1,100000) g", name))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", name))
		require.NoError(t, err)

		scrape := func(pool *pgxpool.Pool, tier pgtype.PermTier) map[string]float64 {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			result, err := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: tier})
			require.NoError(t, err)
			values := map[string]float64{}
			for _, metric := range result.Metrics {
				if metric.Labels["relname"] == name {
					values[metric.Name] = metric.Value
				}
			}
			return values
		}

		require.Empty(t, scrape(pg.Pool(t, "app", pgtest.RoleT0), pgtype.TierReadOnly),
			"without visible statistics the table must not get a fabricated estimate")

		reader := pg.Pool(t, "app", pgtest.RoleT1)
		fresh := scrape(reader, pgtype.TierExplain)
		require.NotEmpty(t, fresh, "a role that can read pg_stats must get an estimate")
		require.Less(t, fresh["pg_bloat_ratio"], 0.15, "a freshly loaded table is not bloated: %v", fresh)

		// Deleting half the rows leaves the heap its size while ANALYZE
		// halves the live tuple count: about half the heap is now dead space.
		_, err = admin.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE i %% 2 = 0", name))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", name))
		require.NoError(t, err)
		bloated := scrape(reader, pgtype.TierExplain)
		require.InDelta(t, 0.5, bloated["pg_bloat_ratio"], 0.15, "half the heap is dead space: %v", bloated)
	})
}
