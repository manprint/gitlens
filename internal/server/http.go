package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the full server handler: liveness/readiness, the agent
// ingest endpoint (wired to inv/pipeline), and the read API (api.RegisterRoutes).
func NewRouter(uiCfg UIConfig, assets fs.FS, store *SessionStore, auth *Auth, inv *Inventory, pipeline *Pipeline, api *API, topoAPI *TopologyAPI, ashAPI *AshAPI, alertAPIs ...*AlertAPI) http.Handler {
	r := chi.NewRouter()
	// The security headers go first, so they are on every response, the 500
	// that the recoverer writes included.
	r.Use(securityHeaders(contentSecurityPolicy(assets)))
	// Recoverer first, so it also covers the credential gate below. Without
	// it a panic in any handler propagates to net/http, which logs the trace
	// and drops the connection with no response at all: the agent sees a bare
	// network error (indistinguishable from the server being down, and
	// retried forever with backoff) and a browser sees a failed fetch. With
	// it the request gets an honest 500 and the process keeps serving.
	r.Use(middleware.Recoverer)
	// A browser states where every request came from. A write from another
	// site, or from a sibling subdomain that SameSite=Strict does not cover,
	// is refused before it can ride on the session cookie. Agents and tools
	// send neither header, so they pass.
	r.Use(refuseCrossOriginWrites())
	// The credential gate is always installed; uiCfg.Enabled only governs the
	// static assets below. See RequireCredential's own comment.
	r.Use(RequireCredential(uiCfg, auth, store))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := api.Ready(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	RegisterSessionRoutes(r, uiCfg, store)
	r.Post("/api/v1/push", IngestHandler(auth, inv, pipeline))
	r.Get("/metrics", MetricsHandler())
	api.RegisterRoutes(r)
	if api != nil {
		NewCommandService(api.pool, auth).RegisterRoutes(r)
	}
	topoAPI.RegisterRoutes(r)
	ashAPI.RegisterRoutes(r)
	if len(alertAPIs) > 0 && alertAPIs[0] != nil {
		alertAPIs[0].RegisterRoutes(r)
	}
	if uiCfg.Enabled && assets != nil {
		spa := SPAHandler(assets)
		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
				return
			}
			spa.ServeHTTP(w, req)
		})
	}
	return r
}

// securityHeaders sets the headers that keep the interface from being framed,
// keep the page address out of outgoing requests, and keep browsers from
// sniffing a response into another type. The policy is the one the interface
// is built for, so it is the same on every response.
func securityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
		})
	}
}

// refuseCrossOriginWrites refuses an unsafe method that the browser reports as
// coming from another origin. The standard library reads Sec-Fetch-Site and,
// when that is absent, compares Origin with Host.
func refuseCrossOriginWrites() func(http.Handler) http.Handler {
	protection := http.NewCrossOriginProtection()
	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusForbidden, "cross_origin", "a write from another origin is refused")
	}))
	return protection.Handler
}
