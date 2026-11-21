package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// purgePlanParametersKind is the command_audit kind of a parameter purge; the
// table has no CHECK on kind (migration 0010).
const purgePlanParametersKind = "purge_plan_parameters"

func (a *API) registerPlanSampleRoutes(r chi.Router) {
	r.Delete("/api/v1/plans/samples/parameters", a.handlePurgePlanParameters)
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
