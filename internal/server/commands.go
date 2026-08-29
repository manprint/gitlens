package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/store"
)

const (
	defaultCommandTTL    = 5 * time.Minute
	commandAgentIDHeader = "X-PGLENS-Agent-ID"
	commandWaitInterval  = 500 * time.Millisecond
	maxCommandWait       = 30 * time.Second
	commandTenant        = "default"
)

// CommandService owns the server side of the command queue.
type CommandService struct {
	pool  dbPool
	auth  *Auth
	ttl   time.Duration
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// NewCommandService creates the command queue service with the configured
// command TTL. An invalid environment value falls back to the safe default.
func NewCommandService(pool dbPool, auth *Auth) *CommandService {
	return &CommandService{
		pool:  pool,
		auth:  auth,
		ttl:   commandTTL(),
		now:   time.Now,
		sleep: sleepContext,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func commandTTL() time.Duration {
	if raw := strings.TrimSpace(getenv("PGLENS_COMMAND_TTL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	return defaultCommandTTL
}

var getenv = os.Getenv

func parseCommandWait(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid wait %q: %w", raw, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("wait must not be negative")
	}
	if d > maxCommandWait {
		return maxCommandWait, nil
	}
	return d, nil
}

func (s *CommandService) RegisterRoutes(r chi.Router) {
	r.Post("/api/v1/instances/{id}/commands", s.enqueue)
	r.Get("/api/v1/commands/{id}", s.get)
	r.Get("/api/v1/agents/{agent_id}/commands", s.poll)
	r.Post("/api/v1/commands/{id}/result", s.result)
}

type enqueueRequest struct {
	Kind command.Kind    `json:"kind"`
	Args json.RawMessage `json:"args"`
}

type commandResponse struct {
	CommandID uuid.UUID       `json:"command_id"`
	State     string          `json:"state"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type pollResponse struct {
	CommandID  uuid.UUID    `json:"command_id"`
	Kind       command.Kind `json:"kind"`
	Args       command.Args `json:"args"`
	InstanceID uuid.UUID    `json:"instance_id"`
	ClusterID  int64        `json:"cluster_id"`
	ClaimToken uuid.UUID    `json:"claim_token"`
	ExpiresAt  time.Time    `json:"expires_at"`
}

type resultRequest struct {
	ClaimToken uuid.UUID       `json:"claim_token"`
	Outcome    string          `json:"outcome"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON", err.Error())
		return false
	}
	return true
}

func (s *CommandService) enqueue(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "commands unavailable", "database pool is not configured")
		return
	}
	instanceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance id", err.Error())
		return
	}
	var req enqueueRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var args command.Args
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			writeError(w, http.StatusBadRequest, "invalid command args", err.Error())
			return
		}
	}
	if err := args.Validate(req.Kind); err != nil {
		writeError(w, http.StatusBadRequest, "invalid command", err.Error())
		return
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid command args", err.Error())
		return
	}
	var agentID uuid.UUID
	var clusterID int64
	var tenantID string
	err = s.pool.QueryRow(r.Context(), `SELECT agent_id, cluster_id, tenant_id FROM instances WHERE instance_id=$1`, instanceID).Scan(&agentID, &clusterID, &tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "instance not found", instanceID.String())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "instance lookup failed", err.Error())
		return
	}
	commandID := uuid.New()
	expiresAt := s.now().UTC().Add(s.ttl)
	_, err = s.pool.Exec(r.Context(), `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',$8)`, commandID, tenantID, agentID, instanceID, clusterID, req.Kind, argsJSON, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command enqueue failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]uuid.UUID{"command_id": commandID})
}

func (s *CommandService) get(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "commands unavailable", "database pool is not configured")
		return
	}
	commandID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid command id", err.Error())
		return
	}
	var out commandResponse
	var result []byte
	var errorText *string
	err = s.pool.QueryRow(r.Context(), `SELECT state, result, error FROM commands WHERE command_id=$1`, commandID).Scan(&out.State, &result, &errorText)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "command not found", commandID.String())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command lookup failed", err.Error())
		return
	}
	out.CommandID = commandID
	out.Result = json.RawMessage(result)
	if errorText != nil {
		out.Error = *errorText
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *CommandService) authenticatedAgent(r *http.Request) (string, bool) {
	if s.auth == nil || !s.auth.Validate(r) {
		return "", false
	}
	agentID := r.Header.Get(commandAgentIDHeader)
	if agentID == "" {
		agentID = r.Header.Get("X-Agent-ID")
	}
	return agentID, agentID != ""
}

