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

func TestFindingPathID_DecodesEscapedSlash(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/api/v1/findings/*", func(w http.ResponseWriter, req *http.Request) {
		require.Equal(t, "ui.acceptance/mute", findingPathID(req, "mute"))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(
		w,
		httptest.NewRequest(http.MethodPost, "/api/v1/findings/ui.acceptance%2Fmute/mute", nil),
	)
	require.Equal(t, http.StatusOK, w.Code)
}

// TestScanFinding_ClusterIDIsAnUnsignedString: the spec's ClusterId. The
// field was a JSON number, which a browser rounds past 2^53.
func TestScanFinding_ClusterIDIsAnUnsignedString(t *testing.T) {
	now := time.Now().UTC()
	cluster := int64(-446744073709551616) // 18000000000000000000 as a bigint
	row := &mockRow{vals: []any{"f1", "rule", "warning", "open", "cluster", &cluster, (*uuid.UUID)(nil), "", "", "t", "d", "r", []byte(`{}`), (*string)(nil), now, now, (*time.Time)(nil), (*time.Time)(nil), (*string)(nil)}}
	f, err := scanFinding(row)
	require.NoError(t, err)
	require.NotNil(t, f.ClusterID)
	require.Equal(t, "18000000000000000000", *f.ClusterID)
	body, err := json.Marshal(f)
	require.NoError(t, err)
	require.Contains(t, string(body), `"cluster_id":"18000000000000000000"`)
}
