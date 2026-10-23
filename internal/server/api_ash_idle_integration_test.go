//go:build integration

package server

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

// INT-ASH-016: a window that ticked with no sampled session is stored as a
// zero-count row carrying its ticks. Those ticks count in every average and
// in the significance total, but the row never shows as a bucket or a group.
func TestIntASH016_IdleWindowsCountTicks(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(5151)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	end := base.Add(30 * time.Second)

	// One busy window, then two idle windows: the agent's zero-count record
	// carries the defaults the server fills for an empty key.
	_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
		VALUES ($1, $2, $3, 'app', 'LWLock', 'WALWrite', 'active', 7, 10, 10, 10)`, base, store.ToDB(cid), instID)
	require.NoError(t, err)
	for _, ts := range []time.Time{base.Add(10 * time.Second), base.Add(20 * time.Second)} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, $2, $3, '', 'CPU', 'CPU', 'active', NULL, 0, 10, 10)`, ts, store.ToDB(cid), instID)
		require.NoError(t, err)
	}

	api := NewAshAPI(pool)

	// Over the three windows the instance ticked 30 times and sampled 10
	// sessions: an average of one third, not the busy window's 1.0.
	buckets, ticks, err := api.queryASHBuckets(ctx, instID, &base, &end, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Len(t, buckets, 1, "an idle window is not listed")
	require.Equal(t, 10, buckets[0].Samples)
	require.Equal(t, 30, ticks, "the idle windows' ticks count toward the total")

	groups, ticks, err := api.queryASHGrouped(ctx, instID, base, end, 30, "", ashFilters{}, []string{"wait_event_type"}, 1000)
	require.NoError(t, err)
	require.Len(t, groups, 1, "no group is made of the zero-count rows")
	require.Equal(t, "LWLock", groups[0].WaitEventType)
	require.Equal(t, 10, groups[0].Samples)
	require.Equal(t, 30, groups[0].Ticks)
	require.InDelta(t, 10.0/30.0, *groups[0].AvgActiveSessions, 1e-9)
	require.Equal(t, 30, ticks)

	top, topTicks, err := api.queryASHTop(ctx, instID, &base, &end, "", ashFilters{}, 10)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, 10, top[0].Samples)
	require.Equal(t, 30, topTicks)

	// A range of idle windows only: no bucket, but its ticks still say the
	// range was sampled, so the significance warning does not fire on it.
	idleFrom := base.Add(10 * time.Second)
	buckets, ticks, err = api.queryASHBuckets(ctx, instID, &idleFrom, &end, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Empty(t, buckets)
	require.Equal(t, 20, ticks)
}
