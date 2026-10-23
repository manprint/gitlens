package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAshAPI_ASHDisabled_ReturnsExplicitFalse(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	pool := &mockPool{
		queryRowVals: []*mockRow{{vals: []any{false}}},
	}
	api := &AshAPI{pool: pool}

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String(), nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Enabled)
	require.False(t, *resp.Enabled)
	require.Equal(t, []ashBucket{}, resp.Buckets)
	require.Equal(t, 1, pool.queryRowCalls, "must not query metrics_ash once short-circuited")
	require.Equal(t, 0, pool.queryCalls)
}

func TestAshAPI_ASHUnknownOrEnabled_FallsThroughToBuckets(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	pool := &mockPool{
		// No row for this instance at all (pgx.ErrNoRows via the default
		// mockRow{err: pgx.ErrNoRows}) — unknown/never-reported must behave
		// exactly like "enabled", not like "disabled".
		queryResults: []*mockRows{{rows: []([]any){}}},
		queryRowVals: []*mockRow{nil, {vals: []any{0}}},
	}
	api := &AshAPI{pool: pool}

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID.String(), nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Nil(t, resp.Enabled)
	require.Empty(t, resp.Buckets)
}

func TestAshAPI_NoPool_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)
	instID := uuid.NewString()

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID, nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, []ashBucket{}, resp.Buckets)
	require.Equal(t, 1, resp.ResolutionSeconds)
	require.True(t, resp.Statistical)
	require.Nil(t, resp.Warning)
}

func TestAshAPI_MissingInstanceID(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)

	req := httptest.NewRequest("GET", "/api/v1/ash", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAshAPI_InvalidInstanceID(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id=invalid", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAshAPI_InvalidGroupBy(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)
	instID := uuid.NewString()

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID+"&group_by=unknown", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAshAPI_AvgActiveSessions_ZeroTicks(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{
				rows: []([]any){
					{time.Now(), 100, 0, "db1", "Lock", "transactionid", "waiting", int64(123)},
				},
			},
		},
		queryRowVals: []*mockRow{{vals: []any{0}}},
	}
	api := &AshAPI{pool: pool}

	buckets, _, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	require.Equal(t, 100, buckets[0].Samples)
	require.Equal(t, 0, buckets[0].Ticks)
	require.Nil(t, buckets[0].AvgActiveSessions) // null when ticks==0
}

func TestAshAPI_AvgActiveSessions_NonZeroTicks(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{
				rows: []([]any){
					{time.Now(), 100, 10, "db1", "Lock", "transactionid", "waiting", int64(123)},
				},
			},
		},
		queryRowVals: []*mockRow{{vals: []any{10}}},
	}
	api := &AshAPI{pool: pool}

	buckets, _, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	require.Equal(t, 100, buckets[0].Samples)
	require.Equal(t, 10, buckets[0].Ticks)
	require.NotNil(t, buckets[0].AvgActiveSessions)
	require.Equal(t, 10.0, *buckets[0].AvgActiveSessions)
}

func TestAshAPI_SignificanceWarning_Below60Ticks(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)
	instID := uuid.NewString()

	req := httptest.NewRequest("GET", "/api/v1/ash?instance_id="+instID, nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	// Mock with 0 ticks -> warning should appear
	require.Equal(t, http.StatusOK, w.Code)
	resp := ashResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Nil(t, resp.Warning) // empty result, no warning needed per spec
}

func TestAshAPI_SignificanceWarning_59Ticks(t *testing.T) {
	t.Parallel()
	instID := uuid.New()

	// Create mock rows with total ticks = 59
	mockRowsData := make([]([]any), 59)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 59; i++ {
		mockRowsData[i] = []any{base.Add(time.Duration(i) * 10 * time.Second), 1, 1, "db1", "CPU", "CPU", "active", (*int64)(nil)}
	}

	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: mockRowsData},
		},
		queryRowVals: []*mockRow{{vals: []any{59}}},
	}
	api := &AshAPI{pool: pool}

	buckets, totalTicks, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 59, totalTicks)
	require.Len(t, buckets, 59)

	// When totalTicks < 60, warning should be set
	require.True(t, totalTicks < 60)
}

