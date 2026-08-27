package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter builds the full server handler: liveness/readiness, the agent
// ingest endpoint (wired to inv/pipeline), and the read API (api.RegisterRoutes).
func NewRouter(auth *Auth, inv *Inventory, pipeline *Pipeline, api *API, topoAPI *TopologyAPI, ashAPI *AshAPI) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Post("/api/v1/push", IngestHandler(auth, inv, pipeline))
	api.RegisterRoutes(r)
	topoAPI.RegisterRoutes(r)
	ashAPI.RegisterRoutes(r)
	return r
}
