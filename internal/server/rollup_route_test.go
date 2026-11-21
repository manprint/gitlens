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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// RLP-004: a range starting exactly one hour past the raw horizon reads raw;
// one second earlier reads the rollup. Families without a rollup or a raw
// retention never do.
func TestRLP004_RollupForBoundaries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	edge := now.Add(-30 * day).Add(time.Hour)
	require.False(t, rollupFor("metrics_statements", edge, now, 30))
	require.True(t, rollupFor("metrics_statements", edge.Add(-time.Second), now, 30))
	require.True(t, rollupFor("metrics_ash", edge.Add(-time.Second), now, 30))
	require.False(t, rollupFor("metrics_tables", now.Add(-300*day), now, 30), "no rollup for tables")
	require.False(t, rollupFor("metrics_statements", now.Add(-300*day), now, 0), "no raw retention")

	stub := &stubPolicyStore{policies: defaultPolicies()}
	ctx := context.Background()
	require.False(t, routeToRollup(ctx, stub, "metrics_statements", now.Add(-2*day), now))
	require.Zero(t, stub.policyReads, "a range within the retention floor reads no policy")
	require.Equal(t, 30, rawRetentionDays(ctx, stub, "metrics_statements"))
	require.True(t, routeToRollup(ctx, stub, "metrics_statements", now.Add(-40*day), now))
	stub.policies["metrics_statements"] = livePolicy{DropAfter: secondsOf(90 * day)}
	require.False(t, routeToRollup(ctx, stub, "metrics_statements", now.Add(-40*day), now), "the live policy decides")
	delete(stub.policies, "metrics_statements")
	require.Zero(t, rawRetentionDays(ctx, stub, "metrics_statements"), "no policy keeps raw rows")
	require.Equal(t, 30, rawRetentionDays(ctx, &stubPolicyStore{policiesErr: errors.New("down")}, "metrics_statements"), "unreadable policy: family default")
}

func rollupStub() *stubPolicyStore { return &stubPolicyStore{policies: defaultPolicies()} }

// RLP-005: the statements query reads metrics_statements_1h on an old range,
// from the hour holding from, and metrics_statements on a recent one.
func TestRLP005_StatementsReadTheRollupOnOldRanges(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for _, tc := range []struct {
		from       time.Time
		table      string
		resolution string
	}{
		{now.Add(-60*day + 30*time.Minute), "FROM metrics_statements_1h", resolutionHourly},
		{now.Add(-2 * time.Hour), "FROM metrics_statements s", resolutionRaw},
	} {
		pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}}
		r := chi.NewRouter()
		(&API{pool: pool, retention: rollupStub()}).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/statements?"+url.Values{
			"instance_id": {uuid.NewString()}, "from": {tc.from.Format(time.RFC3339)},
		}.Encode(), http.NoBody))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, pool.sqls[0], tc.table)
		var resp statementsResp
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Equal(t, tc.resolution, resp.Resolution)
		if tc.resolution == resolutionHourly {
			require.Equal(t, tc.from.Truncate(time.Hour), pool.args[0][1], "the hour holding from is read whole")
		}
	}
}

// RLP-006: on an old range the ASH endpoints refuse every dimension the rollup
// does not keep, and answer the others from it.
func TestRLP006_ASHRollupRefusesDroppedDimensions(t *testing.T) {
	t.Parallel()
	old := time.Now().UTC().Add(-60 * day).Format(time.RFC3339)
	serve := func(path string, q url.Values) (*httptest.ResponseRecorder, *searchCapturePool) {
		// /ash first reads the instance (no row) and /ash/top does not; the
		// tick count reads zero.
		rows := []*mockRow{nil, {vals: []any{0}}}
		if path == "/api/v1/ash/top" {
			rows = rows[1:]
		}
		pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: rows}}
		r := chi.NewRouter()
		(&AshAPI{pool: pool, retention: rollupStub()}).RegisterRoutes(r)
		q.Set("instance_id", uuid.NewString())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+"?"+q.Encode(), http.NoBody))
		return w, pool
	}

	w, _ := serve("/api/v1/ash", url.Values{"from": {old}, "group_by": {"usename"}})
	require.Equal(t, http.StatusBadRequest, w.Code)
	var e apiError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
	require.Equal(t, "this range is served from hourly rollups, which do not keep usename", e.Error)

	for _, tc := range []struct {
		path string
		q    url.Values
		name string
	}{
		{"/api/v1/ash", url.Values{"from": {old}, "group_by": {"wait_event_type,state"}}, "state"},
		{"/api/v1/ash", url.Values{"from": {old}, "client_addr": {"10.0.0.1"}}, "client_addr"},
		{"/api/v1/ash/top", url.Values{"from": {old}, "application_name": {"app"}}, "application_name"},
	} {
		w, _ := serve(tc.path, tc.q)
		require.Equal(t, http.StatusBadRequest, w.Code, tc.name)
		require.Contains(t, w.Body.String(), rollupDroppedMessage(tc.name))
	}

	w, pool := serve("/api/v1/ash", url.Values{"from": {old}, "group_by": {"wait_event_type"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, pool.sqls[1], "FROM metrics_ash_1h")
	var resp ashResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, resolutionHourly, resp.Resolution)
	require.Zero(t, resp.ResolutionSeconds%3600, "grouped buckets are whole hours")

	w, pool = serve("/api/v1/ash/top", url.Values{"from": {old}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, pool.sqls[0], "FROM metrics_ash_1h")
	require.Contains(t, w.Body.String(), `"resolution":"1h"`)

	w, pool = serve("/api/v1/ash", url.Values{"group_by": {"usename"}})
	require.Equal(t, http.StatusOK, w.Code, "a recent range keeps every dimension")
	require.NotContains(t, pool.sqls[1], "metrics_ash_1h")
	require.Contains(t, w.Body.String(), `"resolution":"raw"`)
}

// RLP-007: a 5-minute step of statements on an old range becomes one hour on
// the rollup; tables have no rollup and stay raw.
func TestRLP007_WorkloadStepRaisedOnRollup(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	serve := func(source, series string) (workloadSeriesResponse, *searchCapturePool) {
		pool := &searchCapturePool{mockPool: &mockPool{queryRowVals: []*mockRow{{vals: []any{true}}}}}
		r := chi.NewRouter()
		(&API{pool: pool, retention: rollupStub()}).registerWorkloadRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/workload/series?"+url.Values{
			"source": {source}, "series": {series}, "step": {"5m"},
			"from": {now.Add(-60 * day).Format(time.RFC3339)}, "to": {now.Add(-59 * day).Format(time.RFC3339)},
		}.Encode(), http.NoBody))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp workloadSeriesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp, pool
	}

	resp, pool := serve("statements", "calls")
	require.EqualValues(t, 3600, resp.StepSeconds)
	require.Equal(t, resolutionHourly, resp.Resolution)
	require.Contains(t, pool.sqls[1], "FROM metrics_statements_1h")

	resp, pool = serve("tables", "seq_scans")
	require.EqualValues(t, 600, resp.StepSeconds, "two samples of the source, unchanged")
	require.Equal(t, resolutionRaw, resp.Resolution)
	require.Contains(t, pool.sqls[1], "FROM metrics_tables")
}
