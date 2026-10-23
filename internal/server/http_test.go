package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	router := NewRouter(UIConfig{}, nil, nil, nil, nil, nil, api, NewTopologyAPI(nil), NewAshAPI(nil))
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	api.pool = &mockPool{queryRowVals: []*mockRow{{vals: []any{1}}, {vals: []any{false}}}}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// securityRouter is the full router with the UI on and one signed-in session.
// The API has no database, so a write gets past the gates and then answers
// 503, which shows it reached its handler.
func securityRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	router := NewRouter(UIConfig{Enabled: true, Password: "pw"}, testWebUIAssets(), store, nil, nil, nil, &API{}, NewTopologyAPI(nil), NewAshAPI(nil))
	return router, token
}

func TestRouter_SendsFramingAndReferrerHeaders(t *testing.T) {
	router, _ := securityRouter(t)
	for _, target := range []string{"/", "/api/v1/clusters", "/no-such-page"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"), target)
		require.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"), target)
		require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), target)
		require.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'", target)
	}
}

// A write rides on the session cookie, and SameSite=Strict does not stop a
// sibling subdomain. Browsers say where a request came from, so a write from
// another site is refused, while a same-origin write and a tool that sends no
// origin headers reach their handler.
func TestRouter_RefusesCrossSiteWrites(t *testing.T) {
	router, token := securityRouter(t)
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	mute := func(headers map[string]string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/findings/query.slow_mean/mute", strings.NewReader(`{"reason":"x","until":"`+until+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusForbidden, mute(map[string]string{"Sec-Fetch-Site": "cross-site"}))
	require.Equal(t, http.StatusForbidden, mute(map[string]string{"Sec-Fetch-Site": "same-site"}))
	require.Equal(t, http.StatusForbidden, mute(map[string]string{"Origin": "https://evil.example"}))
	require.Equal(t, http.StatusServiceUnavailable, mute(map[string]string{"Sec-Fetch-Site": "same-origin"}))
	require.Equal(t, http.StatusServiceUnavailable, mute(map[string]string{"Origin": "https://example.com"}))
	require.Equal(t, http.StatusServiceUnavailable, mute(nil))
}

// A reverse proxy that rewrites Host reaches the server under another name. A
// browser that sends Sec-Fetch-Site is judged by that header, so its writes
// still pass. Only a request that carries Origin and no Sec-Fetch-Site is
// compared with Host, so that browser needs the proxy to keep Host.
func TestRouter_ProxyHostRewriteOnlyAffectsOriginOnlyWrites(t *testing.T) {
	router, token := securityRouter(t)
	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	mute := func(headers map[string]string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/findings/query.slow_mean/mute", strings.NewReader(`{"reason":"x","until":"`+until+`"}`))
		req.Host = "backend.internal:8080"
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusServiceUnavailable, mute(map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://pglens.example"}))
	require.Equal(t, http.StatusForbidden, mute(map[string]string{"Origin": "https://pglens.example"}))
}
