package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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
	r.Get("/api/v1/instances/{id}/unindexed-foreign-keys", a.handleUnindexedForeignKeys)
	r.Get("/api/v1/instances/{id}/extensions", a.handleExtensions)
	r.Get("/api/v1/instances/{id}/spatial-columns", a.handleSpatialColumns)
	r.Get("/api/v1/instances/{id}/sequences", a.handleSequences)
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

type unindexedForeignKeyAPIEntry struct {
	Datname    string    `json:"datname"`
	Schemaname string    `json:"schemaname"`
	Relname    string    `json:"relname"`
	Constraint string    `json:"constraint"`
	Columns    string    `json:"columns"`
	Referenced string    `json:"referenced"`
	TableBytes int64     `json:"table_bytes"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

type unindexedForeignKeysAPIResponse struct {
	InstanceID  string                        `json:"instance_id"`
	ForeignKeys []unindexedForeignKeyAPIEntry `json:"foreign_keys"`
	// TruncatedDatabases lists the databases with more unindexed foreign
	// keys than the agent reports; their largest tables come first.
	TruncatedDatabases []string `json:"truncated_databases"`
}

func (a *API) handleUnindexedForeignKeys(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := unindexedForeignKeysAPIResponse{InstanceID: id.String(), ForeignKeys: []unindexedForeignKeyAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "fk_unindexed", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "foreign keys query failed", err.Error())
		return
	}
	for _, f := range facts {
		bytes, _ := strconv.ParseInt(f.Labels["table_bytes"], 10, 64)
		response.ForeignKeys = append(response.ForeignKeys, unindexedForeignKeyAPIEntry{
			Datname: f.Datname, Schemaname: f.Labels["schemaname"], Relname: f.Labels["relname"],
			Constraint: f.Labels["constraint"], Columns: f.ValueText, Referenced: f.Labels["referenced"],
			TableBytes: bytes, FirstSeen: f.FirstSeen, LastSeen: f.LastSeen,
		})
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "fk_indexes", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "foreign keys query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type extensionAPIEntry struct {
	Datname        string    `json:"datname"`
	Name           string    `json:"name"`
	Schema         string    `json:"schema"`
	Version        string    `json:"version"`
	DefaultVersion string    `json:"default_version"`
	Upgrade        string    `json:"upgrade"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	ChangedAt      time.Time `json:"changed_at"`
}

type extensionsAPIResponse struct {
	InstanceID         string              `json:"instance_id"`
	Extensions         []extensionAPIEntry `json:"extensions"`
	TruncatedDatabases []string            `json:"truncated_databases"`
}

