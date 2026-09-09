package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// NewRouter builds the full server handler: liveness/readiness, the agent
// ingest endpoint (wired to inv/pipeline), and the read API (api.RegisterRoutes).
func NewRouter(uiCfg UIConfig, assets fs.FS, store *SessionStore, auth *Auth, inv *Inventory, pipeline *Pipeline, api *API, topoAPI *TopologyAPI, ashAPI *AshAPI, alertAPIs ...*AlertAPI) http.Handler {
	r := chi.NewRouter()
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
