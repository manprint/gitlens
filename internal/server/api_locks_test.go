package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func contentionTestRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.registerContentionRoutes(r)
	return r
}

func TestLocksAPI_StalenessThreshold(t *testing.T) {
	now := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	require.False(t, contentionStale(now.Add(-30*time.Second), now))
	require.True(t, contentionStale(now.Add(-31*time.Second), now))
}

func TestLocksAPI_AbsentTreeIsEmptyNotFound(t *testing.T) {
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+id.String(), http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"stale":true`)
	require.Contains(t, resp.Body.String(), `"nodes":[]`)
}

func TestLocksAPI_InvalidInstance(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id=nope", http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestActivityAPI_GroupsByState(t *testing.T) {
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestDatabasesAPI_ReportsSkipReason(t *testing.T) {
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/databases", http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
}

// A stored tree that does not decode is not an empty tree. It used to answer
// as a fresh 200 with no contention, which hid the fault.
func TestLocksAPI_UnreadableTreeIsServerError(t *testing.T) {
	id := uuid.New()
	ts := time.Now()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{ts, []byte(`{"nodes":{"pid":1},"sessions":[]}`)}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+id.String(), http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusInternalServerError, resp.Code, resp.Body.String())
	require.NotContains(t, resp.Body.String(), `"stale":false`)
}

// A tree that omits its lists still answers with empty lists, not null.
func TestLocksAPI_TreeWithoutListsKeepsEmptyLists(t *testing.T) {
	id := uuid.New()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{time.Now(), []byte(`{}`)}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+id.String(), http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), `"nodes":[]`)
	require.Contains(t, resp.Body.String(), `"sessions":[]`)
}

// Labels that do not decode leave the metric unlabelled, and the failure is
// logged rather than dropped without a trace.
func TestActivityAPI_LogsUnreadableLabels(t *testing.T) {
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	id := uuid.New()
	pool := &mockPool{queryResults: []*mockRows{{rows: [][]any{{time.Now(), "pg_backends", []byte(`[1]`), 3.0}}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody)
	resp := httptest.NewRecorder()
	r := chi.NewRouter()
	(&API{pool: pool}).registerContentionRoutes(r)
	r.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Contains(t, logged.String(), "decode labels of metric pg_backends")
}
