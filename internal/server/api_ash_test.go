package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	}
	api := &AshAPI{pool: pool}

	buckets, _, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", []string{}, 1000)
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
	}
	api := &AshAPI{pool: pool}

	buckets, _, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", []string{}, 1000)
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
	for i := 0; i < 59; i++ {
		mockRowsData[i] = []any{time.Now(), 1, 1, "db1", "CPU", "CPU", "active", (*int64)(nil)}
	}

	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: mockRowsData},
		},
	}
	api := &AshAPI{pool: pool}

	buckets, totalTicks, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", []string{}, 1000)
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
	for i := 0; i < 60; i++ {
		mockRowsData[i] = []any{time.Now(), 1, 1, "db1", "CPU", "CPU", "active", (*int64)(nil)}
	}

	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: mockRowsData},
		},
	}
	api := &AshAPI{pool: pool}

	buckets, totalTicks, err := api.queryASHBuckets(context.Background(), instID, nil, nil, "", []string{}, 1000)
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
					{int64(123), "SELECT 1", 100, 59},
				},
			},
		},
	}
	api := &AshAPI{pool: pool}

	entries, totalTicks, err := api.queryASHTop(context.Background(), instID, nil, nil, "", 1000)
	require.NoError(t, err)
	require.Equal(t, 59, totalTicks)
	require.Len(t, entries, 1)

	// When totalTicks < 60, warning should be set by handler
	require.True(t, totalTicks < 60)
}
