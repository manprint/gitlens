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
)

func settingsRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

func TestSettingsAPI_ChangedSinceFilter(t *testing.T) {
	now := time.Now().UTC()
	value := "64"
	id := uuid.New()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{{"work_mem", &value, "MB", "configuration file", "user", "false", now, now, now}}}}}
	rec := httptest.NewRecorder()
	settingsRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/settings?changed_since="+now.Format(time.RFC3339), http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response settingsAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Settings, 1)
	require.Equal(t, "work_mem", response.Settings[0].Name)
}

func TestSettingsAPI_InvalidChangedSince(t *testing.T) {
	rec := httptest.NewRecorder()
	settingsRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.New().String()+"/settings?changed_since=bad", http.NoBody))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDriftAPI_ReportsOnlyDifferences(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{"work_mem", "4", first.String(), "primary", "user"},
		{"work_mem", "8", second.String(), "standby", "user"},
		{"fsync", "on", first.String(), "primary", "user"},
		{"fsync", "on", second.String(), "standby", "user"},
	}}}}
	rec := httptest.NewRecorder()
	settingsRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/7/settings-drift", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response []driftEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response, 1)
	require.Equal(t, "work_mem", response[0].Name)
}

func TestDriftAPI_ExcludesPerInstanceSettings(t *testing.T) {
	for _, name := range settingsDriftExcluded {
		require.True(t, settingIsDriftExcluded(name), name)
	}
	require.False(t, settingIsDriftExcluded("work_mem"))
}

func TestDriftAPI_SingleInstanceClusterReturnsEmpty(t *testing.T) {
	id := uuid.New()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{{"work_mem", "4", id.String(), "primary", "user"}}}}}
	rec := httptest.NewRecorder()
	settingsRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/7/settings-drift", http.NoBody))
	var response []driftEntry
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Empty(t, response)
}
