package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestFindingPathID_DecodesTheWildcardOnce: a request with no RawPath routes
// on the decoded path, so the wildcard is already decoded. A literal '%' in a
// relation name must survive, or the request names another finding.
func TestFindingPathID_DecodesTheWildcardOnce(t *testing.T) {
	for _, tc := range []struct {
		name, path, action, want string
	}{
		{"escaped percent after an escaped slash", "/api/v1/findings/rule_a/obj%2541x/mute", "mute", "rule_a/obj%41x"},
		{"escaped percent before a slash", "/api/v1/findings/rule_b/x%252Fy", "", "rule_b/x%2Fy"},
		{"ui form with an escaped percent", "/api/v1/findings/rule_a%2Fobj%2541x/mute", "mute", "rule_a/obj%41x"},
		{"plain id", "/api/v1/findings/rule_a/obj/mute", "mute", "rule_a/obj"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			r := chi.NewRouter()
			r.HandleFunc("/api/v1/findings/*", func(w http.ResponseWriter, req *http.Request) {
				got = findingPathID(req, tc.action)
			})
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, tc.path, nil))
			require.Equal(t, tc.want, got)
		})
	}
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

// A mute may not outlast maxFindingMuteDuration: an unbounded mute silences a
// critical finding for as long as the timestamp says, with no one prompted.
func TestFindingsAPI_MuteRefusesAnUntilBeyondTheCap(t *testing.T) {
	post := func(pool *mockPool, until string) *httptest.ResponseRecorder {
		r := chi.NewRouter()
		(&API{pool: pool}).registerFindingsRoutes(r)
		body := `{"reason":"accepted","until":"` + until + `"}`
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/findings/query.slow_mean/mute", strings.NewReader(body)))
		return rec
	}
	inside := time.Now().UTC().Add(maxFindingMuteDuration - time.Hour).Format(time.RFC3339)
	beyond := time.Now().UTC().Add(maxFindingMuteDuration + time.Hour).Format(time.RFC3339)

	pool := &mockPool{execRows: []int64{1}}
	require.Equal(t, http.StatusOK, post(pool, inside).Code)

	pool = &mockPool{execRows: []int64{1}}
	rec := post(pool, beyond)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Equal(t, 0, pool.execCalls, "a refused mute must not reach the database")

	rec = post(&mockPool{execRows: []int64{1}}, "9999-12-31T23:59:59Z")
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
