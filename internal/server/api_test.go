package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/stretchr/testify/require"
)

func TestAPI_NewAPI_Signature(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	require.NotNil(t, api)
}

func TestAPI_RegisterRoutes(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	require.NotPanics(t, func() { api.RegisterRoutes(r) })
}

func TestAPI_InstanceCollectionRouteReturnsArrayWithoutPool(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	NewAPI(nil).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, "[]", rec.Body.String())
}

func TestParseTimeParam(t *testing.T) {
	t.Parallel()
	_, err := parseTimeParam("")
	require.Error(t, err)
	_, err = parseTimeParam("not-a-time")
	require.Error(t, err)
	t1, err := parseTimeParam("2026-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, 2026, t1.Year())
	t2, err := parseTimeParam(time.Now().Format(time.RFC3339Nano))
	require.NoError(t, err)
	require.False(t, t2.IsZero())
	t3, err := parseTimeParam("2026-01-01T00:00:00+07:00")
	require.NoError(t, err)
	_ = t3
	// space-separated fallback layout, with a numeric offset
	t4, err := parseTimeParam("2026-01-02 15:04:05+02:00")
	require.NoError(t, err)
	_ = t4
	// space-separated fallback layout, with Z
	t5, err := parseTimeParam("2026-01-02 15:04:05Z")
	require.NoError(t, err)
	_ = t5
}

func TestParseStep(t *testing.T) {
	t.Parallel()
	_, err := parseStep("")
	require.Error(t, err)
	_, err = parseStep("not")
	require.Error(t, err)
	_, err = parseStep("0s")
	require.Error(t, err)
	_, err = parseStep("-5s")
	require.Error(t, err)
	d, err := parseStep("60s")
	require.NoError(t, err)
	require.Equal(t, 60*time.Second, d)
	d, err = parseStep("1m")
	require.NoError(t, err)
	require.Equal(t, time.Minute, d)
}

func TestParseLimitParam(t *testing.T) {
	t.Parallel()
	v, err := parseLimitParam("", 100, 1000)
	require.NoError(t, err)
	require.Equal(t, 100, v)
	v, err = parseLimitParam("50", 100, 1000)
	require.NoError(t, err)
	require.Equal(t, 50, v)
	v, err = parseLimitParam("5000", 100, 1000)
	require.NoError(t, err)
	require.Equal(t, 1000, v)
	_, err = parseLimitParam("0", 100, 1000)
	require.Error(t, err)
	_, err = parseLimitParam("-5", 100, 1000)
	require.Error(t, err)
	_, err = parseLimitParam("abc", 100, 1000)
	require.Error(t, err)
}

func TestIsUp(t *testing.T) {
	t.Parallel()
	now := time.Now()
	require.True(t, isUp(now.Add(-30*time.Second), now))
	require.True(t, isUp(now.Add(-60*time.Second), now)) // inclusive at 60s
	require.False(t, isUp(now.Add(-61*time.Second), now))
	require.False(t, isUp(now.Add(-200*time.Second), now))
	require.True(t, isUp(now, now))
}

func TestHealthForCluster(t *testing.T) {
	t.Parallel()
	lag := func(v float64) *float64 { return &v }
	require.Equal(t, "unknown", healthForCluster(0, false, 0, nil))
	require.Equal(t, "critical", healthForCluster(0, false, 1, nil))
	require.Equal(t, "critical", healthForCluster(0, true, 2, nil))
	require.Equal(t, "degraded", healthForCluster(1, true, 2, nil))
	require.Equal(t, "ok", healthForCluster(1, false, 2, nil))
	require.Equal(t, "ok", healthForCluster(1, false, 1, nil))
	require.Equal(t, "degraded", healthForCluster(1, false, 2, lag(150.5)), "a lagging replica must degrade health even with every instance up")
	require.Equal(t, "ok", healthForCluster(1, false, 2, lag(0)), "zero lag is not a reason to degrade")
	require.Equal(t, "ok", healthForCluster(1, false, 2, lag(0.4)), "a busy primary's standbys are always milliseconds behind")
	require.Equal(t, "ok", healthForCluster(1, false, 2, lag(30)))
	require.Equal(t, "degraded", healthForCluster(1, false, 2, lag(30.5)), "above the replica.lag_high threshold")
	require.Equal(t, "critical", healthForCluster(2, false, 2, nil), "split-brain (2 primaries) is critical even with nothing else wrong")
}

func TestWriteErrorAndJSON(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "bad", "detail")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var e apiError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
	require.Equal(t, "bad", e.Error)
	require.Equal(t, "detail", e.Detail)

	w2 := httptest.NewRecorder()
	writeJSON(w2, http.StatusOK, map[string]string{"a": "b"})
	require.Equal(t, http.StatusOK, w2.Code)
	require.Contains(t, w2.Body.String(), `"a":"b"`)
}

