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

var readinessTestID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// scriptReadiness queues, in the order readinessInput issues them, the rows of
// one instance. settings are key/value/source triples; failing names failing
// checks.
func scriptReadiness(m *mockPool, settings [][]any, failing []string) {
	flags := []byte(`{"commands_enabled":true,"allow_signal":false,"by_application":true,"host_local":false}`)
	m.queryRowVals = append(m.queryRowVals,
		&mockRow{vals: []any{170002, "primary", "T1"}},  // instance
		&mockRow{vals: []any{"pglens", "true", "true"}}, // monitoring_role
		&mockRow{vals: []any{true, true}},               // host series
		&mockRow{vals: []any{flags}},                    // agent_flags
		&mockRow{vals: []any{3, 0}},                     // databases
	)
	failRows := make([][]any, 0, len(failing))
	for _, f := range failing {
		failRows = append(failRows, []any{f})
	}
	m.queryResults = append(m.queryResults,
		&mockRows{rows: settings}, // settings
		&mockRows{rows: [][]any{{"app", "pg_stat_statements"}, {"app", "pgstattuple"}}}, // extensions
		&mockRows{},               // check_skip
		&mockRows{rows: failRows}, // check_results
	)
}

func readinessRouter(a *API) http.Handler {
	r := chi.NewRouter()
	a.registerReadinessRoutes(r)
	return r
}

func getReadinessJSON(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return rec.Code, body
}

func healthySettings() [][]any {
	return [][]any{
		{"shared_preload_libraries", "pg_stat_statements", "configuration file"},
		{"track_io_timing", "on", "configuration file"},
		{"track_activity_query_size", "4096", "configuration file"},
	}
}

// RDY-API-001: the report shape, 200 for a known instance.
func TestRDYAPI001_ReadinessShape(t *testing.T) {
	pool := &mockPool{}
	scriptReadiness(pool, healthySettings(), []string{"locks"})
	a := &API{pool: pool}
	code, body := getReadinessJSON(t, readinessRouter(a), "/api/v1/instances/"+readinessTestID.String()+"/readiness")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, readinessTestID.String(), body["instance_id"])
	require.Equal(t, "partial", body["level"], "a failing check and no notification channel are warnings")
	require.NotEmpty(t, body["generated_at"])
	counts := body["counts"].(map[string]any)
	require.EqualValues(t, 2, counts["warning"])
	items := body["items"].([]any)
	var ids []string
	for _, raw := range items {
		it := raw.(map[string]any)
		ids = append(ids, it["id"].(string))
		for _, key := range []string{"id", "severity", "title", "summary", "impact", "evidence", "fixes"} {
			require.Contains(t, it, key)
		}
	}
	require.Contains(t, ids, "RDY-CHK-01")
	require.Contains(t, ids, "RDY-NOTIF-01")
	require.Contains(t, ids, "RDY-T2-01", "tier T1 is below T2")
	require.Contains(t, ids, "RDY-SIG-01", "allow_signal false")
	require.NotContains(t, ids, "RDY-HOST-01", "host_local false")
	require.NotContains(t, ids, "RDY-EXT-01")
}

// RDY-API-001: unknown instance 404, invalid id 400, no pool or failing store 503.
func TestRDYAPI001_ReadinessErrors(t *testing.T) {
	path := "/api/v1/instances/" + readinessTestID.String() + "/readiness"

	code, _ := getReadinessJSON(t, readinessRouter(&API{pool: &mockPool{}}), path)
	require.Equal(t, http.StatusNotFound, code, "no instance row")

	code, _ = getReadinessJSON(t, readinessRouter(&API{pool: &mockPool{}}), "/api/v1/instances/nope/readiness")
	require.Equal(t, http.StatusBadRequest, code)

	code, _ = getReadinessJSON(t, readinessRouter(&API{}), path)
	require.Equal(t, http.StatusServiceUnavailable, code)

	pool := &mockPool{queryRowVals: []*mockRow{{err: errors.New("connection refused")}}}
	code, body := getReadinessJSON(t, readinessRouter(&API{pool: pool}), path)
	require.Equal(t, http.StatusServiceUnavailable, code)
	require.NotContains(t, body["detail"], "password")

	pool = &mockPool{}
	scriptReadiness(pool, healthySettings(), nil)
	pool.queryErrs = []error{errors.New("boom")}
	code, _ = getReadinessJSON(t, readinessRouter(&API{pool: pool}), path)
	require.Equal(t, http.StatusServiceUnavailable, code, "a failing fact query is not a 500 with half a report")
}

// RDY-API-002: a report is served from memory for 30 s and recomputed after.
func TestRDYAPI002_Cache(t *testing.T) {
	pool := &mockPool{}
	scriptReadiness(pool, healthySettings(), nil)
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	a := &API{pool: pool}
	a.readinessCache.now = func() time.Time { return now }
	h := readinessRouter(a)
	path := "/api/v1/instances/" + readinessTestID.String() + "/readiness"

	code, first := getReadinessJSON(t, h, path)
	require.Equal(t, http.StatusOK, code)
	calls := pool.queryRowCalls + pool.queryCalls
	require.Positive(t, calls)

	now = now.Add(29 * time.Second)
	_, second := getReadinessJSON(t, h, path)
	require.Equal(t, calls, pool.queryRowCalls+pool.queryCalls, "served from the cache")
	require.Equal(t, first, second)

	now = now.Add(2 * time.Second)
	getReadinessJSON(t, h, path)
	require.Greater(t, pool.queryRowCalls+pool.queryCalls, calls, "recomputed after the TTL")
}

// RDY-API-003: the summary lists each instance with its counts and totals.
func TestRDYAPI003_Summary(t *testing.T) {
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	pool := &mockPool{}
	pool.queryResults = append(pool.queryResults, &mockRows{rows: [][]any{{readinessTestID.String()}, {other.String()}}})
	// Per instance, queries are issued in the same order, so script the shared
	// queue twice: the list query is Query #0, the rest follow.
	scriptReadiness(pool, healthySettings(), nil)
	scriptReadiness(pool, healthySettings(), []string{"locks"})
	// scriptReadiness appends its Query results after the list; the Query
	// order is list, then 4 for the first instance, then 4 for the second.
	a := &API{pool: pool}
	code, body := getReadinessJSON(t, readinessRouter(a), "/api/v1/readiness/summary")
	require.Equal(t, http.StatusOK, code)
	instances := body["instances"].([]any)
	require.Len(t, instances, 2)
	first := instances[0].(map[string]any)
	require.Equal(t, readinessTestID.String(), first["instance_id"])
	for _, key := range []string{"instance_id", "level", "warnings", "infos"} {
		require.Contains(t, first, key)
	}
	totals := body["totals"].(map[string]any)
	require.EqualValues(t, 2, totals["full"].(float64)+totals["partial"].(float64))

	code, body = getReadinessJSON(t, readinessRouter(&API{}), "/api/v1/readiness/summary")
	require.Equal(t, http.StatusServiceUnavailable, code)
	_ = body

	empty := &mockPool{}
	code, body = getReadinessJSON(t, readinessRouter(&API{pool: empty}), "/api/v1/readiness/summary")
	require.Equal(t, http.StatusOK, code)
	require.Empty(t, body["instances"], "no instance: an empty list, not null")
	require.NotNil(t, body["instances"])
}
