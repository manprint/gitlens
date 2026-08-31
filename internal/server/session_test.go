package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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
	defer response.Body.Close()
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
	defer response.Body.Close()
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
	defer response.Body.Close()
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
	defer response.Body.Close()
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
			defer response.Body.Close()
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
	defer response.Body.Close()
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
	defer response.Body.Close()
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestRequireCredential_NoPasswordConfiguredIs503(t *testing.T) {
	server := newSessionTestServer(t, UIConfig{Enabled: true}, NewSessionStore(time.Hour))
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/clusters")
	require.NoError(t, err)
	defer response.Body.Close()
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
	defer response.Body.Close()
	require.NotEqual(t, http.StatusUnauthorized, response.StatusCode)
}

func newSessionTestServer(t *testing.T, cfg UIConfig, store *SessionStore) *httptest.Server {
	t.Helper()
	router := NewRouter(cfg, store, NewAuth("agent-token"), NewInventory(nil), NewPipeline(nil, nil), NewAPI(nil), NewTopologyAPI(nil), NewAshAPI(nil))
	return httptest.NewServer(router)
}
