package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type subscriptionAPIEntry struct {
	Name         string   `json:"name"`
	Database     string   `json:"database"`
	Owner        string   `json:"owner"`
	Enabled      bool     `json:"enabled"`
	SlotName     *string  `json:"slot_name"`
	Publications []string `json:"publications"`
	ApplyWorker  bool     `json:"apply_worker"`
	SyncWorkers  int      `json:"sync_workers"`
	// LastMessageAt is when the apply worker last heard from the publisher,
	// nil without a running worker.
	LastMessageAt *time.Time `json:"last_message_at"`
	ApplyErrors   int64      `json:"apply_errors"`
	SyncErrors    int64      `json:"sync_errors"`
	// Conflicts is nil before PostgreSQL 18, which has no conflict counters.
	Conflicts *int64    `json:"conflicts"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type logicalSlotAPIEntry struct {
	SlotName             string    `json:"slot_name"`
	BytesPerSecond       float64   `json:"bytes_per_second"`
	TxnsPerSecond        float64   `json:"txns_per_second"`
	SpillBytesPerSecond  float64   `json:"spill_bytes_per_second"`
	SpillTxnsPerSecond   float64   `json:"spill_txns_per_second"`
	StreamBytesPerSecond float64   `json:"stream_bytes_per_second"`
	LastSeen             time.Time `json:"last_seen"`
}

type logicalReplicationAPIResponse struct {
	InstanceID    string                 `json:"instance_id"`
	WindowSeconds int                    `json:"window_seconds"`
	Subscriptions []subscriptionAPIEntry `json:"subscriptions"`
	Slots         []logicalSlotAPIEntry  `json:"slots"`
}

func (a *API) handleLogicalReplication(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	response := logicalReplicationAPIResponse{InstanceID: id.String(), WindowSeconds: int(functionWindow.Seconds()),
		Subscriptions: []subscriptionAPIEntry{}, Slots: []logicalSlotAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "subscription", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logical replication query failed", err.Error())
		return
	}
	for _, f := range facts {
		l := f.Labels
		entry := subscriptionAPIEntry{Name: f.Key, Database: l["database"], Owner: l["owner"], Enabled: f.ValueText == "enabled",
			Publications: []string{}, ApplyWorker: l["apply_worker"] == "true", FirstSeen: f.FirstSeen, LastSeen: f.LastSeen}
		if slot := l["slot_name"]; slot != "" {
			entry.SlotName = &slot
		}
		if publications := l["publications"]; publications != "" {
			entry.Publications = strings.Split(publications, ",")
		}
		entry.SyncWorkers, _ = strconv.Atoi(l["sync_workers"])
		entry.ApplyErrors, _ = strconv.ParseInt(l["apply_errors"], 10, 64)
		entry.SyncErrors, _ = strconv.ParseInt(l["sync_errors"], 10, 64)
		if conflicts, err := strconv.ParseInt(l["conflicts"], 10, 64); err == nil {
			entry.Conflicts = &conflicts
		}
		if at, err := time.Parse(time.RFC3339, l["last_msg_receipt_at"]); err == nil {
			entry.LastMessageAt = &at
		}
		response.Subscriptions = append(response.Subscriptions, entry)
	}
	rows, err := a.pool.Query(r.Context(), `SELECT labels->>'slot_name',
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_logical_slot_bytes_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_logical_slot_txns_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_logical_slot_spill_bytes_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_logical_slot_spill_txns_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_logical_slot_stream_bytes_total'), 0),
			max(ts)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1 AND labels ? 'slot_name'
			AND metric IN ('pg_logical_slot_bytes_total', 'pg_logical_slot_txns_total', 'pg_logical_slot_spill_bytes_total',
				'pg_logical_slot_spill_txns_total', 'pg_logical_slot_stream_bytes_total')
			AND ts >= now() - make_interval(secs => $2)
		GROUP BY 1
		ORDER BY 4 DESC, 2 DESC, 1`, id, functionWindow.Seconds())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "logical replication query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e logicalSlotAPIEntry
		if err := rows.Scan(&e.SlotName, &e.BytesPerSecond, &e.TxnsPerSecond, &e.SpillBytesPerSecond, &e.SpillTxnsPerSecond,
			&e.StreamBytesPerSecond, &e.LastSeen); err != nil {
			writeError(w, http.StatusInternalServerError, "logical replication scan failed", err.Error())
			return
		}
		response.Slots = append(response.Slots, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "logical replication query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}
