package server

import (
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	webUIImmutableCache = "public, max-age=31536000, immutable"
	webUINoCache        = "no-cache, no-store, must-revalidate"
)

// SPAHandler serves the embedded web UI and falls back to index.html for
// client-side routes that do not name a real asset.
func SPAHandler(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		requestPath, ok := cleanWebUIPath(r.URL)
		if !ok {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		if requestPath == "" {
			requestPath = "index.html"
		}

		if info, err := fs.Stat(assets, requestPath); err == nil && info.Mode().IsRegular() {
			if strings.HasPrefix(requestPath, "assets/") {
				w.Header().Set("Cache-Control", webUIImmutableCache)
			} else {
				w.Header().Set("Cache-Control", webUINoCache)
			}
			http.ServeFileFS(w, r, assets, requestPath)
			return
		}

		if strings.HasPrefix(requestPath, "assets/") {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", webUINoCache)
		http.ServeFileFS(w, r, assets, "index.html")
	})
}

func cleanWebUIPath(u *url.URL) (string, bool) {
	if hasParentSegment(u.Path) || (u.RawPath != "" && hasParentSegment(u.RawPath)) {
		return "", false
	}

	cleaned := path.Clean(u.Path)
	if cleaned == "." || cleaned == "/" {
		return "", true
	}
	return strings.TrimPrefix(cleaned, "/"), true
}

func hasParentSegment(value string) bool {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return true
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
