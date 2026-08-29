package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type lockAPIResponse struct {
	InstanceID string            `json:"instance_id"`
	SampledAt  *time.Time        `json:"sampled_at"`
	Stale      bool              `json:"stale"`
	Nodes      []json.RawMessage `json:"nodes"`
}

type activityAPIResponse struct {
	InstanceID string                 `json:"instance_id"`
	Stale      bool                   `json:"stale"`
	Metrics    map[string][]apiMetric `json:"metrics"`
}

type apiMetric struct {
	TS     time.Time         `json:"ts"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels"`
}

type databaseAPIResponse struct {
	InstanceID        string             `json:"instance_id"`
	Databases         []databaseAPIEntry `json:"databases"`
	NotMonitoredCount int                `json:"not_monitored_count"`
}

type databaseAPIEntry struct {
	Datname    string  `json:"datname"`
	Monitored  bool    `json:"monitored"`
	SkipReason *string `json:"skip_reason,omitempty"`
}

func contentionStale(sampledAt, now time.Time) bool {
	return now.Sub(sampledAt) > 30*time.Second
}

func (a *API) registerContentionRoutes(r chi.Router) {
	r.Get("/api/v1/locks", a.handleLocks)
	r.Get("/api/v1/instances/{id}/activity", a.handleActivity)
	r.Get("/api/v1/instances/{id}/databases", a.handleDatabases)
}

func (a *API) handleLocks(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", "query param instance_id is required and must be a UUID")
		return
	}
	resp := lockAPIResponse{InstanceID: id.String(), Stale: true, Nodes: []json.RawMessage{}}
	if a.pool != nil {
		var ts time.Time
		var raw []byte
		err = a.pool.QueryRow(r.Context(), `SELECT ts, tree FROM lock_snapshots WHERE tenant_id='default' AND instance_id=$1`, id).Scan(&ts, &raw)
		if err == nil {
			resp.SampledAt = &ts
			resp.Stale = contentionStale(ts, time.Now())
			var tree struct {
				Nodes []json.RawMessage `json:"nodes"`
			}
			if json.Unmarshal(raw, &tree) == nil {
				resp.Nodes = tree.Nodes
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleActivity(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid instance_id", err.Error())
		return
	}
	resp := activityAPIResponse{InstanceID: id.String(), Stale: true, Metrics: map[string][]apiMetric{}}
	if a.pool != nil {
		rows, qerr := a.pool.Query(r.Context(), `SELECT ts, metric, labels, value FROM metrics WHERE tenant_id='default' AND instance_id=$1 AND ts >= now() - interval '30 seconds' AND metric IN ('pg_connections_by_database','pg_connections_by_application','pg_backends','pg_max_state_age_seconds','pg_prepared_xacts','pg_oldest_prepared_xact_seconds','pg_max_datfrozenxid_age') ORDER BY ts DESC`, id)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var m apiMetric
				var name string
				var labels []byte
				if rows.Scan(&m.TS, &name, &labels, &m.Value) == nil {
					_ = json.Unmarshal(labels, &m.Labels)
					resp.Metrics[name] = append(resp.Metrics[name], m)
					resp.Stale = false
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleDatabases(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid instance_id", err.Error())
		return
	}
	resp := databaseAPIResponse{InstanceID: id.String(), Databases: []databaseAPIEntry{}}
	if a.pool != nil {
		rows, qerr := a.pool.Query(r.Context(), `SELECT datname, monitored, skip_reason FROM databases WHERE instance_id=$1 ORDER BY datname`, id)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var d databaseAPIEntry
				if rows.Scan(&d.Datname, &d.Monitored, &d.SkipReason) == nil {
					resp.Databases = append(resp.Databases, d)
					if !d.Monitored {
						resp.NotMonitoredCount++
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