func TestAPI_Clusters_EmptyWhenNoDB(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp)
	require.NotContains(t, w.Body.String(), `"cluster_id":123`)
}

func TestAPI_Metrics_MissingParams(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	cases := []string{
		"/api/v1/metrics/query?metric=pg_backends",
		"/api/v1/metrics/query?metric=x&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&step=60s",
		"/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&step=60s",
	}
	for _, url := range cases {
		req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, url)
		var e apiError
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &e))
		require.NotEmpty(t, e.Error)
	}
}

func TestAPI_Metrics_InvalidInstanceID(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=bad&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&step=60s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Metrics_InvalidTime(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=bad&to=2026-01-01T01:00:00Z&step=60s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=bad&step=60s", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Metrics_InvalidRange(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T01:00:00Z&to=2026-01-01T00:00:00Z&step=60s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Metrics_InvalidStep(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&step=0s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&step=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Metrics_StepTooSmall(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&step=4s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var errResp apiError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	require.NotEmpty(t, errResp.Error)
}

// TestAPI_Metrics_OverflowingPointCountIsRefused: a 1ns step over a span near
// the duration limit is past int range, and the converted count wrapped to a
// value under the cap, so the query ran unbounded. A one-year step over a
// span longer than a duration can hold was answered from its first 292 years.
func TestAPI_Metrics_OverflowingPointCountIsRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, step string }{
		{"1ns step over the whole span", "1ns"},
		{"one year step over a span longer than a duration", "8760h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			NewAPI(nil).RegisterRoutes(r)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=x&instance_id=11111111-1111-1111-1111-111111111111&from=0001-01-01T00:00:00Z&to=9999-12-31T23:59:59Z&step="+tc.step, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
		})
	}
}

func TestAPI_Metrics_NullForGap(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/query?metric=pg_backends&instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T00:00:00Z&to=2026-01-01T00:03:00Z&step=60s", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"value":null`)
	require.NotContains(t, body, `"value":0`)
	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Series, 3)
	for _, pt := range resp.Series {
		require.Nil(t, pt.Value)
	}
}

func TestAPI_Events_LimitCapped(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=5000", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestAPI_Events_Validation(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	// invalid limit
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=bad", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=0", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid cluster_id
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?cluster_id=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid from/to
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?from=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?to=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/events?from=2026-01-01T01:00:00Z&to=2026-01-01T00:00:00Z", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Instance_NotFound(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/11111111-1111-1111-1111-111111111111", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	var errResp apiError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	require.Contains(t, strings.ToLower(errResp.Error), "not found")
}

func TestAPI_Instance_InvalidID(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/bad-uuid", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Statements_ComparableScope(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp statementsResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "cluster", resp.ComparableScope)
}

func TestAPI_Statements_Validation(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	// missing instance_id
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid instance_id
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid limit
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&limit=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid order_by
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&order_by=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid from/to
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&from=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&to=bad", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	// invalid range
	req = httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&from=2026-01-01T01:00:00Z&to=2026-01-01T00:00:00Z", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAPI_Statements_OrderByVariants(t *testing.T) {
	t.Parallel()
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	valid := []string{"calls", "calls_rate", "total_exec_time", "exec_time", "rows", "shared_blks_hit", "wal_bytes", "mean_exec_time"}
	for _, ob := range valid {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id=11111111-1111-1111-1111-111111111111&order_by="+ob, http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, ob)
	}
}

