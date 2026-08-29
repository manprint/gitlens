package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func findingsTestRouter() *chi.Mux {
	r := chi.NewRouter()
	NewAPI(nil).RegisterRoutes(r)
	return r
}

func TestFindingsAPI_RequiresDatabase(t *testing.T) {
	r := findingsTestRouter()
	for _, path := range []string{"/api/v1/findings", "/api/v1/findings/id", "/api/v1/advisor/rules"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if path == "/api/v1/advisor/rules" {
			require.Equal(t, http.StatusOK, w.Code)
		} else {
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
		}
	}
}

func TestAdvisorRulesAPI_ListsCatalogue(t *testing.T) {
	w := httptest.NewRecorder()
	findingsTestRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/advisor/rules", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "query.slow_mean")
	require.Contains(t, w.Body.String(), "index.unused")
}

func TestFindingsAPI_NilDatabaseForMutationsAndLookup(t *testing.T) {
	r := findingsTestRouter()
	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/findings/id"},
		{http.MethodPost, "/api/v1/findings/id/mute"},
		{http.MethodDelete, "/api/v1/findings/id/mute"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
	}
}
