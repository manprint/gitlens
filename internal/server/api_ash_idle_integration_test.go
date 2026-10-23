//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// ingestIdleWindow sends a window that ticked with no sampled session the way
// the agent does: one zero-count ash_samples gauge carrying its ticks.
func ingestIdleWindow(t *testing.T, instID uuid.UUID, cid pgtype.ClusterID, ts time.Time, ticks string) {
	t.Helper()
	env := wire.Envelope{
		ProtocolVersion: 1,
		Instances: []wire.Instance{{
			InstanceID: instID.String(),
			ClusterID:  cid.String(),
			Results: []wire.Result{{
				Check: "ash",
				TS:    ts,
				Metrics: []wire.Metric{{
					Name:   "ash_samples",
					Value:  0,
					Kind:   "gauge",
					Labels: map[string]string{"window_seconds": "10", "window_ticks": ticks},
				}},
			}},
		}},
	}
	_, err := NewPipeline(getSharedPool(t), nil).Process(contextBackground(), env)
	require.NoError(t, err)
}

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

	// One busy window, then two idle windows as the agent ingests them.
	_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
		VALUES ($1, $2, $3, 'app', 'LWLock', 'WALWrite', 'active', 7, 10, 10, 10)`, base, store.ToDB(cid), instID)
	require.NoError(t, err)
	ingestIdleWindow(t, instID, cid, base.Add(10*time.Second), "10")
	ingestIdleWindow(t, instID, cid, base.Add(20*time.Second), "10")

	api := NewAshAPI(pool)

	// Over the three windows the instance ticked 30 times and sampled 10
	// sessions: an average of one third, not the busy window's 1.0.
	buckets, ticks, err := api.queryASHBuckets(ctx, instID, &base, &end, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Len(t, buckets, 1, "an idle window is not listed")
	require.Equal(t, 10, buckets[0].Samples)
	require.Equal(t, 30, ticks, "the idle windows' ticks count toward the total")

	// The listing is cut at its limit. The newest windows here are idle, so
	// with room for one row they must not take it: the busy window is listed.
	one, _, err := api.queryASHBuckets(ctx, instID, &base, &end, "", ashFilters{}, 1)
	require.NoError(t, err)
	require.Len(t, one, 1, "an idle window takes a place under the limit")
	require.Equal(t, 10, one[0].Samples)

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

	// A range of idle windows only has no bucket: idle time is a gap in the
	// listing, not a zero point (the /ash contract). Its ticks still say the
	// range was sampled, so the significance warning does not fire on it.
	idleFrom := base.Add(10 * time.Second)
	buckets, ticks, err = api.queryASHBuckets(ctx, instID, &idleFrom, &end, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Empty(t, buckets)
	require.Equal(t, 20, ticks)
}

// INT-ASH-017: the idle windows of a range do not take the group slots of its
// busy window. Nine idle windows used to count as nine buckets, which cut the
// groups kept under the row limit down to one.
func TestIntASH017_IdleWindowsDoNotFoldGroups(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(5252)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)

	for _, key := range []struct{ wet, we string }{{"LWLock", "WALWrite"}, {"Lock", "transactionid"}, {"IO", "DataFileRead"}} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, $2, $3, 'app', $4, $5, 'active', NULL, 4, 10, 10)`, base, store.ToDB(cid), instID, key.wet, key.we)
		require.NoError(t, err)
	}
	for i := 1; i <= 9; i++ {
		ingestIdleWindow(t, instID, cid, base.Add(time.Duration(i*10)*time.Second), "10")
	}

	groups, ticks, err := NewAshAPI(pool).queryASHGrouped(ctx, instID, base, base.Add(100*time.Second), 10, "", ashFilters{}, []string{"wait_event_type"}, 10)
	require.NoError(t, err)
	require.Len(t, groups, 3, "every wait type of the busy window is kept under the limit")
	require.Equal(t, 100, ticks)
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.WaitEventType] = true
	}
	require.Equal(t, map[string]bool{"LWLock": true, "Lock": true, "IO": true}, seen)
}