func TestAPI_Events_LimitDefaultAndMax(t *testing.T) {
	t.Parallel()
	// nil pool returns default limit handling
	api := NewAPI(nil)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

// The tests below exercise the API handlers' real (non-nil-pool) branches
// with a mockPool instead of a database, closing V002-F07's L1-coverage gap.
// See internal/server/mockpool_test.go for mockPool/mockRow/mockRows.

func TestAPI_MockPool_Clusters_TwoInstances(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(9001)
	cidDB := store.ToDB(cid)
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{cidDB, nil, "manual"}}}, // clusters: one cluster, no name
			{rows: [][]any{ // instances for that cluster
				{cidDB, uuid.NewString(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now()},
				{cidDB, uuid.NewString(), "10.0.0.2", 5432, "standby", 170000, "T0", time.Now().Add(-10 * time.Minute)}, // stale => down
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, cid.String(), resp[0].ClusterID)
	require.Equal(t, 2, resp[0].InstanceCount)
	require.Equal(t, "degraded", resp[0].Health) // has primary, but one instance down
	require.NotNil(t, resp[0].Primary)
	require.Equal(t, 1, resp[0].StandbyCount)
}

// TestAPI_MockPool_Clusters_TopologyEdgesAndLag exercises the topology_edges
// scan loop (row 132) and the bounded max-replay-lag query (row 131) — both
// added this session and, until now, never reached by any mockPool test
// (whose queued query results never included a third result set for the
// topology query at all, so it always fell through to the mock's default
// empty result).
func TestAPI_MockPool_Clusters_TopologyEdgesAndLag(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(9003)
	cidDB := store.ToDB(cid)
	from := uuid.New()
	toSync := uuid.New()
	toLow := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{cidDB, nil, "manual"}}},
			{rows: [][]any{
				{cidDB, from.String(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now()},
				{cidDB, toSync.String(), "10.0.0.2", 5432, "standby", 170000, "T0", time.Now()},
			}},
			{rows: [][]any{
				{cidDB, from.String(), toSync.String(), "streaming", "sync", "high"},
				{cidDB, from.String(), toLow.String(), "streaming", nil, "low"},
			}},
			// standby names, standby edges and sender names, then sync states
			nil, nil, nil, nil,
			{rows: [][]any{{cidDB, 0.75}}}, // max replay lag
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Len(t, resp[0].Topology, 2)
	require.Equal(t, 1, resp[0].SyncStandbyCount)
	require.NotNil(t, resp[0].MaxReplayLagSeconds)
	require.Equal(t, 0.75, *resp[0].MaxReplayLagSeconds)
	require.Equal(t, "ok", resp[0].Health, "sub-second lag is below the degraded threshold")

	var sawSync, sawLowWithNote bool
	for _, e := range resp[0].Topology {
		if e.SyncState != nil && *e.SyncState == "sync" {
			sawSync = true
		}
		if e.Confidence == "low" {
			require.NotNil(t, e.Note)
			sawLowWithNote = true
		}
	}
	require.True(t, sawSync)
	require.True(t, sawLowWithNote)
}

// TestAPI_MockPool_Clusters_MeasuredZeroLag: a caught-up standby measures a
// replay lag of exactly 0. That is a measurement, so the cluster must carry
// it. The UI reads a missing key as "not measured", which would show a
// fully caught-up cluster as unknown lag.
func TestAPI_MockPool_Clusters_MeasuredZeroLag(t *testing.T) {
	t.Parallel()
	cidDB := store.ToDB(pgtype.ClusterID(9004))
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{cidDB, nil, "manual"}}},
			{rows: [][]any{
				{cidDB, uuid.NewString(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now()},
				{cidDB, uuid.NewString(), "10.0.0.2", 5432, "standby", 170000, "T0", time.Now()},
			}},
			nil, nil, nil, nil, nil,
			{rows: [][]any{{cidDB, 0.0}}}, // max replay lag
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.NotNil(t, resp[0].MaxReplayLagSeconds, "a measured zero is still a measurement")
	require.Equal(t, 0.0, *resp[0].MaxReplayLagSeconds)
	require.Equal(t, "ok", resp[0].Health)
	require.Contains(t, w.Body.String(), `"max_replay_lag_seconds":0`)
}

