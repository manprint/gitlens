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

func plansRouter(api *API) http.Handler {
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

func TestPlansAPI_OrdersNewestFirst(t *testing.T) {
	now := time.Now().UTC()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{int64(2), "new", now, false, []byte(`[{"Plan":{"Node Type":"Index Scan"}}]`)},
		{int64(1), "old", now.Add(-time.Minute), false, []byte(`[{"Plan":{"Node Type":"Seq Scan"}}]`)},
	}}}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans?instance_id="+uuid.NewString()+"&queryid=7", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, []string{"new", "old"}, []string{response.Plans[0].PlanHash, response.Plans[1].PlanHash})
}

func TestPlansAPI_MarksChangedAfterFirst(t *testing.T) {
	now := time.Now().UTC()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{int64(1), "first", now, false, []byte(`[]`)},
		{int64(2), "second", now.Add(time.Minute), true, []byte(`[]`)},
	}}}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans?queryid=7", http.NoBody))
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.True(t, response.Plans[0].Changed)
	require.False(t, response.Plans[1].Changed)
}

// A plain and an ANALYZE capture of the same shape are two rows with one hash;
// neither is a change.
func TestPlansAPI_SameShapeCapturedTwiceIsNotChanged(t *testing.T) {
	now := time.Now().UTC()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{int64(2), "shape", now.Add(time.Minute), true, []byte(`[]`)},
		{int64(1), "shape", now, false, []byte(`[]`)},
	}}}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans?queryid=7", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.False(t, response.Plans[0].Changed)
	require.False(t, response.Plans[1].Changed)
}

// total_shapes counts plan hashes: the plain and the ANALYZE capture of one
// shape are two rows.
func TestPlansAPI_TotalShapesCountsHashesOnce(t *testing.T) {
	now := time.Now().UTC()
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{
		{int64(3), "shape", now.Add(2 * time.Minute), true, []byte(`[]`)},
		{int64(2), "other", now.Add(time.Minute), false, []byte(`[]`)},
		{int64(1), "shape", now, false, []byte(`[]`)},
	}}}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans?queryid=7", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Plans, 3)
	require.Equal(t, 2, response.TotalShapes)
}

func TestPlansAPI_TotalShapes(t *testing.T) {
	p := &mockPool{queryResults: []*mockRows{{rows: [][]any{{int64(1), "one", time.Now(), false, []byte(`[]`)}}}}}
	rec := httptest.NewRecorder()
	plansRouter(&API{pool: p}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans?queryid=7", http.NoBody))
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, len(response.Plans), response.TotalShapes)
}

func TestPlansAPI_RequiresQueryID(t *testing.T) {
	rec := httptest.NewRecorder()
	plansRouter(NewAPI(nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plans", http.NoBody))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
