package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type settingAPIEntry struct {
	Name           string    `json:"name"`
	Value          *string   `json:"value"`
	Unit           string    `json:"unit"`
	Source         string    `json:"source"`
	Context        string    `json:"context"`
	PendingRestart string    `json:"pending_restart"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	ChangedAt      time.Time `json:"changed_at"`
}

type settingsAPIResponse struct {
	InstanceID string            `json:"instance_id"`
	Settings   []settingAPIEntry `json:"settings"`
}

func (a *API) registerSettingsRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/settings", a.handleSettings)
	r.Get("/api/v1/clusters/{id}/settings-drift", a.handleSettingsDrift)
}

func (a *API) handleSettings(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	if raw := r.URL.Query().Get("changed_since"); raw != "" {
		if _, parseErr := parseTimeParam(raw); parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid changed_since", parseErr.Error())
			return
		}
	}
	response := settingsAPIResponse{InstanceID: id.String(), Settings: []settingAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	query := `SELECT key, value_text, labels->>'unit', labels->>'source', labels->>'context', labels->>'pending_restart', first_seen, last_seen, changed_at FROM object_facts WHERE instance_id=$1 AND kind='setting'`
	args := []any{id}
	if raw := r.URL.Query().Get("changed_since"); raw != "" {
		since, parseErr := parseTimeParam(raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid changed_since", parseErr.Error())
			return
		}
		query += " AND changed_at >= $2"
		args = append(args, since)
	}
	query += " ORDER BY key"
	rows, err := a.pool.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var entry settingAPIEntry
		if err := rows.Scan(&entry.Name, &entry.Value, &entry.Unit, &entry.Source, &entry.Context, &entry.PendingRestart, &entry.FirstSeen, &entry.LastSeen, &entry.ChangedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "settings scan failed", err.Error())
			return
		}
		response.Settings = append(response.Settings, entry)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "settings rows failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

var settingsDriftExcluded = []string{
	"data_directory", "hot_standby", "listen_addresses", "port",
	"primary_conninfo", "primary_slot_name", "synchronous_standby_names",
}

type driftValue struct {
	InstanceID string `json:"instance_id"`
	Role       string `json:"role"`
	Value      string `json:"value"`
}
type driftEntry struct {
	Name   string       `json:"name"`
	Values []driftValue `json:"values"`
}

func (a *API) handleSettingsDrift(w http.ResponseWriter, r *http.Request) {
	clusterID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}
	response := []driftEntry{}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	rows, err := a.pool.Query(r.Context(), `SELECT f.key, f.value_text, i.instance_id::text, i.role, f.labels->>'context' FROM object_facts f JOIN instances i ON i.instance_id=f.instance_id WHERE f.cluster_id=$1 AND f.kind='setting' ORDER BY f.key, i.instance_id`, clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "settings drift query failed", err.Error())
		return
	}
	defer rows.Close()
	grouped := map[string][]driftValue{}
	for rows.Next() {
		var name, context string
		var value *string
		var instance string
		var role string
		if err := rows.Scan(&name, &value, &instance, &role, &context); err != nil {
			writeError(w, http.StatusInternalServerError, "settings drift scan failed", err.Error())
			return
		}
		if context == "internal" || settingIsDriftExcluded(name) {
			continue
		}
		text := ""
		if value != nil {
			text = *value
		}
		grouped[name] = append(grouped[name], driftValue{InstanceID: instance, Role: role, Value: text})
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "settings drift rows failed", err.Error())
		return
	}
	for name, values := range grouped {
		if len(values) < 2 || allDriftValuesEqual(values) {
			continue
		}
		response = append(response, driftEntry{Name: name, Values: values})
	}
	sort.Slice(response, func(i, j int) bool { return response[i].Name < response[j].Name })
	writeJSON(w, http.StatusOK, response)
}

func settingIsDriftExcluded(name string) bool {
	for _, excluded := range settingsDriftExcluded {
		if name == excluded {
			return true
		}
	}
	return false
}

func allDriftValuesEqual(values []driftValue) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values[1:] {
		if value.Value != values[0].Value {
			return false
		}
	}
	return true
}