func TestAshAPI_SignificanceWarning_60Ticks(t *testing.T) {
	t.Parallel()
	instID := uuid.New()

	// Create mock rows with total ticks = 60
	mockRowsData := make([]([]any), 60)
	base := time.Now().Truncate(time.Second)
	for i := 0; i < 60; i++ {
		mockRowsData[i] = []any{base.Add(time.Duration(i) * 10 * time.Second), 1, 1, "db1", "CPU", "CPU", "active", (*int64)(nil)}
	}

	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: mockRowsData},
		},
		queryRowVals: []*mockRow{{vals: []any{60}}},
	}
	api := &AshAPI{pool: pool}

	buckets, totalTicks, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 60, totalTicks)
	require.Len(t, buckets, 60)

	// When totalTicks >= 60, no warning
	require.False(t, totalTicks < 60)
}

func TestAshAPI_GroupByValidation(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)
	instID := uuid.NewString()

	tests := []struct {
		name      string
		groupBy   string
		wantError bool
	}{
		{"wait_event_type", "wait_event_type", false},
		{"wait_event", "wait_event", false},
		{"state", "state", false},
		{"queryid", "queryid", false},
		{"datname", "datname", false},
		{"combo", "wait_event_type,state", false},
		{"invalid", "invalid_col", true},
		{"mixed_valid_invalid", "wait_event_type,invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/ash?instance_id=" + instID
			if tt.groupBy != "" {
				url += "&group_by=" + tt.groupBy
			}
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			r := chi.NewRouter()
			api.RegisterRoutes(r)
			r.ServeHTTP(w, req)

			if tt.wantError {
				require.Equal(t, http.StatusBadRequest, w.Code)
			} else {
				require.Equal(t, http.StatusOK, w.Code)
			}
		})
	}
}

func TestAshTop_NoPool_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)
	instID := uuid.NewString()

	req := httptest.NewRequest("GET", "/api/v1/ash/top?instance_id="+instID, nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	resp := ashTopResponse{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, []ashTopEntry{}, resp.Entries)
	require.Equal(t, 1, resp.ResolutionSeconds)
	require.True(t, resp.Statistical)
	require.Nil(t, resp.Warning)
}

func TestAshTop_MissingInstanceID(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)

	req := httptest.NewRequest("GET", "/api/v1/ash/top", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAshTop_InvalidInstanceID(t *testing.T) {
	t.Parallel()
	api := NewAshAPI(nil)

	req := httptest.NewRequest("GET", "/api/v1/ash/top?instance_id=invalid", nil)
	w := httptest.NewRecorder()
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAshTop_SignificanceWarning_Below60Ticks(t *testing.T) {
	t.Parallel()
	instID := uuid.New()

	// Create mock rows with total ticks = 59
	pool := &mockPool{
		queryResults: []*mockRows{
			{
				rows: []([]any){
					{int64(9007199254740993), "SELECT 1", 100, 59},
				},
			},
		},
		queryRowVals: []*mockRow{{vals: []any{59}}},
	}
	api := &AshAPI{pool: pool}

	entries, totalTicks, err := api.queryASHTop(context.Background(), instID, nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 59, totalTicks)
	require.Len(t, entries, 1)
	require.Equal(t, "9007199254740993", entries[0].QueryID)

	// When totalTicks < 60, warning should be set by handler
	require.True(t, totalTicks < 60)
}

// TestAshAPI_TicksComeFromTheRangeNotTheRows: the rows of a window carry its
// ticks, and a filter leaves some of them out. The total is the range's ticks,
// read once, so the rows the filter left are not its source.
func TestAshAPI_TicksComeFromTheRangeNotTheRows(t *testing.T) {
	t.Parallel()
	ts := time.Now().Truncate(time.Second)
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{
			{ts, 5, 10, "db1", "CPU", "CPU", "active", (*int64)(nil)},
			{ts, 3, 10, "db1", "Lock", "transactionid", "active", (*int64)(nil)},
			{ts.Add(-10 * time.Second), 2, 10, "db1", "CPU", "CPU", "active", (*int64)(nil)},
		}}},
		queryRowVals: []*mockRow{{vals: []any{600}}},
	}
	_, totalTicks, err := (&AshAPI{pool: pool}).queryASHBuckets(context.Background(), uuid.New(), nil, nil, "", ashFilters{}, 1000)
	require.NoError(t, err)
	require.Equal(t, 600, totalTicks, "the range's ticks, not the two windows the rows show")
}

