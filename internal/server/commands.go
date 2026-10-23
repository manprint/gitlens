package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	// commandClaimLease is how long the agent that claimed a command has to
	// report its result. The agent runs a command for at most 60 seconds, and
	// the rest covers the capability refresh and the result post. The TTL
	// bounds the wait for a claim; it must not expire a command being run.
	commandClaimLease = 2 * time.Minute
	// maxCommandBody bounds a command request. A result is larger, since it
	// carries a plan, and is bounded by command.MaxResultBytes instead.
	maxCommandBody int64 = 1 << 20
	// commandQueueLimit bounds the commands waiting for one agent. The agent
	// claims them in order and runs one at a time, and a pgstattuple can hold
	// it for 50 seconds, so an unbounded queue lets a burst expire the commands
	// behind it before they run. A command over the limit is refused at once.
	commandQueueLimit = 20
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

// decodeJSON decodes a request body of at most maxBytes. A body over the
// limit is reported as such, not as invalid JSON: a result too large to store
// must read as a size problem, not as a malformed request.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large", fmt.Sprintf("request body exceeds %d bytes", maxBytes))
			return false
		}
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
	if !decodeJSON(w, r, &req, maxCommandBody) {
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
	// The claim serves an agent's queue across all its instances, first in
	// first out, so the bound is per agent. Two enqueues in flight can both
	// pass the count, which overshoots the limit by at most that many: the
	// bound is on the damage a flood can do, not an exact count.
	var pending int64
	err = s.pool.QueryRow(r.Context(), `SELECT count(*) FROM commands WHERE tenant_id=$1 AND agent_id=$2 AND state='pending' AND expires_at > now()`, tenantID, agentID).Scan(&pending)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "command queue check failed", err.Error())
		return
	}
	if pending >= commandQueueLimit {
		writeError(w, http.StatusTooManyRequests, "command queue full", fmt.Sprintf("the agent already has %d pending commands; retry once they run or expire", commandQueueLimit))
		return
	}
	commandID := uuid.New()
	expiresAt := s.now().UTC().Add(s.ttl)
	// The request is audited in the same statement. Only results used to
	// be: a command that never reached the agent left no trace, and
	// requested_by was always the column default.
	_, err = s.pool.Exec(r.Context(), `WITH cmd AS (
		INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at, requested_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',$8,$9)
		RETURNING tenant_id, command_id, instance_id, kind, args, requested_by)
		INSERT INTO command_audit (tenant_id, command_id, instance_id, kind, args, requested_by, outcome)
		SELECT tenant_id, command_id, instance_id, kind, args, requested_by, 'requested' FROM cmd`,
		commandID, tenantID, agentID, instanceID, clusterID, req.Kind, argsJSON, expiresAt, requesterOf(r, s.auth))
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
	// Expire first: a command whose agent stopped polling used to read as
	// pending long after its expiry, because only a poll expired commands.
	if err := expireCommands(r.Context(), s.pool); err != nil {
		writeError(w, http.StatusInternalServerError, "command expiry failed", err.Error())
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
	agentID, err := uuid.Parse(requestedID)
	if err != nil {
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
	// A revoked agent is given no commands, as its pushes are refused. The
	// claim repeats this check, so a revocation during the wait holds too.
	revoked, err := agentRevoked(r.Context(), s.pool, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent lookup failed", err.Error())
		return
	}
	if revoked {
		writeError(w, http.StatusUnauthorized, "revoked", "agent is revoked and receives no commands")
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

// agentRevoked reports whether agents.revoked_at is set for the agent. An
// agent that is not registered is not revoked.
func agentRevoked(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, agentID uuid.UUID) (bool, error) {
	var revoked bool
	err := q.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM agents WHERE agent_id=$1`, agentID).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return revoked, err
}

func (s *CommandService) claim(ctx context.Context, agentID string) (pollResponse, error) {
	var out pollResponse
	parsed, err := uuid.Parse(agentID)
	if err != nil {
		return out, err
	}
	if err := expireCommands(ctx, s.pool); err != nil {
		return out, err
	}
	// The claim takes its lease in the same statement, so the TTL the command
	// was queued with no longer expires it. GREATEST keeps a longer expiry.
	err = s.pool.QueryRow(ctx, `UPDATE commands
		SET state='claimed', claimed_at=now(), claim_token=gen_random_uuid(), expires_at=GREATEST(expires_at, $3)
		WHERE command_id = (
			SELECT command_id FROM commands
			WHERE tenant_id=$1 AND agent_id=$2 AND state='pending' AND expires_at > now()
			  AND NOT EXISTS (SELECT 1 FROM agents WHERE agent_id=$4 AND revoked_at IS NOT NULL)
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING command_id, kind, args, claim_token, instance_id, cluster_id, expires_at`, commandTenant, parsed, s.now().UTC().Add(commandClaimLease), parsed).Scan(&out.CommandID, &out.Kind, &out.Args, &out.ClaimToken, &out.InstanceID, &out.ClusterID, &out.ExpiresAt)
	return out, err
}

// expireCommands moves every command past its expiry to expired and audits
// each one, saying whether the agent had claimed it. Concurrent calls do not
// audit a command twice: the second UPDATE finds it expired already.
func expireCommands(ctx context.Context, db dbPool) error {
	_, err := db.Exec(ctx, `WITH expired AS (
		UPDATE commands SET state='expired', finished_at=now()
		 WHERE tenant_id=$1 AND state IN ('pending','claimed') AND expires_at <= now()
		RETURNING tenant_id, command_id, instance_id, kind, args, requested_by, claimed_at)
		INSERT INTO command_audit (tenant_id, command_id, instance_id, kind, args, requested_by, outcome, detail)
		SELECT tenant_id, command_id, instance_id, kind, args, requested_by, 'expired',
		       CASE WHEN claimed_at IS NULL THEN 'expired before the agent claimed it'
		            ELSE 'claimed by the agent, no result before expiry' END
		  FROM expired`, commandTenant)
	return err
}

// requesterOf names who enqueued a command for the audit: the credential
// kind (the agent bearer token or a UI session, identified by a digest of
// the session token, never the token itself) and the client address. The
// server has no user accounts, so this is as far as the identity goes.
func requesterOf(r *http.Request, auth *Auth) string {
	who := "api-token"
	if auth == nil || !auth.Validate(r) {
		who = "ui-session"
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			sum := sha256.Sum256([]byte(cookie.Value))
			who += " " + hex.EncodeToString(sum[:4])
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		who += " from " + host
	}
	return who
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
	if !decodeJSON(w, r, &req, command.MaxResultBytes) {
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
	// A result from a revoked agent is refused, even for a claim it held before
	// the revocation. The agent is the one that queued the command, not a header.
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT a.revoked_at IS NOT NULL FROM commands c JOIN agents a ON a.agent_id::text = c.agent_id WHERE c.command_id=$1`, commandID).Scan(&revoked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "command agent lookup failed", err.Error())
		return
	}
	if revoked {
		writeError(w, http.StatusUnauthorized, "revoked", "agent is revoked and its results are refused")
		return
	}
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
		var claimToken *uuid.UUID
		err = tx.QueryRow(ctx, `SELECT tenant_id, instance_id, kind, args, requested_by, state, claim_token FROM commands WHERE command_id=$1 FOR UPDATE`, commandID).Scan(&tenantID, &instanceID, &kind, &args, &requestedBy, &commandState, &claimToken)
		if err == nil && commandState == "expired" {
			// The sweep keeps the claim token of a claim it expires, so a
			// matching token shows the result came from the agent that claimed
			// the command. The result is still refused, but the audit records
			// what that agent reported, not a rejection it never sent.
			outcome := "rejected"
			if claimToken != nil && *claimToken == req.ClaimToken {
				outcome = req.Outcome
			}
			if _, auditErr := tx.Exec(ctx, `INSERT INTO command_audit (tenant_id, command_id, instance_id, kind, args, requested_by, outcome, detail) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID, commandID, instanceID, kind, args, requestedBy, outcome, nullableText(req.Error)); auditErr != nil {
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
			Datname  string          `json:"datname"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return fmt.Errorf("decode explain result: %w", err)
		}
		if len(payload.Plan) == 0 || payload.PlanHash == "" || args.QueryID == nil {
			return nil
		}
		// A plan is only meaningful under its database, and the command often
		// names none. The agent reports the database it ran in, which is the one
		// to file under. An agent that predates that report sends no name, and
		// its plan is filed under the empty name, as plans were before the
		// report existed. A database filter does not find such a row, but the
		// unfiltered history does, so the plan is kept rather than dropped.
		datname := args.Datname
		if datname == "" {
			datname = payload.Datname
		}
		return store.WriteQueryPlan(ctx, tx, store.QueryPlanRow{TenantID: tenantID, InstanceID: instanceID, ClusterID: clusterID, Datname: datname, QueryID: *args.QueryID, PlanHash: payload.PlanHash, Analyzed: args.Analyze, CapturedAt: capturedAt.UTC(), Plan: payload.Plan})
	case command.KindPgstattuple:
		var payload struct {
			Datname      string `json:"datname"`
			TableLen     int64  `json:"table_len"`
			DeadTupleLen int64  `json:"dead_tuple_len"`
			FreeSpace    int64  `json:"free_space"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			return fmt.Errorf("decode pgstattuple result: %w", err)
		}
		// Bloat is the reclaimable space, dead tuples plus free space, and
		// the ratio is its share of the table, as the estimate defines them
		// both (bloat_ratio = bloat_bytes / real_bytes). The row stored
		// table_len - tuple_len (page overhead included) next to a ratio of
		// free space alone, two numbers that did not describe each other.
		bloat := max(payload.DeadTupleLen+payload.FreeSpace, 0)
		expected := payload.TableLen - bloat
		var ratio float64
		if payload.TableLen > 0 {
			ratio = float64(bloat) / float64(payload.TableLen)
		}
		datname := args.Datname
		if datname == "" {
			datname = payload.Datname
		}
		// The agent measures ordinary tables only (see PgstattupleExecutor), so the
		// kind is always table.
		return store.WriteBloat(ctx, tx, []store.BloatRow{{TS: capturedAt.UTC(), TenantID: tenantID, ClusterID: clusterID, InstanceID: instanceID, Datname: datname, Schemaname: args.Schema, Relname: args.Relation, ObjectKind: "table", Method: "pgstattuple", RealBytes: &payload.TableLen, ExpectedBytes: &expected, BloatBytes: &bloat, BloatRatio: &ratio}})
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
