package server

import (
	"fmt"
	"net/http"
	"time"
)

// maxPlanChanges caps the plan changes returned by GET /api/v1/plans/stats.
const maxPlanChanges = 100

// planStatJSON is the duration statistics of one plan of a statement over the
// window, from its plan_samples rows.
type planStatJSON struct {
	PlanHash        string    `json:"plan_hash"`
	Samples         int64     `json:"samples"`
	AvgMS           float64   `json:"avg_ms"`
	P95MS           float64   `json:"p95_ms"`
	MinMS           float64   `json:"min_ms"`
	MaxMS           float64   `json:"max_ms"`
	FirstTS         time.Time `json:"first_ts"`
	LastTS          time.Time `json:"last_ts"`
	AnalyzedSamples int64     `json:"analyzed_samples"`
	HasPlan         bool      `json:"has_plan"`
}

// planChangeJSON is a sample whose plan differs from the previous sample of the
// same statement in the same database.
type planChangeJSON struct {
	TS           time.Time `json:"ts"`
	FromPlanHash string    `json:"from_plan_hash"`
	ToPlanHash   string    `json:"to_plan_hash"`
}

type planStatsResponse struct {
	Plans   []planStatJSON   `json:"plans"`
	Changes []planChangeJSON `json:"changes"`
}

// handlePlanStats returns per-plan duration statistics of one statement and
// the times its plan changed (plan 007 9.2). Plans are newest-seen first,
// changes newest first, at most maxPlanChanges.
func (a *API) handlePlanStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	instanceID, queryID, from, to, ok := planSampleScope(w, q)
	if !ok {
		return
	}
	out := planStatsResponse{Plans: []planStatJSON{}, Changes: []planChangeJSON{}}
	if a.pool == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	where := `tenant_id='default' AND instance_id=$1 AND queryid=$2 AND ts >= $3 AND ts < $4`
	args := []any{instanceID, queryID, from, to}
	planWhere := `p.tenant_id='default' AND p.instance_id=$1 AND p.queryid=$2 AND p.plan_hash=s.plan_hash`
	if db := q.Get("database"); db != "" {
		args = append(args, db)
		where += fmt.Sprintf(" AND datname=$%d", len(args))
		planWhere += fmt.Sprintf(" AND p.datname=$%d", len(args))
	}
	statsSQL := `SELECT s.plan_hash, count(*), avg(s.duration_ms),
  percentile_cont(0.95) WITHIN GROUP (ORDER BY s.duration_ms),
  min(s.duration_ms), max(s.duration_ms), min(s.ts), max(s.ts),
  count(*) FILTER (WHERE s.analyzed),
  EXISTS (SELECT 1 FROM query_plans p WHERE ` + planWhere + `)
FROM plan_samples s WHERE ` + where + `
GROUP BY s.plan_hash ORDER BY max(s.ts) DESC, s.plan_hash`
	rows, err := a.pool.Query(r.Context(), statsSQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan stats query failed", err.Error())
		return
	}
	for rows.Next() {
		var p planStatJSON
		if err := rows.Scan(&p.PlanHash, &p.Samples, &p.AvgMS, &p.P95MS, &p.MinMS, &p.MaxMS, &p.FirstTS, &p.LastTS, &p.AnalyzedSamples, &p.HasPlan); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "plan stats decode failed", err.Error())
			return
		}
		out.Plans = append(out.Plans, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "plan stats read failed", err.Error())
		return
	}
	changesSQL := fmt.Sprintf(`SELECT ts, prev, plan_hash FROM (
  SELECT ts, plan_hash, lag(plan_hash) OVER (PARTITION BY datname ORDER BY ts) AS prev
  FROM plan_samples WHERE %s
) c WHERE prev IS NOT NULL AND prev <> plan_hash ORDER BY ts DESC LIMIT %d`, where, maxPlanChanges)
	rows, err = a.pool.Query(r.Context(), changesSQL, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "plan changes query failed", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var c planChangeJSON
		if err := rows.Scan(&c.TS, &c.FromPlanHash, &c.ToPlanHash); err != nil {
			writeError(w, http.StatusInternalServerError, "plan changes decode failed", err.Error())
			return
		}
		out.Changes = append(out.Changes, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "plan changes read failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
