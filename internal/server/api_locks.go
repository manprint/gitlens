package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type lockAPIResponse struct {
	InstanceID string            `json:"instance_id"`
	SampledAt  *time.Time        `json:"sampled_at"`
	Stale      bool              `json:"stale"`
	Nodes      []json.RawMessage `json:"nodes"`
	Sessions   []json.RawMessage `json:"sessions"`
}

type activityAPIResponse struct {
	InstanceID string                 `json:"instance_id"`
	Stale      bool                   `json:"stale"`
	Metrics    map[string][]apiMetric `json:"metrics"`
	// XminHorizon is the oldest holder of the xmin horizon in the latest
	// activity sample. It is null when no holder is known from a recent sample.
	XminHorizon *xminHorizonAPI `json:"xmin_horizon"`
}

// xminHorizonAPI is the oldest xmin holder. The identity is a pid, a slot
// name, a standby's application_name or a prepared transaction's gid. It is
// never query text.
type xminHorizonAPI struct {
	HolderKind string    `json:"holder_kind"`
	Identity   string    `json:"identity"`
	AgeXIDs    int64     `json:"age_xids"`
	AgeSeconds *float64  `json:"age_seconds"`
	SampledAt  time.Time `json:"sampled_at"`
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
	resp := lockAPIResponse{
		InstanceID: id.String(),
		Stale:      true,
		Nodes:      []json.RawMessage{},
		Sessions:   []json.RawMessage{},
	}
	if a.pool != nil {
		var ts time.Time
		var raw []byte
		err = a.pool.QueryRow(r.Context(), `SELECT ts, tree FROM lock_snapshots WHERE tenant_id='default' AND instance_id=$1`, id).Scan(&ts, &raw)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "query failed", err.Error())
			return
		}
		if err == nil {
			resp.SampledAt = &ts
			resp.Stale = contentionStale(ts, time.Now())
			var tree struct {
				Nodes    []json.RawMessage `json:"nodes"`
				Sessions []json.RawMessage `json:"sessions"`
			}
			// A tree that does not decode is not an empty tree: answering it as a
			// fresh, empty result would show no contention as current data.
			if err := json.Unmarshal(raw, &tree); err != nil {
				log.Printf("decode lock tree of instance %s: %v", id, err)
				writeError(w, http.StatusInternalServerError, "lock tree unreadable", err.Error())
				return
			}
			// A tree that omits a list, or stores null, keeps the empty list the
			// response promises instead of encoding null.
			if tree.Nodes != nil {
				resp.Nodes = tree.Nodes
			}
			if tree.Sessions != nil {
				resp.Sessions = tree.Sessions
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
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// The connection totals, the saturation ratio and the oldest open and
	// idle-in-transaction ages were missing from this list, so the Activity
	// section showed them as never reported although the check sends them on
	// every scrape. pg_deadlocks_total (a per-second rate once stored) comes
	// from database_stats, every 30 seconds: its window is wider, or half the
	// reads missed it. A failed query was an empty, stale-looking answer.
	rows, err := a.pool.Query(r.Context(), `SELECT ts, metric, labels, value FROM metrics
		WHERE tenant_id='default' AND instance_id=$1
		  AND ((ts >= now() - interval '30 seconds' AND metric IN (
		         'pg_connections_used','pg_connections_limit','pg_connections_used_ratio',
		         'pg_connections_by_database','pg_connections_by_application','pg_backends',
		         'pg_max_xact_age_seconds','pg_max_idle_in_transaction_seconds','pg_max_state_age_seconds',
		         'pg_prepared_xacts','pg_oldest_prepared_xact_seconds','pg_max_datfrozenxid_age',
		         'pg_xmin_horizon_age_xids'))
		    OR (ts >= now() - interval '90 seconds' AND metric = 'pg_deadlocks_total'))
		ORDER BY ts DESC`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var m apiMetric
		var name string
		var labels []byte
		if err := rows.Scan(&m.TS, &name, &labels, &m.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		if err := json.Unmarshal(labels, &m.Labels); err != nil {
			log.Printf("decode labels of metric %s for instance %s: %v", name, id, err)
		}
		resp.Metrics[name] = append(resp.Metrics[name], m)
		resp.Stale = false
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	holder, err := a.oldestXminHolder(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	resp.XminHorizon = holder
	writeJSON(w, http.StatusOK, resp)
}

// oldestXminHolder reads the xmin_horizon fact that the activity check writes
// with every sample. It answers nil when no holder fact is fresh: the fact is
// replaced by each sample, so one older than the activity window is no longer
// the instance's holder. A fact that does not decode is logged and left out of
// the answer, which the stored value of a fact the server wrote cannot cause.
func (a *API) oldestXminHolder(ctx context.Context, id uuid.UUID) (*xminHorizonAPI, error) {
	var raw []byte
	var seen time.Time
	err := a.pool.QueryRow(ctx, `SELECT value_json, last_seen FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='xmin_horizon' AND key='oldest'
		  AND last_seen >= now() - interval '30 seconds'`, id).Scan(&raw, &seen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var holder xminHorizonAPI
	if err := json.Unmarshal(raw, &holder); err != nil {
		log.Printf("decode xmin horizon of instance %s: %v", id, err)
		return nil, nil
	}
	holder.SampledAt = seen
	return &holder, nil
}

func (a *API) handleDatabases(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid instance_id", err.Error())
		return
	}
	resp := databaseAPIResponse{InstanceID: id.String(), Databases: []databaseAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT datname, monitored, skip_reason FROM databases WHERE instance_id=$1 ORDER BY datname`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d databaseAPIEntry
		if err := rows.Scan(&d.Datname, &d.Monitored, &d.SkipReason); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed", err.Error())
			return
		}
		resp.Databases = append(resp.Databases, d)
		if !d.Monitored {
			resp.NotMonitoredCount++
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "rows error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
