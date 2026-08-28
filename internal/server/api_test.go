package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	require.True(t, isUp(now.Add(-90*time.Second), now)) // inclusive at 90s
	require.False(t, isUp(now.Add(-91*time.Second), now))
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
				{uuid.NewString(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now()},
				{uuid.NewString(), "10.0.0.2", 5432, "standby", 170000, "T0", time.Now().Add(-10 * time.Minute)}, // stale => down
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
				{from.String(), "10.0.0.1", 5432, "primary", 170000, "T0", time.Now()},
				{toSync.String(), "10.0.0.2", 5432, "standby", 170000, "T0", time.Now()},
			}},
			{rows: [][]any{
				{from.String(), toSync.String(), "streaming", "sync", "high"},
				{from.String(), toLow.String(), "streaming", nil, "low"},
			}},
		},
		queryRowVals: []*mockRow{{vals: []any{0.75}}},
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
	require.Equal(t, "degraded", resp[0].Health)

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
				{staleID, "pg-primary", 5432, "primary", 170000, "T0", time.Now().Add(-2 * time.Minute)},
				{freshID, "pg-standby", 5432, "primary", 170000, "T0", time.Now()},
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
			{rows: [][]any{{from.Add(90 * time.Second), 42.0}}},
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
				{int64(123), "app", "SELECT 1", 10.0, 100.0, 5.0, 1.0, 2.0, 3.0, 10.0},
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
	require.Equal(t, int64(123), resp.Statements[0].QueryID)
	require.Equal(t, "SELECT 1", resp.Statements[0].QueryText)
	require.False(t, resp.Truncated)
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
