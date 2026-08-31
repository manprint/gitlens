package server

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestSessionStore_CreateAndValidate(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }

	token, expiresAt, err := store.Create()
	require.NoError(t, err)
	require.Len(t, token, 43)
	require.NotContains(t, token, "=")
	require.Equal(t, now.Add(time.Hour), expiresAt)
	require.Equal(t, 1, store.Len())

	gotExpiry, ok := store.Validate(token)
	require.True(t, ok)
	require.Equal(t, expiresAt, gotExpiry)
}

func TestSessionStore_Expiry(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }
	token, _, err := store.Create()
	require.NoError(t, err)

	now = now.Add(time.Hour)
	_, ok := store.Validate(token)
	require.False(t, ok)
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	store.Delete(token)
	_, ok := store.Validate(token)
	require.False(t, ok)
	require.Equal(t, 0, store.Len())
}

func TestSessionStore_UnknownToken(t *testing.T) {
	store := NewSessionStore(time.Hour)
	_, ok := store.Validate("")
	require.False(t, ok)
	_, ok = store.Validate("not-a-session")
	require.False(t, ok)
}

func TestSessionStore_TokensAreUnique(t *testing.T) {
	store := NewSessionStore(time.Hour)
	tokens := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		token, _, err := store.Create()
		require.NoError(t, err)
		require.Len(t, token, 43)
		_, duplicate := tokens[token]
		require.False(t, duplicate)
		tokens[token] = struct{}{}
	}
	require.Len(t, tokens, 1000)
}

func TestSessionStore_ReapsExpired(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }

	for i := 0; i < 1100; i++ {
		_, _, err := store.Create()
		require.NoError(t, err)
		if i == 0 {
			now = now.Add(time.Hour)
		}
	}
	require.Less(t, store.Len(), 1100)

	now = now.Add(time.Hour)
	store.reap()
	require.Equal(t, 0, store.Len())
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := NewSessionStore(time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				token, _, err := store.Create()
				if err != nil {
					t.Errorf("Create() error = %v", err)
					return
				}
				store.Validate(token)
				store.Delete(token)
			}
		}()
	}
	wg.Wait()
}

func TestSessionStorePropagatesRandomnessFailure(t *testing.T) {
	original := sessionRandRead
	t.Cleanup(func() { sessionRandRead = original })
	wantErr := errors.New("random source unavailable")
	sessionRandRead = func([]byte) (int, error) { return 0, wantErr }

	store := NewSessionStore(time.Hour)
	token, expiresAt, err := store.Create()
	require.ErrorIs(t, err, wantErr)
	require.Empty(t, token)
	require.True(t, expiresAt.IsZero())
	require.Equal(t, 0, store.Len())
}

func TestRequireCredential_AnonymousApiIs401(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, NewSessionStore(time.Hour))
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/clusters")
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.Empty(t, response.Header.Get("WWW-Authenticate"))
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"error":"unauthorized"`)
}

func TestRequireCredential_BearerTokenStillWorks(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, NewSessionStore(time.Hour))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/clusters", http.NoBody)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer agent-token")
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.NotEqual(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_SessionCookieWorks(t *testing.T) {
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, store)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/clusters", http.NoBody)
	require.NoError(t, err)
	request.AddCookie(&http.Cookie{Name: "pglens_session", Value: token})
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.NotEqual(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_ExpiredCookieIs401(t *testing.T) {
	now := time.Now()
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }
	token, _, err := store.Create()
	require.NoError(t, err)
	now = now.Add(time.Hour)
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, store)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/clusters", http.NoBody)
	require.NoError(t, err)
	request.AddCookie(&http.Cookie{Name: "pglens_session", Value: token})
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_ExemptPaths(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, NewSessionStore(time.Hour))
	defer server.Close()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz"},
		{name: "readyz", method: http.MethodGet, path: "/readyz"},
		{name: "metrics", method: http.MethodGet, path: "/metrics"},
		{name: "create session", method: http.MethodPost, path: "/api/v1/session"},
		{name: "get session", method: http.MethodGet, path: "/api/v1/session"},
		{name: "root asset", method: http.MethodGet, path: "/"},
		{name: "static asset", method: http.MethodGet, path: "/assets/app.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, err := http.NewRequest(tt.method, server.URL+tt.path, http.NoBody)
			require.NoError(t, err)
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer func() { require.NoError(t, response.Body.Close()) }()
			if tt.name == "get session" {
				require.Equal(t, http.StatusUnauthorized, response.StatusCode)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"configured":true`)
				return
			}
			require.NotEqual(t, http.StatusUnauthorized, response.StatusCode)
		})
	}
}

func TestRequireCredential_DeleteSessionIsNotExempt(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, NewSessionStore(time.Hour))
	defer server.Close()

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/v1/session", http.NoBody)
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_PushStillUsesBearerOnly(t *testing.T) {
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	server := newSessionTestServer(t, UIConfig{Enabled: true, Password: "password"}, store)
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/push", http.NoBody)
	require.NoError(t, err)
	request.AddCookie(&http.Cookie{Name: "pglens_session", Value: token})
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_NoPasswordConfiguredIs503(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true}, NewSessionStore(time.Hour))
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/clusters")
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), `"error":"ui_password_not_configured"`)
}

