//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTBGW001_CheckpointRequestCounterAdvances(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		before, err := (&checkpointerCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CHECKPOINT")
		require.NoError(t, err)
		conn2, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		after, err := (&checkpointerCheck{}).Scrape(ctx, &registryTestTarget{conn: conn2, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.Greater(t, metricValue(after, "pg_checkpoints_requested_total", nil), metricValue(before, "pg_checkpoints_requested_total", nil))
	})
}

func TestINTBGW002_CheckpointerMetricNamesStableAcrossVersions(t *testing.T) {
	expected := []string{
		"pg_bgwriter_buffers_clean_total", "pg_bgwriter_maxwritten_clean_total",
		"pg_buffers_alloc_total", "pg_checkpoint_sync_time_ms_total",
		"pg_checkpoint_write_time_ms_total", "pg_checkpoints_requested_total",
		"pg_checkpoints_timed_total",
	}
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
		require.NoError(t, err)
		result, err := (&checkpointerCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.Equal(t, expected, checkpointerMetricNames(result))
	})
}
