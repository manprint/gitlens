package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
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
			serveWebUIFile(w, r, assets, requestPath)
			return
		}

		if strings.HasPrefix(requestPath, "assets/") {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", webUINoCache)
		serveWebUIFile(w, r, assets, "index.html")
	})
}

// serveWebUIFile serves name, or the gzip copy the build writes next to it
// (name.gz) to a client that accepts gzip: the bundle is about 3.5 MB of
// text, a quarter of that compressed, and the server compresses nothing at
// run time.
func serveWebUIFile(w http.ResponseWriter, r *http.Request, assets fs.FS, name string) {
	// The README the interface links to is read in the browser. Go has no
	// type for .md and a host's mime.types may say text/markdown, which
	// browsers offer as a download.
	contentType := mime.TypeByExtension(path.Ext(name))
	if path.Ext(name) == ".md" {
		contentType = "text/plain; charset=utf-8"
	}
	compressed := name + ".gz"
	if info, err := fs.Stat(assets, compressed); err == nil && info.Mode().IsRegular() {
		w.Header().Add("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header.Get("Accept-Encoding")) {
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			}
			w.Header().Set("Content-Encoding", "gzip")
			http.ServeFileFS(w, r, assets, compressed)
			return
		}
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeFileFS(w, r, assets, name)
}

// acceptsGzip reports whether an Accept-Encoding header admits gzip: named
// with a weight above zero, or, when not named, covered by *.
func acceptsGzip(header string) bool {
	named, star := -1.0, -1.0
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		weight := 1.0
		for _, param := range strings.Split(params, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			if strings.EqualFold(strings.TrimSpace(key), "q") {
				if q, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					weight = q
				}
			}
		}
		switch strings.ToLower(strings.TrimSpace(coding)) {
		case "gzip", "x-gzip":
			named = max(named, weight)
		case "*":
			star = max(star, weight)
		}
	}
	if named >= 0 {
		return named > 0
	}
	return star > 0
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