// INT-ASH-018: a filter narrows the samples, not the ticks. The total of a
// grouped answer and of the top list counts every window of the range, the
// ones the filter drops included.
func TestIntASH018_FilterKeepsTheRangeTicks(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(5353)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	end := base.Add(20 * time.Second)

	// A busy window of LWLock samples, then an idle window in the next bucket.
	_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
		VALUES ($1, $2, $3, 'app', 'LWLock', 'WALWrite', 'active', 7, 10, 10, 10)`, base, store.ToDB(cid), instID)
	require.NoError(t, err)
	ingestIdleWindow(t, instID, cid, base.Add(10*time.Second), "10")

	api := NewAshAPI(pool)
	filter := ashFilters{waitEventType: "LWLock"}

	// Only the busy window matches the filter, so only its bucket is listed.
	// Its total still counts both windows, the idle one too.
	groups, ticks, err := api.queryASHGrouped(ctx, instID, base, end, 10, "", filter, []string{"wait_event_type"}, 1000)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Equal(t, 10, groups[0].Samples)
	require.Equal(t, 20, ticks, "the filter narrows the samples, not the ticks")

	top, topTicks, err := api.queryASHTop(ctx, instID, &base, &end, "", filter, 10)
	require.NoError(t, err)
	require.Len(t, top, 1)
	require.Equal(t, 10, top[0].Samples)
	require.Equal(t, 20, top[0].Ticks)
	require.InDelta(t, 10.0/20.0, *top[0].AvgActive, 1e-9)
	require.Equal(t, 20, topTicks)
}

// INT-ASH-019: a range whose samples carry no queryid still ticked. Its top
// list is empty, and its significance total is the range's ticks, not zero.
func TestIntASH019_TopTotalWithoutQueryIDRows(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	instID := uuid.New()
	cid := pgtype.ClusterID(5454)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	end := base.Add(70 * time.Second)

	// One busy window with no queryid (compute_query_id off), then six idle
	// windows: 70 ticks in all.
	_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
		VALUES ($1, $2, $3, 'app', 'CPU', 'CPU', 'active', NULL, 10, 10, 10)`, base, store.ToDB(cid), instID)
	require.NoError(t, err)
	for i := 1; i <= 6; i++ {
		ingestIdleWindow(t, instID, cid, base.Add(time.Duration(i*10)*time.Second), "10")
	}

	top, ticks, err := NewAshAPI(pool).queryASHTop(ctx, instID, &base, &end, "", ashFilters{}, 10)
	require.NoError(t, err)
	require.Empty(t, top)
	require.Equal(t, 70, ticks, "the windows ticked with or without a queryid")

	// Over HTTP the empty list must not read as a range that was never sampled.
	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ash/top?instance_id="+instID.String()+"&from="+base.Format(time.RFC3339)+"&to="+end.Format(time.RFC3339), nil))
	require.Equal(t, http.StatusOK, w.Code)
	resp := ashTopResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Empty(t, resp.Entries)
	require.Nil(t, resp.Warning, "70 ticks are enough to be significant")
}

// INT-ASH-020: an idle range has no buckets by design: idle time is a gap in
// the listing, not a zero point. Over HTTP it is an empty list with no warning
// when its ticks are enough, and an empty list with the warning when nothing
// was sampled in it.
func TestIntASH020_IdleRangeIsEmptyWithoutWarning(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	instID := uuid.New()
	cid := pgtype.ClusterID(5555)
	base := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	end := base.Add(70 * time.Second)
	for i := 0; i < 7; i++ {
		ingestIdleWindow(t, instID, cid, base.Add(time.Duration(i*10)*time.Second), "10")
	}

	r := chi.NewRouter()
	NewAshAPI(pool).RegisterRoutes(r)
	get := func(from, to time.Time) ashResponse {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String()+"&from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), nil))
		require.Equal(t, http.StatusOK, w.Code)
		resp := ashResponse{}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		return resp
	}

	idle := get(base, end)
	require.NotNil(t, idle.Buckets, "an empty listing is [], not null")
	require.Empty(t, idle.Buckets)
	require.Nil(t, idle.Warning, "70 idle ticks are enough to be significant")

	none := get(base.Add(-time.Hour), base.Add(-30*time.Minute))
	require.NotNil(t, none.Buckets)
	require.Empty(t, none.Buckets)
	require.NotNil(t, none.Warning)
	require.Contains(t, *none.Warning, "fewer than 60 samples")
}
