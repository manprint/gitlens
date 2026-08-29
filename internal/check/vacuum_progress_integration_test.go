//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTVAC001_NoRunningVacuumReportsZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		r, err := (&vacuumProgressCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running", Kind: pgtype.KindGauge, Value: 0})
	})
}
func TestINTVAC002_StableMetricNamesAcrossVersions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		r, err := (&vacuumProgressCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		names := map[string]bool{}
		for _, m := range r.Metrics {
			names[m.Name] = true
		}
		require.True(t, names["pg_vacuum_jobs_running"])
		require.True(t, names["pg_analyze_jobs_running"])
	})
}
