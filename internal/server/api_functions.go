package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// functionWindow is the span the function rates are averaged over. The
// function_stats counters reach the store as per-second rates, one per
// scrape, so the average of a window's rates is its rate.
const functionWindow = time.Hour

// functionListLimit bounds the functions one response lists, busiest first.
const functionListLimit = 200

type functionTrackingAPIEntry struct {
	Datname          string    `json:"datname"`
	TrackFunctions   string    `json:"track_functions"`
	TrackedFunctions int       `json:"tracked_functions"`
	PLFunctions      int       `json:"pl_functions"`
	LastSeen         time.Time `json:"last_seen"`
}

type functionStatAPIEntry struct {
	Datname          string  `json:"datname"`
	Schemaname       string  `json:"schemaname"`
	Name             string  `json:"name"`
	Arguments        string  `json:"arguments"`
	Language         string  `json:"language"`
	CallsPerSecond   float64 `json:"calls_per_second"`
	TotalMsPerSecond float64 `json:"total_ms_per_second"`
	SelfMsPerSecond  float64 `json:"self_ms_per_second"`
	// MeanMs is the mean time per call over the window, nil when the
	// function was not called in it.
	MeanMs   *float64  `json:"mean_ms"`
	LastSeen time.Time `json:"last_seen"`
}

type functionsAPIResponse struct {
	InstanceID    string                     `json:"instance_id"`
	WindowSeconds int                        `json:"window_seconds"`
	Databases     []functionTrackingAPIEntry `json:"databases"`
	Functions     []functionStatAPIEntry     `json:"functions"`
}

func (a *API) handleFunctions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := functionsAPIResponse{InstanceID: id.String(), WindowSeconds: int(functionWindow.Seconds()),
		Databases: []functionTrackingAPIEntry{}, Functions: []functionStatAPIEntry{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "function_tracking", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "functions query failed", err.Error())
		return
	}
	for _, f := range facts {
		entry := functionTrackingAPIEntry{Datname: f.Datname, TrackFunctions: f.ValueText, LastSeen: f.LastSeen}
		entry.TrackedFunctions, _ = strconv.Atoi(f.Labels["tracked"])
		entry.PLFunctions, _ = strconv.Atoi(f.Labels["pl_functions"])
		response.Databases = append(response.Databases, entry)
	}
	rows, err := a.pool.Query(r.Context(), `SELECT datname, COALESCE(labels->>'schemaname', ''), COALESCE(labels->>'function', ''),
			COALESCE(labels->>'arguments', ''), COALESCE(labels->>'language', ''),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_function_calls_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_function_total_time_ms_total'), 0),
			COALESCE(avg(value) FILTER (WHERE metric = 'pg_function_self_time_ms_total'), 0),
			max(ts)
		FROM metrics
		WHERE tenant_id = 'default' AND instance_id = $1
			AND metric IN ('pg_function_calls_total', 'pg_function_total_time_ms_total', 'pg_function_self_time_ms_total')
			AND ts >= now() - make_interval(secs => $2) AND ($3 = '' OR datname = $3)
		GROUP BY 1, 2, 3, 4, 5
		ORDER BY 8 DESC, 6 DESC, 1, 2, 3, 4
		LIMIT $4`, id, functionWindow.Seconds(), datname, functionListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "functions query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var e functionStatAPIEntry
		if err := rows.Scan(&e.Datname, &e.Schemaname, &e.Name, &e.Arguments, &e.Language,
			&e.CallsPerSecond, &e.TotalMsPerSecond, &e.SelfMsPerSecond, &e.LastSeen); err != nil {
			writeError(w, http.StatusInternalServerError, "functions scan failed", err.Error())
			return
		}
		if e.CallsPerSecond > 0 {
			mean := e.TotalMsPerSecond / e.CallsPerSecond
			e.MeanMs = &mean
		}
		response.Functions = append(response.Functions, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "functions query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type securityDefinerAPIEntry struct {
	Datname        string `json:"datname"`
	Schemaname     string `json:"schemaname"`
	Name           string `json:"name"`
	Arguments      string `json:"arguments"`
	Signature      string `json:"signature"`
	Kind           string `json:"kind"`
	Language       string `json:"language"`
	Owner          string `json:"owner"`
	OwnerSuperuser bool   `json:"owner_superuser"`
	PublicExecute  bool   `json:"public_execute"`
	// SearchPath is the search_path the function pins, nil when it pins
	// none.
	SearchPath *string   `json:"search_path"`
	PgTempLast bool      `json:"pg_temp_last"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

type securityDefinerAPIResponse struct {
	InstanceID         string                    `json:"instance_id"`
	Functions          []securityDefinerAPIEntry `json:"functions"`
	TruncatedDatabases []string                  `json:"truncated_databases"`
}

func (a *API) handleSecurityDefinerFunctions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", err.Error())
		return
	}
	datname := r.URL.Query().Get("datname")
	response := securityDefinerAPIResponse{InstanceID: id.String(), Functions: []securityDefinerAPIEntry{}, TruncatedDatabases: []string{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, response)
		return
	}
	facts, err := latestCatalogFacts(r.Context(), a.pool, id, "security_definer_function", datname)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "security definer functions query failed", err.Error())
		return
	}
	for _, f := range facts {
		l := f.Labels
		entry := securityDefinerAPIEntry{
			Datname: f.Datname, Schemaname: l["schemaname"], Name: l["name"], Arguments: l["arguments"], Signature: f.Key,
			Kind: l["kind"], Language: l["language"], Owner: l["owner"], OwnerSuperuser: l["owner_superuser"] == "true",
			PublicExecute: l["public_execute"] == "true", PgTempLast: l["pg_temp_last"] == "true",
			FirstSeen: f.FirstSeen, LastSeen: f.LastSeen,
		}
		if f.ValueText == "pinned" {
			path := l["search_path"]
			entry.SearchPath = &path
		}
		response.Functions = append(response.Functions, entry)
	}
	if response.TruncatedDatabases, err = catalogTruncatedDatabases(r.Context(), a.pool, id, "function_security", datname); err != nil {
		writeError(w, http.StatusInternalServerError, "security definer functions query failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, response)
}
