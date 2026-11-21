package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const avhInstance = "11111111-1111-1111-1111-111111111111"

func avhGet(t *testing.T, api *API, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	plansRouter(api).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

// AVH-009: both endpoints validate instance_id, the window and the limit;
// runs also validates kind and order_by, and an unknown order_by is a 400.
func TestAVH009_ParameterChecks(t *testing.T) {
	api := &API{}
	bad := []string{
		"/api/v1/autovacuum/runs",
		"/api/v1/autovacuum/runs?instance_id=nope",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&order_by=relname",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&order_by=ts%3Bdrop",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&kind=full",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&limit=0",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&limit=1001",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&from=2026-11-02T00:00:00Z&to=2026-11-01T00:00:00Z",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&from=yesterday",
		"/api/v1/autovacuum/summary",
		"/api/v1/autovacuum/summary?instance_id=" + avhInstance + "&limit=abc",
		"/api/v1/autovacuum/summary?instance_id=" + avhInstance + "&to=never",
	}
	for _, u := range bad {
		require.Equal(t, http.StatusBadRequest, avhGet(t, api, u).Code, u)
	}
	good := []string{
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance,
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&order_by=elapsed_seconds&kind=vacuum&limit=1000",
		"/api/v1/autovacuum/runs?instance_id=" + avhInstance + "&order_by=tuples_removed&database=db&schema=public&relation=t",
		"/api/v1/autovacuum/summary?instance_id=" + avhInstance + "&limit=1",
	}
	for _, u := range good {
		rec := avhGet(t, api, u)
		require.Equal(t, http.StatusOK, rec.Code, u)
		require.Regexp(t, `^\{"(runs|tables)":\[\]\}`, rec.Body.String(), "no pool answers an empty list")
	}
}

// AVH-010: the summary maps each aggregated row in the order the query
// returns it (total elapsed first) and the runs keep absent numbers null.
func TestAVH010_SummaryAndRunsMapping(t *testing.T) {
	last := time.Date(2026, 11, 3, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{"db", "public", "big", int64(3), 12.5, 10.0, int64(900), last, int64(1)},
		{"db", "public", "small", int64(5), 1.5, 0.5, int64(0), last, int64(0)},
	}}}}
	rec := avhGet(t, &API{pool: p}, "/api/v1/autovacuum/summary?instance_id="+avhInstance)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var s struct {
		Tables []autovacuumSummaryJSON `json:"tables"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
	require.Len(t, s.Tables, 2)
	require.Equal(t, "big", s.Tables[0].Relname)
	require.Equal(t, autovacuumSummaryJSON{Datname: "db", Schemaname: "public", Relname: "big", Runs: 3,
		TotalElapsedSeconds: 12.5, MaxElapsedSeconds: 10, TuplesRemoved: 900, LastTS: last.UTC(), WraparoundRuns: 1}, s.Tables[0])
	require.Contains(t, rec.Body.String(), `"last_ts":"2026-11-03T09:00:00Z"`)

	elapsed := 2.25
	p = &mockPool{queryResults: []*mockRows{{rows: [][]any{{last, "db", "public", "big", "vacuum", true, true,
		int64(1), nil, nil, int64(40), int64(900), nil, int64(7), nil, nil, nil, nil, nil, int64(4096), nil, nil, elapsed}}}}}
	rec = avhGet(t, &API{pool: p}, "/api/v1/autovacuum/runs?instance_id="+avhInstance+"&order_by=elapsed_seconds")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var r struct {
		Runs []map[string]any `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &r))
	require.Len(t, r.Runs, 1)
	run := r.Runs[0]
	require.Equal(t, "2026-11-03T09:00:00Z", run["ts"])
	require.Equal(t, true, run["wraparound"])
	require.Equal(t, 900.0, run["tuples_removed"])
	require.Equal(t, 7.0, run["tuples_dead_not_removable"])
	require.Equal(t, 4096.0, run["wal_bytes"])
	require.Equal(t, 2.25, run["elapsed_seconds"])
	require.Contains(t, run, "pages_removed")
	require.Nil(t, run["pages_removed"], "a number the log line lacked stays null")
	require.Len(t, run, 23, "every column but tenant and cluster")
}