func TestRequireCredential_DisabledLeavesRoutesOpen(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Password: "password"}, NewSessionStore(time.Hour))
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/clusters")
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	require.NotEqual(t, http.StatusUnauthorized, response.StatusCode)
}

func TestSessionLogin_Success(t *testing.T) {
	cfg := UIConfig{Enabled: true, Password: "correct", SessionTTL: time.Hour, CookieSecure: "false"}
	store := NewSessionStore(time.Hour)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":"correct"}`))

	newSessionRoutes(cfg, store).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Empty(t, recorder.Body.String())
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	require.Equal(t, sessionCookieName, cookie.Name)
	require.NotEmpty(t, cookie.Value)
	require.Equal(t, "/", cookie.Path)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, 3600, cookie.MaxAge)
	_, ok := store.Validate(cookie.Value)
	require.True(t, ok)
}

func TestSessionLogin_WrongPassword(t *testing.T) {
	cfg := UIConfig{Enabled: true, Password: "correct", SessionTTL: time.Hour}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":"wrong"}`))
	started := time.Now()

	newSessionRoutes(cfg, NewSessionStore(time.Hour)).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.GreaterOrEqual(t, time.Since(started), sessionLoginFailureWait)
	require.Empty(t, recorder.Header().Values("Set-Cookie"))
}

func TestSessionLogin_BodyTooLarge(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(strings.Repeat("x", 1<<20)))

	newSessionRoutes(UIConfig{Password: "correct"}, NewSessionStore(time.Hour)).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestSessionLogin_MalformedJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"password":`))

	newSessionRoutes(UIConfig{Password: "correct"}, NewSessionStore(time.Hour)).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"error":"invalid JSON"`)
}

func TestSessionGet_States(t *testing.T) {
	tests := []struct {
		name          string
		password      string
		authenticated bool
		configured    bool
		wantStatus    int
	}{
		{name: "authenticated", password: "correct", authenticated: true, configured: true, wantStatus: http.StatusOK},
		{name: "unauthenticated", password: "correct", configured: true, wantStatus: http.StatusUnauthorized},
		{name: "not configured", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := UIConfig{Password: tt.password, SessionTTL: time.Hour}
			store := NewSessionStore(time.Hour)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
			if tt.authenticated {
				token, _, err := store.Create()
				require.NoError(t, err)
				request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
			}

			newSessionRoutes(cfg, store).ServeHTTP(recorder, request)

			require.Equal(t, tt.wantStatus, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			var status struct {
				Authenticated bool   `json:"authenticated"`
				Configured    bool   `json:"configured"`
				ExpiresAt     string `json:"expires_at"`
			}
			require.NoError(t, json.NewDecoder(recorder.Body).Decode(&status))
			require.Equal(t, tt.authenticated, status.Authenticated)
			if !tt.authenticated {
				require.Equal(t, tt.configured, status.Configured)
			}
			if tt.authenticated {
				require.NotEmpty(t, status.ExpiresAt)
			}
		})
	}
}

func TestSessionDelete_ClearsCookie(t *testing.T) {
	cfg := UIConfig{Password: "correct", SessionTTL: time.Hour, CookieSecure: "false"}
	store := NewSessionStore(time.Hour)
	token, _, err := store.Create()
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})

	newSessionRoutes(cfg, store).ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Contains(t, recorder.Header().Get("Set-Cookie"), "Max-Age=0")
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, -1, cookies[0].MaxAge)
	_, ok := store.Validate(token)
	require.False(t, ok)
}

func TestSecureCookie_Resolution(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		withTLS  bool
		forward  string
		expected bool
	}{
		{name: "auto TLS", setting: "auto", withTLS: true, expected: true},
		{name: "auto forwarded HTTPS", setting: "auto", forward: "https", expected: true},
		{name: "auto plain", setting: "auto", expected: false},
		{name: "forced true", setting: "true", expected: true},
		{name: "forced false", setting: "false", withTLS: true, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session", nil)
			if tt.withTLS {
				request.TLS = &tls.ConnectionState{}
			}
			if tt.forward != "" {
				request.Header.Set("X-Forwarded-Proto", tt.forward)
			}
			require.Equal(t, tt.expected, secureCookie(UIConfig{CookieSecure: tt.setting}, request))
		})
	}
}

func newSessionRoutes(cfg UIConfig, store *SessionStore) http.Handler {
	router := chi.NewRouter()
	RegisterSessionRoutes(router, cfg, store)
	return router
}

func newSessionTestServer(t *testing.T, cfg UIConfig, store *SessionStore) *httptest.Server {
	t.Helper()
	router := NewRouter(cfg, store, NewAuth("agent-token"), NewInventory(nil), NewPipeline(nil, nil), NewAPI(nil), NewTopologyAPI(nil), NewAshAPI(nil))
	return httptest.NewServer(router)
}
