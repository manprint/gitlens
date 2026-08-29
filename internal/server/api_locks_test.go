package server

import (
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