func (s *CommandService) poll(w http.ResponseWriter, r *http.Request) {
	authenticatedID, ok := s.authenticatedAgent(r)
	if !ok {
		if s.auth == nil || !s.auth.Validate(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "agent authentication failed")
		} else {
			writeError(w, http.StatusForbidden, "forbidden", "agent identity header is required")
		}
		return
	}
	requestedID := chi.URLParam(r, "agent_id")
	if authenticatedID != requestedID {
		writeError(w, http.StatusForbidden, "forbidden", "agent may only poll its own queue")
		return
	}
	if _, err := uuid.Parse(requestedID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id", err.Error())
		return
	}
	wait, err := parseCommandWait(r.URL.Query().Get("wait"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid wait", err.Error())
		return
	}
	if s.pool == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	deadline := s.now().Add(wait)
	for {
		claimed, claimErr := s.claim(r.Context(), requestedID)
		if claimErr != nil {
			if errors.Is(claimErr, pgx.ErrNoRows) {
				if wait == 0 || !s.now().Before(deadline) {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if sleepErr := s.sleep(r.Context(), commandWaitInterval); sleepErr != nil {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				continue
			}
			writeError(w, http.StatusInternalServerError, "command poll failed", claimErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, claimed)
		return
	}
}

func (s *CommandService) claim(ctx context.Context, agentID string) (pollResponse, error) {
	var out pollResponse
	parsed, err := uuid.Parse(agentID)
	if err != nil {
		return out, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE commands SET state='expired', finished_at=now() WHERE tenant_id=$1 AND state IN ('pending','claimed') AND expires_at <= now()`, commandTenant); err != nil {
		return out, err
	}
	err = s.pool.QueryRow(ctx, `UPDATE commands
		SET state='claimed', claimed_at=now(), claim_token=gen_random_uuid()
		WHERE command_id = (
			SELECT command_id FROM commands
			WHERE tenant_id=$1 AND agent_id=$2 AND state='pending' AND expires_at > now()
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING command_id, kind, args, claim_token, instance_id, cluster_id, expires_at`, commandTenant, parsed).Scan(&out.CommandID, &out.Kind, &out.Args, &out.ClaimToken, &out.InstanceID, &out.ClusterID, &out.ExpiresAt)
	return out, err
}

func (s *CommandService) result(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil || !s.auth.Validate(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "agent authentication failed")
		return
	}
	if s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "commands unavailable", "database pool is not configured")
		return
	}
	commandID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid command id", err.Error())
		return
	}
	var req resultRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateResult(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid command result", err.Error())
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command result transaction failed", err.Error())
		return
	}
	ctx := r.Context()
	defer func() { _ = tx.Rollback(ctx) }()
	state := "failed"
	if req.Outcome == "ok" {
		state = "done"
	}
	var tenantID string
	var instanceID uuid.UUID
	var kind command.Kind
	var args []byte
	var clusterID int64
	var requestedBy string
	err = tx.QueryRow(ctx, `UPDATE commands SET state=$3, result=$4, error=$5, finished_at=now() WHERE command_id=$1 AND state='claimed' AND claim_token=$2 RETURNING tenant_id, instance_id, cluster_id, kind, args, requested_by`, commandID, req.ClaimToken, state, nullableJSON(req.Result), nullableText(req.Error)).Scan(&tenantID, &instanceID, &clusterID, &kind, &args, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		var tenantID, commandState string
		err = tx.QueryRow(ctx, `SELECT tenant_id, instance_id, kind, args, requested_by, state FROM commands WHERE command_id=$1 FOR UPDATE`, commandID).Scan(&tenantID, &instanceID, &kind, &args, &requestedBy, &commandState)
		if err == nil && commandState == "expired" {
			if _, auditErr := tx.Exec(ctx, `INSERT INTO command_audit (tenant_id, command_id, instance_id, kind, args, requested_by, outcome, detail) VALUES ($1,$2,$3,$4,$5,$6,'rejected',$7)`, tenantID, commandID, instanceID, kind, args, requestedBy, nullableText(req.Error)); auditErr != nil {
				writeError(w, http.StatusInternalServerError, "command audit failed", auditErr.Error())
				return
			}
			if commitErr := tx.Commit(ctx); commitErr != nil {
				writeError(w, http.StatusInternalServerError, "command result commit failed", commitErr.Error())
				return
			}
			writeError(w, http.StatusConflict, "claim conflict", "command expired before the result was accepted")
			return
		}
		writeError(w, http.StatusConflict, "claim conflict", "claim token is missing, mismatched, or command is no longer claimed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command result update failed", err.Error())
		return
	}
	if req.Outcome == "ok" {
		if err := persistCommandArtifact(ctx, tx, tenantID, instanceID, clusterID, kind, args, req.Result, s.now()); err != nil {
			writeError(w, http.StatusInternalServerError, "command artifact failed", err.Error())
			return
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO command_audit (tenant_id, command_id, instance_id, kind, args, requested_by, outcome, detail) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, commandID, instanceID, kind, args, requestedBy, req.Outcome, nullableText(req.Error)); err != nil {
		writeError(w, http.StatusInternalServerError, "command audit failed", err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "command result commit failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func persistCommandArtifact(ctx context.Context, tx pgx.Tx, tenantID string, instanceID uuid.UUID, clusterID int64, kind command.Kind, argsJSON, result json.RawMessage, capturedAt time.Time) error {
	var args command.Args
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		return fmt.Errorf("decode command args: %w", err)
	}
	switch kind {
	case command.KindExplain:
		var payload struct {
			Plan     json.RawMessage `json:"plan"`
			PlanHash string          `json:"plan_hash"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return fmt.Errorf("decode explain result: %w", err)
		}
		if len(payload.Plan) == 0 || payload.PlanHash == "" || args.QueryID == nil {
			return nil
		}
		return store.WriteQueryPlan(ctx, tx, store.QueryPlanRow{TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID, Datname: args.Datname, QueryID: *args.QueryID, PlanHash: payload.PlanHash, Analyzed: args.Analyze, CapturedAt: capturedAt.UTC(), Plan: payload.Plan})
	case command.KindPgstattuple:
		var payload struct {
			TableLen    int64   `json:"table_len"`
			TupleLen    int64   `json:"tuple_len"`
			FreePercent float64 `json:"free_percent"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return fmt.Errorf("decode pgstattuple result: %w", err)
		}
		bloat := payload.TableLen - payload.TupleLen
		ratio := payload.FreePercent / 100
		return store.WriteBloat(ctx, tx, []store.BloatRow{{TS: capturedAt.UTC(), TenantID: tenantID, ClusterID: clusterID, InstanceID: instanceID, Datname: args.Datname, Schemaname: args.Schema, Relname: args.Relation, ObjectKind: "table", Method: "pgstattuple", RealBytes: &payload.TableLen, ExpectedBytes: &payload.TupleLen, BloatBytes: &bloat, BloatRatio: &ratio}})
	default:
		return nil
	}
}

func validateResult(req resultRequest) error {
	if req.ClaimToken == uuid.Nil {
		return errors.New("claim_token is required")
	}
	if req.Outcome != "ok" && req.Outcome != "error" && req.Outcome != "rejected" {
		return fmt.Errorf("unknown outcome %q", req.Outcome)
	}
	if req.Outcome == "ok" && req.Error != "" {
		return errors.New("ok result cannot include error")
	}
	if req.Outcome != "ok" && len(req.Result) > 0 {
		return errors.New("failed result cannot include result")
	}
	if req.Outcome != "ok" && req.Error == "" {
		return errors.New("failed result requires error")
	}
	return nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
