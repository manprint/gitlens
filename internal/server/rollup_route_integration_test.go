//go:build integration

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// refreshRollupWindow materializes [from, to) of a rollup, waiting out a
// refresh its policy job runs at the same time (SQLSTATE 55P03).
func refreshRollupWindow(t *testing.T, pool *pgxpool.Pool, view string, from, to time.Time) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := pool.Exec(context.Background(), `CALL refresh_continuous_aggregate($1::regclass, $2::timestamptz, $3::timestamptz)`, view, from, to)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55P03" || time.Now().After(deadline) {
			require.NoError(t, err)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// INT-RLP-005: raw rows only inside the last day; the rollup alone holds a day
// 60 days back (raw rows written, materialized, then deleted). A request on
// that day reads the rollup and says resolution "1h"; a recent one reads raw.
func TestINTRLP005_OldRangesReadTheRollup(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	instance := seedWorkloadInstance(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM metrics_ash WHERE instance_id=$1`, instance)
	})
	oldDay := time.Now().UTC().Truncate(24 * time.Hour).Add(-60 * day)
	recent := time.Now().UTC().Add(-2 * time.Hour)
	for _, ts := range []time.Time{oldDay.Add(2 * time.Hour), oldDay.Add(2*time.Hour + time.Minute), recent} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_statements (ts, cluster_id, instance_id, datname, queryid, calls_rate, exec_time_rate_ms, sample_seconds)
			VALUES ($1, 1, $2, 'app', 42, 2, 4, 60)`, ts, instance)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, 1, $2, 'app', 'IO', 'DataFileRead', 'active', 42, 5, 10, 10)`, ts, instance)
		require.NoError(t, err)
	}
	refreshRollupWindow(t, pool, "metrics_statements_1h", oldDay, oldDay.Add(24*time.Hour))
	refreshRollupWindow(t, pool, "metrics_ash_1h", oldDay, oldDay.Add(24*time.Hour))
	// Only this instance's old raw rows go: drop_chunks would take the rows
	// other tests keep in the shared store.
	for _, table := range []string{"metrics_statements", "metrics_ash"} {
		_, err := pool.Exec(ctx, `DELETE FROM `+table+` WHERE instance_id=$1 AND ts < $2`, instance, oldDay.Add(24*time.Hour))
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	NewAshAPI(pool).RegisterRoutes(r)
	get := func(path string, q url.Values, into any) {
		t.Helper()
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?"+q.Encode(), http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), into))
	}
	old := url.Values{"instance_id": {instance.String()}, "from": {oldDay.Format(time.RFC3339)}, "to": {oldDay.Add(24 * time.Hour).Format(time.RFC3339)}}

	var stmts statementsResp
	get("/api/v1/statements", old, &stmts)
	require.Equal(t, resolutionHourly, stmts.Resolution)
	require.Len(t, stmts.Statements, 1)
	require.InDelta(t, 2*120, *stmts.Statements[0].Calls, 1e-9, "hourly average x scraped seconds")

	var top ashTopResponse
	get("/api/v1/ash/top", old, &top)
	require.Equal(t, resolutionHourly, top.Resolution)
	require.Len(t, top.Entries, 1)
	require.Equal(t, 10, top.Entries[0].Samples)

	var grouped ashResponse
	q := url.Values{"group_by": {"wait_event_type"}}
	for k, v := range old {
		q[k] = v
	}
	get("/api/v1/ash", q, &grouped)
	require.Equal(t, resolutionHourly, grouped.Resolution)
	require.NotEmpty(t, grouped.Buckets)

	recentQ := url.Values{"instance_id": {instance.String()}, "from": {recent.Add(-time.Minute).Format(time.RFC3339)}}
	get("/api/v1/statements", recentQ, &stmts)
	require.Equal(t, resolutionRaw, stmts.Resolution)
	require.Len(t, stmts.Statements, 1)
}
