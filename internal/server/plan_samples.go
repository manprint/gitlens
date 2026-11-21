package server

import (
	"encoding/json"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/store"
)

const (
	planFactKind       = "plan"
	planSourceAuto     = "auto_explain"
	planRejectedSecret = "plan_credential"
)

// autoPlan is what one auto_explain plan fact stores: always a sample, and the
// plan shape when the agent shipped its body.
type autoPlan struct {
	sample store.PlanSampleRow
	plan   *store.QueryPlanRow
}

// autoPlanFromFact maps a plan fact the agent's log reader shipped (plan 007
// 7.2). skip is true for a plan fact of another source, which the pipeline
// ignores as before; reason is set when the fact is rejected.
func autoPlanFromFact(labels map[string]string, value json.RawMessage, tenantID string, clusterID int64, instanceID uuid.UUID) (p autoPlan, skip bool, reason string) {
	if labels["source"] != planSourceAuto {
		return autoPlan{}, true, ""
	}
	ts, err := time.Parse(time.RFC3339Nano, labels["ts"])
	if err != nil {
		return autoPlan{}, false, "invalid_fact"
	}
	queryID, err := strconv.ParseInt(labels["queryid"], 10, 64)
	if err != nil {
		return autoPlan{}, false, "invalid_fact"
	}
	duration, err := strconv.ParseFloat(labels["duration_ms"], 64)
	if err != nil || duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return autoPlan{}, false, "invalid_fact"
	}
	hash := labels["plan_hash"]
	if hash == "" {
		return autoPlan{}, false, "invalid_fact"
	}
	var body struct {
		Plan json.RawMessage `json:"plan"`
	}
	if len(value) > 0 {
		if err := json.Unmarshal(value, &body); err != nil {
			return autoPlan{}, false, "invalid_fact"
		}
	}
	analyzed := labels["analyzed"] == "true"
	ts = ts.UTC()
	p.sample = store.PlanSampleRow{
		TS: ts, TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID,
		Datname: labels["datname"], QueryID: queryID, PlanHash: hash, DurationMS: duration, Analyzed: analyzed,
	}
	if len(body.Plan) > 0 && string(body.Plan) != "null" {
		// The agent checks first; the server checks again and drops the whole
		// fact on a hit (I-4).
		if redact.MayHoldCredential(string(body.Plan)) {
			return autoPlan{}, false, planRejectedSecret
		}
		p.plan = &store.QueryPlanRow{
			TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID, Datname: p.sample.Datname,
			QueryID: queryID, PlanHash: hash, Analyzed: analyzed, CapturedAt: ts, Plan: body.Plan, Source: planSourceAuto,
		}
	}
	return p, false, ""
}
