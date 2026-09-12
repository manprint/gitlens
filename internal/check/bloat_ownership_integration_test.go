//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-BLOAT-004 verifies the T0 ownership case. pg_monitor exposes pg_stats
// in the shipped permission model, so the check must return a real estimate
// rather than a fabricated zero or an error.
func TestINTBLOAT004_TierZeroSkipsForeignOwnedStatistics(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		t0 := pg.Pool(t, "app", pgtest.RoleT0)
		name := fmt.Sprintf("pglens_bloat_owner_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (payload text)", name))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT repeat('payload', 32) FROM generate_series(1,100000)", name))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("ANALYZE %s", name))
		require.NoError(t, err)

		conn, err := t0.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&bloatEstimateCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		seen := false
		for _, metric := range result.Metrics {
			if metric.Labels["relname"] == name {
				seen = true
				require.NotEqual(t, float64(0), metric.Value, "ownership must not produce a fabricated zero estimate")
			}
		}
		require.True(t, seen, "pg_monitor must expose the analysed relation to the T0 check")
	})
}