// TestAPI_MockPool_Clusters_SkipsClusterWithoutInstances: a cluster row with
// no instance behind it is left by a registration that failed. Its health is
// "unknown", which the spec does not allow, and the fleet summary cannot
// count it. The list leaves it out, so the total matches the clusters shown.
func TestAPI_MockPool_Clusters_SkipsClusterWithoutInstances(t *testing.T) {
	t.Parallel()
	orphan := store.ToDB(pgtype.ClusterID(9005))
	withInstances := store.ToDB(pgtype.ClusterID(9006))
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{orphan, nil, "manual"}, {withInstances, nil, "manual"}}},
			{rows: [][]any{
				{withInstances, uuid.NewString(), "10.0.0.5", 5432, "primary", 170000, "T0", time.Now()},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, pgtype.ClusterID(9006).String(), resp[0].ClusterID)
	require.Equal(t, "ok", resp[0].Health)
}

// TestAPI_MockPool_Clusters_MostRecentPrimaryWins covers a real bug found
// live running SYS-REPL-001: after a real failover, the old (now-dead)
// primary's own instances.role row still says "primary" for a while (a
// promote doesn't retroactively update the instance it promoted away
// from) — picking the first role="primary" row by addr/port order can
// pick the stale one over the genuinely current one. The fix: prefer the
// most recently-seen "primary" row.
func TestAPI_MockPool_Clusters_MostRecentPrimaryWins(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(9003)
	cidDB := store.ToDB(cid)
	staleID := uuid.NewString()
	freshID := uuid.NewString()
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{cidDB, nil, "manual"}}},
			{rows: [][]any{
				// "pg-primary" sorts before "pg-standby" by addr, but it's
				// the stale, dead one; the freshly-promoted instance must
				// still win.
				{cidDB, staleID, "pg-primary", 5432, "primary", 170000, "T0", time.Now().Add(-2 * time.Minute)},
				{cidDB, freshID, "pg-standby", 5432, "primary", 170000, "T0", time.Now()},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.NotNil(t, resp[0].Primary)
	require.Equal(t, freshID, *resp[0].Primary)
}

func TestAPI_MockPool_Clusters_QueryError(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryErrs: []error{errors.New("db down")}}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPI_MockPool_Instance_Found(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(9002)
	iid := uuid.New()
	pool := &mockPool{
		queryRowVals: []*mockRow{
			{vals: []any{iid.String(), store.ToDB(cid), "10.0.0.9", 5432, "primary", 170000, "T0", time.Now()}},
		},
		queryResults: []*mockRows{
			{rows: [][]any{
				{"app", true, nil},
				{"template0", false, "system db"},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+iid.String(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp instanceDetailResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, iid.String(), resp.InstanceID)
	require.Equal(t, cid.String(), resp.ClusterID)
	require.Len(t, resp.Databases, 2)
	require.Equal(t, 1, resp.DatabasesNotMonitored)
}

func TestAPI_MockPool_Instance_NotFound(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestAPI_MockPool_MetricsQuery_NullAndValue(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(3 * time.Minute)
	// one point landing in the 2nd of 3 one-minute buckets
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{from.Add(90 * time.Second), 42.0, int64(1)}}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	url := "/api/v1/metrics/query?metric=pg_backends&instance_id=" + iid.String() +
		"&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&step=60s"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Series, 3)
	require.Nil(t, resp.Series[0].Value)
	require.NotNil(t, resp.Series[1].Value)
	require.Equal(t, 42.0, *resp.Series[1].Value)
	require.Nil(t, resp.Series[2].Value)
}

func TestAPI_MockPool_MetricsQuery_RefusesSeveralSeries(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	// Two label sets scraped in the same cycle share one timestamp.
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{
				{from.Add(10 * time.Second), 42.0, int64(1)},
				{from.Add(10 * time.Second), 3.0, int64(2)},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	url := "/api/v1/metrics/query?metric=pg_io_hits&instance_id=" + iid.String() +
		"&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&step=60s"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "several series")
}

func TestAPI_MockPool_MetricsQuery_MeanWithinOneSeries(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Minute)
	// One series with three samples in the same bucket answers with their mean,
	// not with the last scrape (2.0), which would count one interval for the bucket.
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{
				{from.Add(10 * time.Second), 1.0, int64(7)},
				{from.Add(25 * time.Second), 0.0, int64(7)},
				{from.Add(40 * time.Second), 2.0, int64(7)},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	url := "/api/v1/metrics/query?metric=pg_io_hits&instance_id=" + iid.String() +
		"&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&step=60s"
	req := httptest.NewRequest(http.MethodGet, url, http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp metricsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Series, 1)
	require.NotNil(t, resp.Series[0].Value)
	require.Equal(t, 1.0, *resp.Series[0].Value)
}

func TestAPI_MockPool_Events_WithRows(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(9003)
	iid := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{
				{int64(1), time.Now(), "failover_detected", store.ToDB(cid), iid.String(), []byte(`{"a":1}`)},
			}},
		},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []eventResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)
	require.Equal(t, "failover_detected", resp[0].Type)
	require.NotNil(t, resp[0].ClusterID)
	require.Equal(t, cid.String(), *resp[0].ClusterID)
}

func TestAPI_MockPool_Statements_WithRows(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{
				{int64(9007199254740993), "app", "SELECT 1", 10.0, 100.0, 5.0, 1.0, 2.0, 3.0, 4.0, 10.0},
			}},
		},
		queryRowVals: []*mockRow{{vals: []any{false}}},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+iid.String(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp statementsResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Statements, 1)
	got := resp.Statements[0]
	require.Equal(t, "9007199254740993", got.QueryID)
	require.Equal(t, "SELECT 1", got.QueryText)
	// The fields the UI's cache-hit ratio and temp bytes are built from.
	require.Equal(t, 1.0, *got.SharedBlksHit)
	require.Equal(t, 2.0, *got.SharedBlksRead)
	require.Equal(t, 3.0, *got.WALBytes)
	require.Equal(t, 4.0, *got.TempBlksWritten)
	require.Equal(t, 10.0, *got.MeanExec)
	require.False(t, resp.Truncated)
	require.False(t, resp.Disabled)
	require.NotContains(t, w.Body.String(), "disabled")
}