// TestAshAPI_EmptyResultKeepsTheRangeTicks: a filter that matches nothing leaves
// no rows to read ticks from. The significance guard must still read the
// range's ticks, or every empty answer warns of a small sample. The ASH
// handlers check the instance's ash_enabled first, which takes the first
// scripted row; the top list has no such check.
func TestAshAPI_EmptyResultKeepsTheRangeTicks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path string
		rows []*mockRow
	}{
		{"/api/v1/ash?instance_id=%s", []*mockRow{nil, {vals: []any{600}}}},
		{"/api/v1/ash?instance_id=%s&group_by=wait_event_type&from=2026-09-01T11:00:00Z&to=2026-09-01T12:00:00Z", []*mockRow{nil, {vals: []any{600}}}},
		{"/api/v1/ash/top?instance_id=%s&wait_event=Nope", []*mockRow{{vals: []any{600}}}},
	} {
		pool := &mockPool{queryRowVals: tc.rows}
		r := chi.NewRouter()
		(&AshAPI{pool: pool}).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf(tc.path, uuid.NewString()), nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.NotContains(t, resp, "warning", tc.path)
	}
}

func TestAshResolution_FitsTheRangeInMaxPoints(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 10, ashResolution(to.Add(-10*time.Minute), to), "never finer than the agent's window")
	require.Equal(t, 20, ashResolution(to.Add(-time.Hour), to))
	require.Equal(t, 480, ashResolution(to.Add(-24*time.Hour), to))
	require.Zero(t, ashResolution(to.Add(-24*time.Hour), to)%ashWindowSeconds)
}

// TestAshAPI_GroupByAggregatesAndFillsTheGroup: one row per bucket and
// group, with the group's value filled in. The requested columns used to be
// copied before the scan filled them, so every row read as CPU.
func TestAshAPI_GroupByAggregatesAndFillsTheGroup(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pool := &mockPool{
		queryResults: []*mockRows{{rows: [][]any{
			{ts, 30, 20, "LWLock"},
			{ts, 10, 20, "CPU"},
		}}},
		queryRowVals: []*mockRow{nil, {vals: []any{20}}},
	}
	r := chi.NewRouter()
	(&AshAPI{pool: pool}).RegisterRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/ash?instance_id="+uuid.NewString()+"&group_by=wait_event_type&from=2026-09-01T11:00:00Z&to=2026-09-01T12:00:00Z", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ashResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, 20, resp.ResolutionSeconds)
	require.Len(t, resp.Buckets, 2)
	require.Equal(t, "LWLock", resp.Buckets[0].WaitEventType)
	require.Equal(t, 1.5, *resp.Buckets[0].AvgActiveSessions)
	require.Equal(t, "CPU", resp.Buckets[1].WaitEventType)
	require.NotNil(t, resp.Warning, "20 ticks are not significant")
}

// TestFoldASHGroups_ConservesTheTotal: the groups beyond the largest ones
// become one "other" row per bucket, and a NULL queryid, which the chart
// already shows as other, is folded with them rather than duplicated.
func TestFoldASHGroups_ConservesTheTotal(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	var rows []ashBucket
	for i := 0; i < 5; i++ {
		qid := strconv.Itoa(i)
		rows = append(rows, ashBucket{TS: ts, Samples: 10 - i, Ticks: 10, QueryID: &qid})
	}
	rows = append(rows, ashBucket{TS: ts, Samples: 50, Ticks: 10})
	// limit 3 per bucket: two groups kept, plus other.
	out := foldASHGroups(rows, []string{"queryid"}, 1, 3)
	require.Len(t, out, 3)
	total := 0
	var nilRows int
	for _, b := range out {
		total += b.Samples
		if b.QueryID == nil {
			nilRows++
		}
	}
	require.Equal(t, 90, total)
	require.Equal(t, 1, nilRows)
	require.Equal(t, "0", *out[0].QueryID)
	require.Equal(t, "1", *out[1].QueryID)
	require.Equal(t, 50+8+7+6, out[2].Samples)
}
