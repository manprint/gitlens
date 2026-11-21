package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	defaultPlanSamplesLimit = 200
	maxPlanSamplesLimit     = 1000
	defaultPlanSamplesRange = 7 * 24 * time.Hour
)

// planSampleJSON is one row of GET /api/v1/plans/samples. Parameters is
// present only with include_parameters=true.
type planSampleJSON struct {
	TS            time.Time `json:"ts"`
	Datname       string    `json:"datname"`
	QueryID       string    `json:"queryid"`
	PlanHash      string    `json:"plan_hash"`
	DurationMS    float64   `json:"duration_ms"`
	Analyzed      bool      `json:"analyzed"`
	HasParameters bool      `json:"has_parameters"`
	Parameters    *string   `json:"parameters,omitempty"`
}

type planSamplesResponse struct {
	Samples []planSampleJSON `json:"samples"`
}

// purgePlanParametersKind is the command_audit kind of a parameter purge; the
// table has no CHECK on kind (migration 0010).
const purgePlanParametersKind = "purge_plan_parameters"

func (a *API) registerPlanSampleRoutes(r chi.Router) {
	r.Get("/api/v1/plans/samples", a.handlePlanSamples)
	r.Delete("/api/v1/plans/samples/parameters", a.handlePurgePlanParameters)
}

// handlePlanSamples lists the automatic plan samples of one statement of an
// instance, newest first (plan 007 7.4). to defaults to now and from to seven
// days before to.
func (a *API) handlePlanSamples(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceID, err := uuid.Parse(q.Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance_id", "query param instance_id must be a UUID")
		return
	}
	queryID, err := strconv.ParseInt(q.Get("queryid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid queryid", "query param queryid must be an integer")
		return
	}
	to := time.Now().UTC()
	if s := q.Get("to"); s != "" {
		if to, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid to", err.Error())
			return
		}
	}
	from := to.Add(-defaultPlanSamplesRange)
	if s := q.Get("from"); s != "" {
		if from, err = parseTimeParam(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid from", err.Error())
			return
		}
	}
	if !from.Before(to) {
		writeError(w, http.StatusBadRequest, "invalid range", "from must be before to")
		return
	}
	limit := defaultPlanSamplesLimit
	if s := q.Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > maxPlanSamplesLimit {
			writeError(w, http.StatusBadRequest, "invalid limit", fmt.Sprintf("limit must be between 1 and %d, got %q", maxPlanSamplesLimit, s))
			return
		}
		limit = v
	}
	include := false
	if s := q.Get("include_parameters"); s != "" {
		if include, err = strconv.ParseBool(s); err != nil {
			writeError(w, http.StatusBadRequest, "invalid include_parameters", "include_parameters must be true or false")
			return
		}
	}
	out := planSamplesResponse{Samples: []planSampleJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var sb strings.Builder
	sb.WriteString(`SELECT ts, datname, queryid, plan_hash, duration_ms, analyzed, parameters FROM plan_samples
WHERE tenant_id='default' AND instance_id=$1 AND queryid=$2 AND ts >= $3 AND ts < $4`)
	args := []any{instanceID, queryID, from, to}
	if s := q.Get("database"); s != "" {
		args = append(args, s)
		fmt.Fprintf(&sb, " AND datname=$%d", len(args))
	}
	if s := q.Get("plan_hash"); s != "" {
		args = append(args, s)
		fmt.Fprintf(&sb, " AND plan_hash=$%d", len(args))
	}
	args = append(args, limit)
	fmt.Fprintf(&sb, " ORDER BY ts DESC LIMIT $%d", len(args))
	rows, err := a.pool.Query(r.Context(), sb.String(), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan samples query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row planSampleJSON
		var qid int64
		var params *string
		if err := rows.Scan(&row.TS, &row.Datname, &qid, &row.PlanHash, &row.DurationMS, &row.Analyzed, &params); err != nil {
			writeError(w, http.StatusInternalServerError, "plan samples decode failed", err.Error())
			return
		}
		row.QueryID = strconv.FormatInt(qid, 10)
		row.HasParameters = params != nil
		if include {
			row.Parameters = params
		}
		out.Samples = append(out.Samples, row)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "plan samples read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePurgePlanParameters sets parameters to NULL on every plan sample of
// one instance and keeps the samples (plan 007 7.3). Neither the answer, the
// audit row nor the log line carries a parameter value.
func (a *API) handlePurgePlanParameters(w http.ResponseWriter, r *http.Request) {
	instanceID, err := uuid.Parse(r.URL.Query().Get("instance_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance id", "instance_id must be a UUID")
		return
	}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, map[string]int64{"cleared": 0})
		return
	}
	ctx := r.Context()
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "purge failed", "could not start the transaction")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE plan_samples SET parameters = NULL WHERE instance_id = $1 AND parameters IS NOT NULL`, instanceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "purge failed", "could not clear the parameters")
		return
	}
	cleared := tag.RowsAffected()
	who := requesterOf(r, a.auth)
	args, _ := json.Marshal(map[string]int64{"cleared": cleared})
	if _, err := tx.Exec(ctx, `INSERT INTO command_audit (command_id, instance_id, kind, args, requested_by, outcome) VALUES ($1,$2,$3,$4,$5,'ok')`, uuid.New(), instanceID, purgePlanParametersKind, args, who); err != nil {
		writeError(w, http.StatusInternalServerError, "purge failed", "could not write the audit row")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "purge failed", "could not commit")
		return
	}
	log.Printf("plans: parameters of %d samples of instance %s cleared by %s", cleared, instanceID, who)
	writeJSON(w, http.StatusOK, map[string]int64{"cleared": cleared})
}