func TestAPI_Statements_OrderByTempBlocks(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryResults: []*mockRows{{}}, queryRowVals: []*mockRow{{vals: []any{false}}}}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?order_by=temp_blks_written&instance_id="+uuid.NewString(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAPI_MockPool_Statements_TruncatedQueryError(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{}}},
		queryRowVals: []*mockRow{{err: errors.New("boom")}},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+iid.String(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPI_MockPool_Statements_Truncated(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{}}},
		queryRowVals: []*mockRow{{vals: []any{true}}},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+iid.String(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp statementsResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Truncated)
}

// A skipped stat_statements check used to answer exactly like an instance
// with no statements: the interface's disabled state was unreachable.
func TestAPI_MockPool_Statements_ReportsTheSkippedCheck(t *testing.T) {
	t.Parallel()
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{}}},
		queryRowVals: []*mockRow{{vals: []any{false}}, {vals: []any{"missing extension pg_stat_statements"}}},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+uuid.NewString(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp statementsResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Disabled)
	require.NotNil(t, resp.DisabledReason)
	require.Equal(t, "missing extension pg_stat_statements", *resp.DisabledReason)
}

func TestAPI_MockPool_Statements_SkipLookupError(t *testing.T) {
	t.Parallel()
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{}}},
		queryRowVals: []*mockRow{{vals: []any{false}}, {err: errors.New("boom")}},
	}
	api := &API{pool: pool}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/statements?instance_id="+uuid.NewString(), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAPI_InstanceTruncated_QueryVariants(t *testing.T) {
	t.Parallel()
	iid := uuid.New()
	from := time.Now().Add(-time.Hour)
	to := time.Now()

	t.Run("no range, true", func(t *testing.T) {
		t.Parallel()
		pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{true}}}}
		api := &API{pool: pool}
		truncated, err := api.instanceTruncated(context.Background(), iid, nil, nil)
		require.NoError(t, err)
		require.True(t, truncated)
	})

	t.Run("from only", func(t *testing.T) {
		t.Parallel()
		pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}
		api := &API{pool: pool}
		truncated, err := api.instanceTruncated(context.Background(), iid, &from, nil)
		require.NoError(t, err)
		require.False(t, truncated)
	})

	t.Run("to only", func(t *testing.T) {
		t.Parallel()
		pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{false}}}}
		api := &API{pool: pool}
		truncated, err := api.instanceTruncated(context.Background(), iid, nil, &to)
		require.NoError(t, err)
		require.False(t, truncated)
	})

	t.Run("from and to", func(t *testing.T) {
		t.Parallel()
		pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{true}}}}
		api := &API{pool: pool}
		truncated, err := api.instanceTruncated(context.Background(), iid, &from, &to)
		require.NoError(t, err)
		require.True(t, truncated)
	})

	t.Run("query error", func(t *testing.T) {
		t.Parallel()
		pool := &mockPool{queryRowVals: []*mockRow{{err: fmt.Errorf("boom")}}}
		api := &API{pool: pool}
		_, err := api.instanceTruncated(context.Background(), iid, nil, nil)
		require.Error(t, err)
	})
}

