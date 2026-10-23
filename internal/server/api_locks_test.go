package server

import (
	"bytes"
	"encoding/json"
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

// The tree reaches the API as stored: a node keeps its text and wait fields,
// and a session keeps its identity and wait fields but no text.
func TestLocksAPI_PassesWaitFieldsAndTreeOnlyText(t *testing.T) {
	id := uuid.New()
	tree := `{"nodes":[{"pid":7,"query":"UPDATE t SET x = 1","blocked_by":[8],"session_id":"65e7e3480a401.7","wait_started_at":"2026-10-23T19:59:00Z","wait_duration":60}],` +
		`"sessions":[{"pid":7,"session_id":"65e7e3480a401.7","wait_duration":60,"state":"active"},{"pid":8,"session_id":"65e7e3480a408.8","state":"idle"}]}`
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{time.Now(), []byte(tree)}}}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+id.String(), http.NoBody)
	resp := httptest.NewRecorder()
	contentionTestRouter(&API{pool: pool}).ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got struct {
		Nodes    []map[string]any `json:"nodes"`
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Len(t, got.Nodes, 1)
	require.Equal(t, "UPDATE t SET x = 1", got.Nodes[0]["query"])
	require.Equal(t, "65e7e3480a401.7", got.Nodes[0]["session_id"])
	require.Equal(t, "2026-10-23T19:59:00Z", got.Nodes[0]["wait_started_at"])
	require.Equal(t, float64(60), got.Nodes[0]["wait_duration"])
	require.Len(t, got.Sessions, 2)
	for _, session := range got.Sessions {
		require.NotContains(t, session, "query")
	}
	require.Equal(t, float64(60), got.Sessions[0]["wait_duration"])
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
