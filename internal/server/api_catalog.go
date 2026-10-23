package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The catalog checks (trigger_inventory, ...) report one fact per object of
// each database. A complete result replaces the database's facts of its
// kind, so the stored facts are the objects that exist; a truncated result
// leaves the facts it did not reach, which the read APIs skip by listing
// only each database's latest batch. A database whose newest fact is older
// than catalogFreshHours (dropped, or no longer scheduled) is left out, as
// the advisor's catalog rules leave it out.
const catalogFreshHours = 3

func (a *API) registerCatalogRoutes(r chi.Router) {
	r.Get("/api/v1/instances/{id}/triggers", a.handleTriggers)
}

// catalogFact is one object_facts row of a catalog kind.
type catalogFact struct {
	Datname   string
	Key       string
	ValueText string
	ValueJSON map[string]any
	Labels    map[string]string
	FirstSeen time.Time
	LastSeen  time.Time
	ChangedAt time.Time
}

// latestCatalogFacts returns the facts of a kind from each database's latest
// batch, by database and key, optionally for one database only.
func latestCatalogFacts(ctx context.Context, pool dbPool, id uuid.UUID, kind, datname string) ([]catalogFact, error) {
	rows, err := pool.Query(ctx, `WITH latest AS (
			SELECT datname, max(last_seen) AS ts FROM object_facts
			WHERE tenant_id='default' AND instance_id=$1 AND kind=$2 AND ($3='' OR datname=$3)
			GROUP BY datname
		)
		SELECT f.datname, f.key, COALESCE(f.value_text, ''), f.value_json, f.labels, f.first_seen, f.last_seen, f.changed_at
		FROM object_facts f
		JOIN latest l ON l.datname=f.datname
		WHERE f.tenant_id='default' AND f.instance_id=$1 AND f.kind=$2
			AND f.last_seen >= l.ts - interval '1 minute'
			AND l.ts >= now() - make_interval(hours => $4)
		ORDER BY f.datname, f.key`, id, kind, datname, catalogFreshHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facts []catalogFact
	for rows.Next() {
		var f catalogFact
		var rawValue, rawLabels []byte
		if err := rows.Scan(&f.Datname, &f.Key, &f.ValueText, &rawValue, &rawLabels, &f.FirstSeen, &f.LastSeen, &f.ChangedAt); err != nil {
			return nil, err
		}
		f.Labels = map[string]string{}
		if len(rawLabels) > 0 {
			_ = json.Unmarshal(rawLabels, &f.Labels)
		}
		if len(rawValue) > 0 {
			_ = json.Unmarshal(rawValue, &f.ValueJSON)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// catalogTruncatedDatabases returns the databases whose latest result of the
// check was cut at its limit: their list is the first objects only.
func catalogTruncatedDatabases(ctx context.Context, pool dbPool, id uuid.UUID, check, datname string) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT datname FROM object_facts
		WHERE tenant_id='default' AND instance_id=$1 AND kind='check_partial' AND key=$2
			AND ($3='' OR datname=$3) AND last_seen >= now() - make_interval(hours => $4)
		ORDER BY datname`, id, check, datname, catalogFreshHours)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	databases := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		databases = append(databases, name)
	}
	return databases, rows.Err()
}

type triggerAPIEntry struct {
	Datname        string    `json:"datname"`
	Schemaname     string    `json:"schemaname"`
	Relname        string    `json:"relname"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	State          string    `json:"state"`
	Function       string    `json:"function"`
	Timing         string    `json:"timing"`
	Events         string    `json:"events"`
	Level          string    `json:"level"`
	Constraint     string    `json:"constraint"`
	ConstraintType string    `json:"constraint_type"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	ChangedAt      time.Time `json:"changed_at"`
}

type triggersAPIResponse struct {
	InstanceID string            `json:"instance_id"`
	Triggers   []triggerAPIEntry `json:"triggers"`
	// TruncatedDatabases lists the databases with more triggers than the
	// agent reports; their disabled triggers come first.
	TruncatedDatabases []string `json:"truncated_databases"`
}

func (a *API) handleTriggers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := triggersAPIResponse{InstanceID: id.String(), Triggers: []triggerAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "trigger", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "triggers query failed", err.Error())
		return
	}
	for _, f := range facts {
		response.Triggers = append(response.Triggers, triggerAPIEntry{
			Datname: f.Datname, Schemaname: f.Labels["schemaname"], Relname: f.Labels["relname"], Name: f.Labels["trigger"],
			Kind: f.Labels["kind"], State: f.ValueText, Function: f.Labels["function"],
			Timing: f.Labels["timing"], Events: f.Labels["events"], Level: f.Labels["level"],
			Constraint: f.Labels["constraint"], ConstraintType: f.Labels["constraint_type"],
			FirstSeen: f.FirstSeen, LastSeen: f.LastSeen, ChangedAt: f.ChangedAt,
		})
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "trigger_inventory", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "triggers query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}