// TestAPI_JSONWrites_RefuseOversizedBody: the write routes must not buffer a
// body of arbitrary size. A 1 MiB body is refused with 413 before the decoder
// reads it, on every route that takes JSON.
func TestAPI_JSONWrites_RefuseOversizedBody(t *testing.T) {
	t.Parallel()
	body := `{"junk":"` + strings.Repeat("A", 1<<20) + `"}`
	api := &API{pool: &mockPool{}}
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	alerts := newAlertAPIForTest(&mockPool{}, &alertAPITestStore{})
	ar := alertRouter(alerts)
	for _, tc := range []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{"rule update", ar, http.MethodPut, "/api/v1/alert-rules/custom"},
		{"silence create", ar, http.MethodPost, "/api/v1/silences"},
		{"finding mute", r, http.MethodPost, "/api/v1/findings/some-id/mute"},
	} {
		w := httptest.NewRecorder()
		tc.handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(body)))
		require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// TestAPI_MockPool_Clusters_QueriesDoNotGrowWithClusters: the cluster list reads
// every cluster's instances, edges, sync states and lag in one query each, so
// it issues the same number of queries for one cluster as for twenty.
func TestAPI_MockPool_Clusters_QueriesDoNotGrowWithClusters(t *testing.T) {
	t.Parallel()
	countQueries := func(n int) int {
		clusters := make([][]any, 0, n)
		for i := 0; i < n; i++ {
			clusters = append(clusters, []any{int64(9100 + i), nil, "manual"})
		}
		pool := &mockPool{queryResults: []*mockRows{{rows: clusters}}}
		api := &API{pool: pool}
		r := chi.NewRouter()
		api.RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return pool.queryCalls
	}
	require.Equal(t, countQueries(1), countQueries(20))
}

// A payload that cannot be encoded, such as a NaN metric, must not reach the
// client as an empty 200: the status is only written once the body is known.
func TestAPI_WriteJSON_EncodeFailureIsServerError(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]any{"v": math.NaN()})
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	var got apiError
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), w.Body.String())
	require.Equal(t, "encode failed", got.Error)
}

func TestAPI_WriteJSON_EncodesWithTrailingNewline(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]any{"v": 1.5})
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "{\"v\":1.5}\n", w.Body.String())
}