func (a *API) handleExtensions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := extensionsAPIResponse{InstanceID: id.String(), Extensions: []extensionAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "extension", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "extensions query failed", err.Error())
		return
	}
	for _, f := range facts {
		response.Extensions = append(response.Extensions, extensionAPIEntry{
			Datname: f.Datname, Name: f.Key, Schema: f.Labels["schema"], Version: f.ValueText,
			DefaultVersion: f.Labels["default_version"], Upgrade: f.Labels["upgrade"],
			FirstSeen: f.FirstSeen, LastSeen: f.LastSeen, ChangedAt: f.ChangedAt,
		})
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "extensions", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "extensions query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type spatialColumnAPIEntry struct {
	Datname    string `json:"datname"`
	Schemaname string `json:"schemaname"`
	Relname    string `json:"relname"`
	Column     string `json:"column"`
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	// SRID is nil for a column declared without a typmod, whose values
	// each carry their own SRID.
	SRID *int `json:"srid"`
	// SRIDKnown is nil when spatial_ref_sys was not readable or the column
	// declares no SRID.
	SRIDKnown   *bool     `json:"srid_known"`
	IndexMethod string    `json:"index_method"`
	Rows        float64   `json:"rows"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

type spatialColumnsAPIResponse struct {
	InstanceID         string                  `json:"instance_id"`
	Columns            []spatialColumnAPIEntry `json:"columns"`
	TruncatedDatabases []string                `json:"truncated_databases"`
}

func (a *API) handleSpatialColumns(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := spatialColumnsAPIResponse{InstanceID: id.String(), Columns: []spatialColumnAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "spatial_column", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "spatial columns query failed", err.Error())
		return
	}
	for _, f := range facts {
		entry := spatialColumnAPIEntry{
			Datname: f.Datname, Schemaname: f.Labels["schemaname"], Relname: f.Labels["relname"],
			Column: f.Labels["column"], Type: f.ValueText, Subtype: f.Labels["subtype"],
			IndexMethod: f.Labels["index_method"], FirstSeen: f.FirstSeen, LastSeen: f.LastSeen,
		}
		if srid, err := strconv.Atoi(f.Labels["srid"]); err == nil {
			entry.SRID = &srid
		}
		if known, err := strconv.ParseBool(f.Labels["srid_known"]); err == nil {
			entry.SRIDKnown = &known
		}
		entry.Rows, _ = strconv.ParseFloat(f.Labels["rows"], 64)
		response.Columns = append(response.Columns, entry)
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "postgis_columns", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "spatial columns query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// sequenceAPIEntry is one sequence. The bounds and values are decimal
// strings: a bigint sequence's range does not fit a JSON number a browser
// reads exactly.
type sequenceAPIEntry struct {
	Datname    string `json:"datname"`
	Schemaname string `json:"schemaname"`
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	State      string `json:"state"`
	Start      string `json:"start"`
	Increment  string `json:"increment"`
	Min        string `json:"min"`
	Max        string `json:"max"`
	Cycle      bool   `json:"cycle"`
	// LastValue is nil when the sequence was never called or its value is
	// not readable (State says which).
	LastValue   *string `json:"last_value"`
	Limit       string  `json:"limit"`
	LimitSource string  `json:"limit_source"`
	Exhausts    bool    `json:"exhausts"`
	// UsedRatio and Remaining are nil when the value is not readable.
	UsedRatio   *float64  `json:"used_ratio"`
	Remaining   *string   `json:"remaining"`
	TableSchema string    `json:"table_schema"`
	Table       string    `json:"table"`
	Column      string    `json:"column"`
	ColumnType  string    `json:"column_type"`
	Link        string    `json:"link"`
	Columns     int       `json:"columns"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	ChangedAt   time.Time `json:"changed_at"`
}

type sequencesAPIResponse struct {
	InstanceID         string             `json:"instance_id"`
	Sequences          []sequenceAPIEntry `json:"sequences"`
	TruncatedDatabases []string           `json:"truncated_databases"`
}

func (a *API) handleSequences(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := sequencesAPIResponse{InstanceID: id.String(), Sequences: []sequenceAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "sequence", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sequences query failed", err.Error())
		return
	}
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	for _, f := range facts {
		l := f.Labels
		entry := sequenceAPIEntry{
			Datname: f.Datname, Schemaname: l["schemaname"], Name: l["sequence"], DataType: l["data_type"], State: f.ValueText,
			Start: l["start"], Increment: l["increment"], Min: l["min"], Max: l["max"], Cycle: l["cycle"] == "true",
			LastValue: optional(l["last_value"]), Limit: l["limit"], LimitSource: l["limit_source"], Exhausts: l["exhausts"] == "true",
			Remaining:   optional(l["remaining"]),
			TableSchema: l["table_schema"], Table: l["table"], Column: l["column"], ColumnType: l["column_type"], Link: l["link"],
			FirstSeen: f.FirstSeen, LastSeen: f.LastSeen, ChangedAt: f.ChangedAt,
		}
		if ratio, err := strconv.ParseFloat(l["used_ratio"], 64); err == nil {
			entry.UsedRatio = &ratio
		}
		entry.Columns, _ = strconv.Atoi(l["columns"])
		response.Sequences = append(response.Sequences, entry)
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "sequences", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "sequences query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}
