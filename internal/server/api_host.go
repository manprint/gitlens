package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const hostMetricStaleAfter = 90 * time.Second

type hostAPIResponse struct {
	InstanceID string             `json:"instance_id"`
	Available  bool               `json:"available"`
	Reason     string             `json:"reason,omitempty"`
	Source     string             `json:"source,omitempty"`
	SampledAt  time.Time          `json:"sampled_at,omitempty"`
	Stale      bool               `json:"stale,omitempty"`
	Metrics    map[string]float64 `json:"-"`
}

func (a *API) registerHostRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/host", a.handleHost)
}

func hostSource(value float64) string {
	switch int(value) {
	case 1:
		return "cgroup_v1"
	case 2:
		return "cgroup_v2"
	default:
		return "host"
	}
}

func (a *API) handleHost(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	resp := hostAPIResponse{InstanceID: id.String(), Metrics: map[string]float64{}}
	if a.pool != nil {
		rows, queryErr := a.pool.Query(r.Context(), `SELECT ts, metric, value FROM metrics WHERE tenant_id='default' AND instance_id=$1 AND metric LIKE 'host_%' ORDER BY ts DESC`, id)
		if queryErr == nil {
			defer rows.Close()
			for rows.Next() {
				var ts time.Time
				var name string
				var value float64
				if rows.Scan(&ts, &name, &value) != nil {
					continue
				}
				if _, exists := resp.Metrics[name]; exists {
					continue
				}
				resp.Metrics[name] = value
				if resp.SampledAt.IsZero() {
					resp.SampledAt = ts
				}
			}
		}
	}
	if resp.SampledAt.IsZero() {
		resp.Available = false
		resp.Reason = "target is not local to any agent"
		resp.Metrics = nil
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Available = true
	resp.Source = hostSource(resp.Metrics["host_metrics_source"])
	resp.Stale = time.Since(resp.SampledAt) > hostMetricStaleAfter

	data := map[string]any{
		"instance_id": resp.InstanceID,
		"available":   resp.Available,
		"source":      resp.Source,
		"sampled_at":  resp.SampledAt,
		"stale":       resp.Stale,
	}
	for name, value := range resp.Metrics {
		if name == "host_metrics_source" {
			continue
		}
		data[name] = value
	}
	writeJSON(w, http.StatusOK, json.RawMessage(mustJSON(data)))
}

func mustJSON(value any) []byte {
	b, _ := json.Marshal(value)
	return b
}
