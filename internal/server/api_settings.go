package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/advisor"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
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
	// Only the settings the latest scrape shipped: a setting put back to its
	// default stops being shipped, and its fact used to be listed with the
	// old value forever.
	query := `SELECT key, value_text, COALESCE(labels->>'unit', ''), COALESCE(labels->>'source', ''), COALESCE(labels->>'context', ''), COALESCE(labels->>'pending_restart', ''), first_seen, last_seen, changed_at FROM object_facts WHERE instance_id=$1 AND kind='setting'
		AND last_seen >= (SELECT max(last_seen) FROM object_facts WHERE instance_id=$1 AND kind='setting') - interval '1 minute'`
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
	// The path carries the unsigned decimal the clusters list returns;
	// ParseInt rejected every identifier above 2^63.
	cid, err := pgtype.ParseClusterID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cluster_id", err.Error())
		return
	}
	clusterID := store.ToDB(cid)
	response := []driftEntry{}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	// The members that reported in the last hour, each with the settings its
	// latest scrape shipped: a decommissioned instance, or a setting since
	// put back to its default, used to show as drift forever. The advisor's
	// config.drift rule reads the same set.
	rows, err := a.pool.Query(r.Context(), `WITH latest AS (
			SELECT instance_id, max(last_seen) AS ts FROM object_facts
			WHERE cluster_id=$1 AND kind='setting' GROUP BY instance_id
		)
		SELECT f.key, f.value_text, i.instance_id::text, i.role, COALESCE(f.labels->>'context', '')
		FROM object_facts f
		JOIN latest l ON l.instance_id=f.instance_id
		JOIN instances i ON i.instance_id=f.instance_id
		WHERE f.cluster_id=$1 AND f.kind='setting' AND f.last_seen >= l.ts - interval '1 minute'
			AND i.last_seen >= now() - interval '1 hour'
		ORDER BY f.key, i.instance_id`, clusterID)
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
		if advisor.SettingDriftExcluded(name, context) {
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
