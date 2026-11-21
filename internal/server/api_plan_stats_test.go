package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestPLI001: /plans/stats needs a valid instance_id and queryid and an
// ordered window.
func TestPLI001(t *testing.T) {
	id := uuid.NewString()
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"instance_id=" + id, http.StatusBadRequest},
		{"instance_id=" + id + "&queryid=x", http.StatusBadRequest},
		{"queryid=7", http.StatusBadRequest},
		{"instance_id=" + id + "&queryid=7&from=2026-11-02T00:00:00Z&to=2026-11-01T00:00:00Z", http.StatusBadRequest},
		{"instance_id=" + id + "&queryid=7&to=yesterday", http.StatusBadRequest},
		{"instance_id=" + id + "&queryid=7", http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		plansRouter(&API{pool: &mockPool{}}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans/stats?"+tc.query, http.NoBody))
		require.Equal(t, tc.code, rec.Code, "%s: %s", tc.query, rec.Body.String())
	}
}

// TestPLI002: rows map to plans and changes; p95_ms is passed through.
func TestPLI002(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	p := &mockPool{queryResults: []*mockRows{
		{rows: [][]any{
			{"hB", int64(20), 100.0, 99.5, 90.0, 110.0, now.Add(-time.Hour), now, int64(3), true},
			{"hA", int64(20), 10.0, 12.25, 9.0, 13.0, now.Add(-2 * time.Hour), now.Add(-time.Hour), int64(0), false},
		}},
		{rows: [][]any{{now.Add(-time.Hour), "hA", "hB"}}},
	}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans/stats?instance_id="+uuid.NewString()+"&queryid=7&database=app", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out planStatsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, []planStatJSON{
		{PlanHash: "hB", Samples: 20, AvgMS: 100, P95MS: 99.5, MinMS: 90, MaxMS: 110, FirstTS: now.Add(-time.Hour), LastTS: now, AnalyzedSamples: 3, HasPlan: true},
		{PlanHash: "hA", Samples: 20, AvgMS: 10, P95MS: 12.25, MinMS: 9, MaxMS: 13, FirstTS: now.Add(-2 * time.Hour), LastTS: now.Add(-time.Hour)},
	}, out.Plans)
	require.Equal(t, []planChangeJSON{{TS: now.Add(-time.Hour), FromPlanHash: "hA", ToPlanHash: "hB"}}, out.Changes)

	var raw map[string][]map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Len(t, raw["plans"][0], 10)
	require.Len(t, raw["changes"][0], 3)
}
