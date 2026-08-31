package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const testIndexHTML = "<!doctype html><html><body>pglens app</body></html>"

func testWebUIAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte(testIndexHTML)},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	}
}

func serveWebUIRequest(t *testing.T, assets fs.FS, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	SPAHandler(assets).ServeHTTP(rec, req)
	return rec
}

func TestSPA_ServesIndexAtRoot(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, "/")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, testIndexHTML, rec.Body.String())
}

func TestSPA_ServesHashedAsset(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, "/assets/app-abc123.js")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "console.log('ok')", rec.Body.String())
	require.Equal(t, webUIImmutableCache, rec.Header().Get("Cache-Control"))
}

func TestSPA_FallsBackForClientRoute(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, "/clusters/123")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, testIndexHTML, rec.Body.String())
}

func TestSPA_MissingAssetIs404(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, "/assets/nope.js")

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotContains(t, strings.ToLower(rec.Body.String()), "<html")
}

func TestSPA_ApiPathIs404Json(t *testing.T) {
	router := NewRouter(
		UIConfig{Enabled: true, Password: "secret"},
		testWebUIAssets(),
		NewSessionStore(0),
		NewAuth("agent-token"),
		nil,
		nil,
		&API{},
		NewTopologyAPI(nil),
		NewAshAPI(nil),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Contains(t, rec.Body.String(), `"error"`)
}

func TestSPA_RejectsPathTraversal(t *testing.T) {
	for _, target := range []string{"/../go.mod", "/%2e%2e/go.mod"} {
		t.Run(target, func(t *testing.T) {
			rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, target)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.NotContains(t, rec.Body.String(), "module github.com/manprint/pglens")
		})
	}
}

func TestSPA_RejectsPost(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodPost, "/")

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestSPA_IndexIsNotCached(t *testing.T) {
	rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, "/")

	require.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
}

func TestSPA_SetsNosniff(t *testing.T) {
	for _, target := range []string{"/", "/assets/app-abc123.js", "/assets/nope.js"} {
		t.Run(target, func(t *testing.T) {
			rec := serveWebUIRequest(t, testWebUIAssets(), http.MethodGet, target)

			require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
		})
	}
}

func TestRouter_UIDisabledServesNoSPA(t *testing.T) {
	router := NewRouter(
		UIConfig{Enabled: false},
		testWebUIAssets(),
		nil,
		nil,
		nil,
		nil,
		&API{},
		NewTopologyAPI(nil),
		NewAshAPI(nil),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotEqual(t, testIndexHTML, rec.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}
