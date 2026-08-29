//go:build integration

package check

import (
	"context"
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
		_, e = (&bloatEstimateCheck{selector: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
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
		r, e := (&bloatEstimateCheck{selector: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, e)
		require.LessOrEqual(t, len(r.Metrics), 1000)
	})
}
