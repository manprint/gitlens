package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPI_ReadyRequiresDatabaseAndMigrations(t *testing.T) {
	tests := []struct {
		name string
		pool *mockPool
		want string
	}{
		{name: "ready", pool: &mockPool{queryRowVals: []*mockRow{{vals: []any{1}}, {vals: []any{true}}}}},
		{name: "database down", pool: &mockPool{queryRowVals: []*mockRow{{err: errors.New("down")}}}, want: "database ping"},
		{name: "migrations missing", pool: &mockPool{queryRowVals: []*mockRow{{vals: []any{1}}, {vals: []any{false}}}}, want: "not applied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&API{pool: tt.pool}).Ready(context.Background())
			if tt.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.want)
			}
		})
	}
	require.Error(t, (&API{}).Ready(context.Background()))
}

func TestReadyzRouteReflectsReadiness(t *testing.T) {
	api := &API{pool: &mockPool{queryRowVals: []*mockRow{{vals: []any{1}}, {vals: []any{true}}}}}
	router := NewRouter(UIConfig{}, nil, nil, nil, nil, api, NewTopologyAPI(nil), NewAshAPI(nil))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	api.pool = &mockPool{queryRowVals: []*mockRow{{vals: []any{1}}, {vals: []any{false}}}}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
