package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type planHistoryItem struct {
	PlanID     int64           `json:"plan_id"`
	PlanHash   string          `json:"plan_hash"`
	CapturedAt time.Time       `json:"captured_at"`
	Analyzed   bool            `json:"analyzed"`
	Plan       json.RawMessage `json:"plan"`
	Changed    bool            `json:"changed"`
}

type planHistoryResponse struct {
	Plans       []planHistoryItem `json:"plans"`
	TotalShapes int               `json:"total_shapes"`
}

func (a *API) registerPlanRoutes(r interface {
	Get(string, http.HandlerFunc)
}) {
	r.Get("/api/v1/plans", a.handlePlans)
}

func (a *API) handlePlans(w http.ResponseWriter, r *http.Request) {
	queryID, err := strconv.ParseInt(r.URL.Query().Get("queryid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "queryid is required", "queryid must be an integer")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 500 {
		limit = 500
	}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, planHistoryResponse{Plans: []planHistoryItem{}})
		return
	}
	var instanceID any
	if raw := r.URL.Query().Get("instance_id"); raw != "" {
		parsed, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "invalid instance id", parseErr.Error())
			return
		}
		instanceID = parsed
	}
	datname := r.URL.Query().Get("datname")
	rows, err := a.pool.Query(r.Context(), `SELECT plan_id, plan_hash, captured_at, analyzed, plan FROM query_plans WHERE ($1::uuid IS NULL OR instance_id=$1) AND queryid=$2 AND ($3='' OR datname=$3) ORDER BY captured_at DESC, plan_id DESC LIMIT $4`, instanceID, queryID, datname, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan history query failed", err.Error())
		return
	}
	defer rows.Close()
	out := planHistoryResponse{Plans: make([]planHistoryItem, 0)}
	for rows.Next() {
		var item planHistoryItem
		var rawPlan []byte
		if err := rows.Scan(&item.PlanID, &item.PlanHash, &item.CapturedAt, &item.Analyzed, &rawPlan); err != nil {
			writeError(w, http.StatusInternalServerError, "plan history decode failed", err.Error())
			return
		}
		item.Plan = json.RawMessage(rawPlan)
		item.Changed = len(out.Plans) > 0
		out.Plans = append(out.Plans, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "plan history read failed", err.Error())
		return
	}
	out.TotalShapes = len(out.Plans)
	writeJSON(w, http.StatusOK, out)
}
