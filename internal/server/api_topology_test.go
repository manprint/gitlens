package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var errQueryFailed = errors.New("query failed")

// TestTopologyAPI_HandleTopology_NilPool tests nil pool path.
func TestTopologyAPI_HandleTopology_NilPool(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/topology", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp topologyResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "123", resp.ClusterID)
	require.Len(t, resp.Topology, 0)
	require.Len(t, resp.Events, 0)
}

// TestTopologyAPI_HandleTopology_InvalidClusterID tests invalid cluster ID.
func TestTopologyAPI_HandleTopology_InvalidClusterID(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/invalid/topology", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestTopologyAPI_HandleReplication_NilPool tests nil pool path.
func TestTopologyAPI_HandleReplication_NilPool(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/replication?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp replicationResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "123", resp.ClusterID)
	require.Len(t, resp.Edges, 0)
}

// TestTopologyAPI_HandleReplication_MissingTimeRange tests missing query params.
func TestTopologyAPI_HandleReplication_MissingTimeRange(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/replication", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestTopologyAPI_HandleReplication_InvalidTimeRange tests invalid time range.
func TestTopologyAPI_HandleReplication_InvalidTimeRange(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/replication?from=2024-01-02T00:00:00Z&to=2024-01-01T00:00:00Z", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestTopologyAPI_HandleReplication_InvalidClusterID tests invalid cluster ID.
func TestTopologyAPI_HandleReplication_InvalidClusterID(t *testing.T) {
	topo := NewTopologyAPI(nil)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/invalid/replication?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestTopologyAPI_HandleTopology_MockPool_EdgesAndEvents exercises the real
// query/scan/response path: one high-confidence edge with a sync_state, one
// low-confidence edge (which must carry the resolvability note), and one
// failover event.
func TestTopologyAPI_HandleTopology_MockPool_EdgesAndEvents(t *testing.T) {
	from1, to1 := uuid.New(), uuid.New()
	from2, to2 := uuid.New(), uuid.New()
	instID := uuid.New()

	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{
				{from1.String(), to1.String(), "streaming", "async", "high"},
				{from2.String(), to2.String(), "streaming", nil, "low"},
			}},
			{rows: [][]any{
				{int64(1), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "failover_detected", instID.String(), []byte(`{"reason":"promote"}`)},
			}},
		},
	}
	topo := &TopologyAPI{pool: pool}
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/topology", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp topologyResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Topology, 2)
	require.NotNil(t, resp.Topology[0].SyncState)
	require.Equal(t, "async", *resp.Topology[0].SyncState)
	require.Nil(t, resp.Topology[0].Note)
	require.Nil(t, resp.Topology[1].SyncState)
	require.NotNil(t, resp.Topology[1].Note, "a low-confidence edge must carry the resolvability note")

	require.Len(t, resp.Events, 1)
	require.Equal(t, "failover_detected", resp.Events[0].Type)
	require.NotNil(t, resp.Events[0].InstanceID)
	require.Equal(t, instID.String(), *resp.Events[0].InstanceID)
}

// TestTopologyAPI_HandleTopology_QueryEdgesError exercises the edges-query
// error branch.
func TestTopologyAPI_HandleTopology_QueryEdgesError(t *testing.T) {
	pool := &mockPool{queryErrs: []error{errQueryFailed}}
	topo := &TopologyAPI{pool: pool}
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/topology", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestTopologyAPI_HandleTopology_QueryEventsError exercises the events-query
// error branch (edges succeed, events fail).
func TestTopologyAPI_HandleTopology_QueryEventsError(t *testing.T) {
	pool := &mockPool{
		queryResults: []*mockRows{{rows: nil}},
		queryErrs:    []error{nil, errQueryFailed},
	}
	topo := &TopologyAPI{pool: pool}
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/123/topology", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestTopologyAPI_HandleReplication_MockPool_ExpandsPerMetric proves one
// metrics_replication row expands into 3 edges (write/flush/replay), each
// with a null gap for the lag values that weren't reported, and that a row
// with no upstream_id is skipped rather than crashing.
func TestTopologyAPI_HandleReplication_MockPool_ExpandsPerMetric(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{
			{from.String(), to.String(), "async", ts, nil, nil, 50.0},
			{from.String(), nil, nil, ts, nil, nil, nil}, // no upstream: must be skipped, not crash
		}}},
	}
	topo := &TopologyAPI{pool: pool}
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/clusters/123/replication?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp replicationResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Edges, 3)

	byMetric := make(map[string]replicationSeriesResp, len(resp.Edges))
	for _, e := range resp.Edges {
		require.Equal(t, from.String(), e.From)
		require.Equal(t, to.String(), e.To)
		byMetric[e.Metric] = e
	}
	require.NotNil(t, byMetric["replay_lag_sec"].Series[0].Value)
	require.Equal(t, 50.0, *byMetric["replay_lag_sec"].Series[0].Value)
	require.Nil(t, byMetric["write_lag_sec"].Series[0].Value)
	require.Nil(t, byMetric["flush_lag_sec"].Series[0].Value)
}

// TestTopologyAPI_HandleReplication_QueryError exercises the metrics-query
// error branch.
func TestTopologyAPI_HandleReplication_QueryError(t *testing.T) {
	pool := &mockPool{queryErrs: []error{errQueryFailed}}
	topo := &TopologyAPI{pool: pool}
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/clusters/123/replication?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
