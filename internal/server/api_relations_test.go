package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func relationRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

func TestRelationAPI_InvalidInstanceID(t *testing.T) {
	h := relationRouter(NewAPI(nil))
	for _, path := range []string{
		"/api/v1/instances/not-a-uuid/tables",
		"/api/v1/instances/not-a-uuid/indexes",
		"/api/v1/instances/not-a-uuid/bloat",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestRelationAPI_TablesRejectInvalidOrder(t *testing.T) {
	rec := httptest.NewRecorder()
	relationRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/v1/instances/"+uuid.New().String()+"/tables?order_by=unknown", http.NoBody))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRelationAPI_NilPoolReturnsStableEmptyPayloads(t *testing.T) {
	h := relationRouter(NewAPI(nil))
	id := uuid.New().String()
	for _, path := range []string{
		"/api/v1/instances/" + id + "/tables?limit=0",
		"/api/v1/instances/" + id + "/indexes?limit=999",
		"/api/v1/instances/" + id + "/bloat?limit=10",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, path)
		var payload relationAPIResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		require.Equal(t, id, payload.InstanceID)
		require.Empty(t, payload.Items)
		require.False(t, payload.Truncated)
	}
}

func TestRelationLimit_DefaultAndCap(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{query: "", want: 50},
		{query: "?limit=25", want: 25},
		{query: "?limit=0", want: 50},
		{query: "?limit=-1", want: 50},
		{query: "?limit=999", want: 500},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.New().String()+"/tables"+tc.query, http.NoBody)
		require.Equal(t, tc.want, relationLimit(req), tc.query)
	}
}
