package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
)

func serveAlert(t *testing.T, api *AlertAPI, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	alertRouter(api).ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

type alertAPITestStore struct {
	alerts []alert.Alert
	rules  []alert.Rule
}

func (s *alertAPITestStore) Rules(context.Context) ([]alert.Rule, error) { return s.rules, nil }
func (s *alertAPITestStore) Silences(context.Context, time.Time) ([]alert.Silence, error) {
	return nil, nil
}
func (s *alertAPITestStore) Upsert(context.Context, alert.Alert) error { return nil }
func (s *alertAPITestStore) ClaimNotification(context.Context, string, string, string, alert.Alert) (bool, error) {
	return true, nil
}
func (s *alertAPITestStore) MarkNotification(context.Context, string, string, string, bool, int, error) error {
	return nil
}
func (s *alertAPITestStore) Active(context.Context, alert.Filter) ([]alert.Alert, error) {
	return s.alerts, nil
}

func alertRouter(api *AlertAPI) http.Handler { r := chi.NewRouter(); api.RegisterRoutes(r); return r }

func TestAlerts_DefaultFilterIsPendingAndFiring(t *testing.T) {
	st := &alertAPITestStore{alerts: []alert.Alert{{Key: "a", State: alert.StatePending}, {Key: "b", State: alert.StateFiring}}}
	r := httptest.NewRecorder()
	alertRouter(newAlertAPIForTest(nil, st)).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil))
	require.Equal(t, 200, r.Code)
	var got []alertJSON
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &got))
	require.Len(t, got, 2)
}

func TestAlerts_RejectsUnknownState(t *testing.T) {
	r := httptest.NewRecorder()
	alertRouter(newAlertAPIForTest(nil, &alertAPITestStore{})).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?state=bogus", nil))
	require.Equal(t, 400, r.Code)
}

func TestAlertRules_Tier0IsRejectedWithConflict(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/agent_down", strings.NewReader(`{"enabled":false}`))
	alertRouter(newAlertAPIForTest(nil, &alertAPITestStore{})).ServeHTTP(r, req)
	require.Equal(t, 409, r.Code)
}

func TestAlertRules_InvalidBodyIsBadRequest(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/custom", strings.NewReader(`{"severity":"bad"}`))
	alertRouter(newAlertAPIForTest(nil, &alertAPITestStore{})).ServeHTTP(r, req)
	require.Equal(t, 400, r.Code)
}

func TestSilences_CreateValidates(t *testing.T) {
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/silences", strings.NewReader(`{"matchers":[],"reason":"x"}`))
	alertRouter(newAlertAPIForTest(nil, &alertAPITestStore{})).ServeHTTP(r, req)
	require.Equal(t, 400, r.Code)
}

func TestAlerts_ClusterIDIsAString(t *testing.T) {
	n := int64(42)
	id := uuid.New()
	st := &alertAPITestStore{alerts: []alert.Alert{{Key: "a", State: alert.StateFiring, ClusterID: &n, InstanceID: &id}}}
	r := httptest.NewRecorder()
	alertRouter(newAlertAPIForTest(nil, st)).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?state=firing", nil))
	require.Equal(t, 200, r.Code)
	require.Contains(t, r.Body.String(), `"cluster_id":"42"`)
}

func TestAlertAPI_DatabaseBackedBranches(t *testing.T) {
	store := &alertAPITestStore{rules: []alert.Rule{{ID: "custom", Tier: alert.Tier1, Enabled: true, Severity: alert.SeverityWarning, Scope: alert.ScopeInstance, Metric: "m", Comparator: alert.GT, Summary: "s"}}}
	pool := &mockPool{}
	api := newAlertAPIForTest(pool, store)
	require.Equal(t, 200, serveAlert(t, api, http.MethodGet, "/api/v1/alert-rules", "").Code)
	pool.queryRowVals = []*mockRow{{vals: []any{true, alert.SeverityWarning, 1.0, 10}}}
	pool.execErrs = []error{nil}
	pool.execRows = []int64{1}
	require.Equal(t, 200, serveAlert(t, api, http.MethodPut, "/api/v1/alert-rules/custom", `{"enabled":false,"threshold":2}`).Code)
	pool.queryRowVals = []*mockRow{{err: pgx.ErrNoRows}}
	require.Equal(t, 404, serveAlert(t, api, http.MethodPut, "/api/v1/alert-rules/missing", `{}`).Code)
	silenceID := uuid.New()
	pool.queryResults = []*mockRows{{rows: [][]any{{silenceID[:], []byte(`[{"name":"rule_id","value":"custom"}]`), "maintenance", time.Now().Add(-time.Hour), time.Now().Add(time.Hour)}}}}
	wSilences := serveAlert(t, api, http.MethodGet, "/api/v1/silences", "")
	require.Equal(t, 200, wSilences.Code, wSilences.Body.String())
	require.Equal(t, 200, serveAlert(t, api, http.MethodGet, "/api/v1/silences?all=true", "").Code)
	pool.execErrs = []error{nil}
	pool.execRows = []int64{0, 0, 1}
	w := serveAlert(t, api, http.MethodPost, "/api/v1/silences", `{"matchers":[{"name":"rule_id","value":"custom"}],"reason":"maintenance","starts_at":"2026-08-29T00:00:00Z","ends_at":"2026-08-30T00:00:00Z"}`)
	require.Equal(t, 201, w.Code)
	pool.execErrs = []error{nil}
	pool.execRows = []int64{1, 1, 1, 1, 1, 1}
	require.Equal(t, 204, serveAlert(t, api, http.MethodDelete, "/api/v1/silences/"+uuid.NewString(), "").Code)
	id := uuid.New()
	n := int64(7)
	now := time.Now()
	pool.queryRowVals = make([]*mockRow, pool.queryRowCalls+1)
	pool.queryRowVals[pool.queryRowCalls] = &mockRow{vals: []any{"key", "custom", alert.SeverityWarning, alert.StateFiring, &n, &id, "db", []byte(`{"x":"y"}`), 1.0, "s", now, now, nil}}
	require.Equal(t, 200, serveAlert(t, api, http.MethodGet, "/api/v1/alerts/key", "").Code)
	pool.queryRowVals = make([]*mockRow, pool.queryRowCalls+1)
	pool.queryRowVals[pool.queryRowCalls] = &mockRow{err: pgx.ErrNoRows}
	require.Equal(t, 404, serveAlert(t, api, http.MethodGet, "/api/v1/alerts/missing", "").Code)
}

func TestAlertAPI_UnavailableBranches(t *testing.T) {
	routes := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, "/api/v1/alerts/key", "", http.StatusServiceUnavailable},
		{http.MethodGet, "/api/v1/alert-rules", "", http.StatusServiceUnavailable},
		{http.MethodPut, "/api/v1/alert-rules/custom", `{}`, http.StatusServiceUnavailable},
		{http.MethodGet, "/api/v1/silences", "", http.StatusServiceUnavailable},
		{http.MethodPost, "/api/v1/silences", `{"matchers":[{"name":"rule_id","value":"custom"}],"reason":"x","starts_at":"2026-08-29T00:00:00Z","ends_at":"2026-08-30T00:00:00Z"}`, http.StatusServiceUnavailable},
		{http.MethodDelete, "/api/v1/silences/" + uuid.NewString(), "", http.StatusServiceUnavailable},
	}
	api := newAlertAPIForTest(nil, nil)
	for _, tc := range routes {
		require.Equal(t, tc.status, serveAlert(t, api, tc.method, tc.path, tc.body).Code, tc.path)
	}
}
