package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type auditItem struct {
	AuditID     int64          `json:"audit_id"`
	CommandID   uuid.UUID      `json:"command_id"`
	InstanceID  uuid.UUID      `json:"instance_id"`
	Kind        string         `json:"kind"`
	Args        map[string]any `json:"args"`
	RequestedBy string         `json:"requested_by"`
	ExecutedAt  string         `json:"executed_at"`
	Outcome     string         `json:"outcome"`
	Detail      string         `json:"detail,omitempty"`
}

func (a *API) registerAuditRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/command-audit", a.listCommandAudit)
}

func (a *API) listCommandAudit(w http.ResponseWriter, r *http.Request) {
	instanceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance id", err.Error())
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit", "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	if limit > 500 {
		limit = 500
	}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, []auditItem{})
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT audit_id, command_id, instance_id, kind, args, requested_by, executed_at, outcome, detail FROM command_audit WHERE tenant_id='default' AND instance_id=$1 ORDER BY executed_at DESC, audit_id DESC LIMIT $2`, instanceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command audit query failed", err.Error())
		return
	}
	defer rows.Close()
	out := make([]auditItem, 0)
	for rows.Next() {
		var item auditItem
		var rawArgs []byte
		var executedAt time.Time
		var detail *string
		if err := rows.Scan(&item.AuditID, &item.CommandID, &item.InstanceID, &item.Kind, &rawArgs, &item.RequestedBy, &executedAt, &item.Outcome, &detail); err != nil {
			writeError(w, http.StatusInternalServerError, "command audit decode failed", err.Error())
			return
		}
		item.ExecutedAt = executedAt.UTC().Format(time.RFC3339Nano)
		if detail != nil {
			item.Detail = *detail
		}
		if err := json.Unmarshal(rawArgs, &item.Args); err != nil {
			writeError(w, http.StatusInternalServerError, "command audit args decode failed", err.Error())
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "command audit read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
