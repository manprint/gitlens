package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
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
			// The README the interface links to is read in the browser. Go
			// has no type for .md and a host's mime.types may say
			// text/markdown, which browsers offer as a download.
			if path.Ext(requestPath) == ".md" {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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

// contentSecurityPolicy is sent with every response. The interface has one
// inline script, the theme setter in index.html, which runs before the bundle
// loads, so script-src admits it by the hash of the index.html being served
// and no other inline script. The hash follows the script when the interface
// is rebuilt. Styles stay inline-permitted: the component libraries set style
// attributes and inject style sheets at run time.
func contentSecurityPolicy(assets fs.FS) string {
	scriptSources := []string{"'self'"}
	if assets != nil {
		if index, err := fs.ReadFile(assets, "index.html"); err == nil {
			for _, match := range inlineScriptPattern.FindAllSubmatch(index, -1) {
				sum := sha256.Sum256(match[1])
				scriptSources = append(scriptSources, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
			}
		}
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scriptSources, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"worker-src 'self' blob:",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// inlineScriptPattern matches a script element with no attributes, the only
// kind of inline script the policy admits.
var inlineScriptPattern = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

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
