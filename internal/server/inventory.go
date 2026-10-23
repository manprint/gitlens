package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// Inventory handles identity resolution and inventory upsert (I-1).
// It materializes clusters, instances and databases from the wire envelope,
// enforces immutability of cluster_id per instance, and emits events for
// role changes, identity conflicts, cluster moves and duplicate instances.
type Inventory struct {
	pool dbPool
}

// NewInventory creates an Inventory backed by the given pool.
// Pool may be nil in tests that exercise only pure-function helpers.
func NewInventory(pool *pgxpool.Pool) *Inventory {
	return &Inventory{pool: asDBPool(pool)}
}

// UpsertResult reports per-instance acceptance.
type UpsertResult struct {
	Accepted int
	Rejected int
	// Errors maps instance_id string to the rejection reason.
	Errors map[string]error
}

// instanceRejected is the inventory refusing an instance on its own terms: an
// identifier it cannot read, or a cluster_id other than the one the instance
// already has. Only the instance is refused; the rest of the envelope goes on.
type instanceRejected struct{ err error }

func (r instanceRejected) Error() string { return r.err.Error() }
func (r instanceRejected) Unwrap() error { return r.err }

// storeFailure reports whether err says the database could not do the work
// just now, rather than that the instance is refused. The envelope is worth
// sending again after a store failure. A refusal is not: the same instance
// would be refused on every push, and the agent would keep sending it.
func storeFailure(err error) bool {
	var rejected instanceRejected
	if errors.As(err, &rejected) {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		// No answer from the server: a refused connection, a dropped one, or
		// a context that ended.
		return true
	}
	// Connection exceptions, transaction rollbacks (serialization, deadlock),
	// insufficient resources and operator interventions go away on their own.
	// A constraint or data error is about this instance, and would repeat.
	for _, class := range []string{"08", "40", "53", "57"} {
		if strings.HasPrefix(pgErr.Code, class) {
			return true
		}
	}
	return false
}

// Upsert processes all instances in the envelope. Instances are handled
// independently: an instance the inventory refuses increments Rejected and is
// recorded in Errors, and the rest of the envelope goes on. A store failure is
// not a refusal, so it stops the envelope and is returned as an error: the
// caller answers it so the agent sends the envelope again.
func (inv *Inventory) Upsert(ctx context.Context, env wire.Envelope) (*UpsertResult, error) {
	res := &UpsertResult{Errors: make(map[string]error)}
	if inv.pool == nil {
		res.Accepted = len(env.Instances)
		return res, nil
	}
	tenantID := "default"

	// Parse agent_id once for use as instances.agent_id when inserting.
	var agentUUID uuid.UUID
	if env.AgentID != "" {
		if parsed, err := uuid.Parse(env.AgentID); err == nil {
			agentUUID = parsed
		}
	}

	for _, inst := range env.Instances {
		if err := inv.upsertOne(ctx, tenantID, agentUUID, inst); err != nil {
			if storeFailure(err) {
				return nil, err
			}
			res.Rejected++
			res.Errors[inst.InstanceID] = err
			continue
		}
		res.Accepted++
	}
	return res, nil
}

// IsRevoked reports whether agentID has a non-null agents.revoked_at, or
// false if the agent is unknown (a brand-new agent's first push should not
// be rejected). A nil pool (unit tests exercising validation only) never
// reports revoked, matching Upsert's own nil-pool tolerance.
func (inv *Inventory) IsRevoked(ctx context.Context, agentID string) (bool, error) {
	if inv.pool == nil || agentID == "" {
		return false, nil
	}
	parsed, parseErr := uuid.Parse(agentID)
	if parseErr != nil {
		return false, nil //nolint:nilerr // a malformed agent_id is treated as unknown, not an error worth rejecting the push over
	}
	var revokedAt *time.Time
	err := inv.pool.QueryRow(ctx, `SELECT revoked_at FROM agents WHERE agent_id=$1`, parsed).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query agent revoked_at: %w", err)
	}
	return revokedAt != nil, nil
}

// OwnerRevoked reports whether the agent recorded as the owner of any of the
// instances has a non-null agents.revoked_at. The envelope's agent_id is
// self-reported under the shared bootstrap token, so a revoked host can leave
// it out or name another agent; the owner recorded for an instance is what a
// revocation applies to. Instances that are not yet known have no owner to
// check. A nil pool never reports revoked, like IsRevoked.
func (inv *Inventory) OwnerRevoked(ctx context.Context, instances []wire.Instance) (bool, error) {
	if inv.pool == nil {
		return false, nil
	}
	ids := make([]uuid.UUID, 0, len(instances))
	for _, inst := range instances {
		if id, err := uuid.Parse(inst.InstanceID); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	var revoked bool
	err := inv.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM instances i JOIN agents a ON a.agent_id=i.agent_id WHERE i.instance_id = ANY($1) AND a.revoked_at IS NOT NULL)`, ids).Scan(&revoked)
	if err != nil {
		return false, fmt.Errorf("query instance owner revoked_at: %w", err)
	}
	return revoked, nil
}

// upsertOne handles a single wire Instance.
func (inv *Inventory) upsertOne(ctx context.Context, tenantID string, agentID uuid.UUID, inst wire.Instance) error {
	// Parse and validate cluster_id.
	cid, err := pgtype.ParseClusterID(inst.ClusterID)
	if err != nil {
		return instanceRejected{fmt.Errorf("invalid cluster_id %q: %w", inst.ClusterID, err)}
	}
	cidDB := store.ToDB(cid)

	// Parse instance_id.
	instUUID, err := uuid.Parse(inst.InstanceID)
	if err != nil {
		return instanceRejected{fmt.Errorf("invalid instance_id %q: %w", inst.InstanceID, err)}
	}

	// Validate role – keep as-is but normalize unknown values to "unknown".
	role := inst.Role
	switch role {
	case "primary", "standby", "unknown":
	default:
		role = "unknown"
	}

	// 1. Look up the instance before anything is written for it. A push that
	// names another cluster than the instance's own is refused before that
	// cluster is created or its identity source is changed, so the refused push
	// leaves nothing behind in clusters.
	var existingCID int64
	var existingRole string
	err = inv.pool.QueryRow(ctx, `SELECT cluster_id, role FROM instances WHERE instance_id=$1`, instUUID).Scan(&existingCID, &existingRole)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup instance %s: %w", instUUID, err)
	}
	// Existing instance – enforce cluster_id immutability (I-1).
	if err == nil && existingCID != cidDB {
		payload := map[string]any{
			"instance_id":          inst.InstanceID,
			"existing_cluster_id":  pgtype.ClusterID(uint64(existingCID)).String(),
			"attempted_cluster_id": inst.ClusterID,
		}
		if emitErr := inv.emitRepeatable(ctx, tenantID, "cluster_id_changed", existingCID, instUUID, payload, "attempted_cluster_id", inst.ClusterID); emitErr != nil {
			return fmt.Errorf("emit cluster_id_changed: %w", emitErr)
		}
		return instanceRejected{fmt.Errorf("cluster_id_changed for instance %s: existing %s attempted %s", inst.InstanceID, pgtype.ClusterID(uint64(existingCID)).String(), inst.ClusterID)}
	}

	// 2. Upsert cluster row (must precede instance FK).
	if err := inv.upsertCluster(ctx, tenantID, cidDB, inst.ClusterID, inst.ClusterIDSource, instUUID); err != nil {
		return err
	}

	if err == nil {
		// Existing instance: role change detection – only primary<->standby transitions.
		if (existingRole == "primary" && role == "standby") || (existingRole == "standby" && role == "primary") {
			payload := map[string]any{
				"instance_id": inst.InstanceID,
				"old_role":    existingRole,
				"new_role":    role,
			}
			if emitErr := inv.emitEvent(ctx, tenantID, time.Now().UTC(), "role_change", &cidDB, &instUUID, payload); emitErr != nil {
				return fmt.Errorf("emit role_change: %w", emitErr)
			}
		}
		// Update mutable fields.
		if _, execErr := inv.pool.Exec(ctx,
			`UPDATE instances SET role=$1, pg_version=$2, perm_tier=$3, addr=$4, port=$5, target_name=$6, last_seen=now(), ash_enabled=$7 WHERE instance_id=$8`,
			role, inst.PGVersion, inst.PermTier, inst.Addr, inst.Port, inst.TargetName, inst.ASHEnabled, instUUID); execErr != nil {
			return fmt.Errorf("update instance %s: %w", inst.InstanceID, execErr)
		}
	} else {
		// New instance – duplicate-instance detection.
		if dupErr := inv.checkDuplicateInstance(ctx, tenantID, cidDB, inst, instUUID); dupErr != nil {
			return dupErr
		}
		// Insert new instance.
		agentParam := agentID
		// If envelope had no valid agent_id, fall back to instance_id as placeholder – the FK requires a valid agents row,
		// but in production auth ensures agents exists. For tests we still need a UUID.
		if agentParam == uuid.Nil {
			agentParam = instUUID
		}
		// Ensure agent row exists for FK (best-effort, ignore conflict).
		if _, err := inv.pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1) ON CONFLICT DO NOTHING`, agentParam); err != nil {
			return fmt.Errorf("ensure agent %s: %w", agentParam, err)
		}
		if _, err := inv.pool.Exec(ctx,
			`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, target_name, last_seen, ash_enabled)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,now(),$11)
			 ON CONFLICT (instance_id) DO UPDATE SET role=EXCLUDED.role, pg_version=EXCLUDED.pg_version, perm_tier=EXCLUDED.perm_tier, addr=EXCLUDED.addr, port=EXCLUDED.port, target_name=EXCLUDED.target_name, last_seen=now(), ash_enabled=EXCLUDED.ash_enabled`,
			instUUID, tenantID, cidDB, agentParam, inst.Addr, inst.Port, inst.PGVersion, role, inst.PermTier, inst.TargetName, inst.ASHEnabled); err != nil {
			return fmt.Errorf("insert instance %s: %w", inst.InstanceID, err)
		}
	}

	// 3. Upsert databases.
	for _, db := range inst.Databases {
		if _, err := inv.pool.Exec(ctx,
			`INSERT INTO databases (instance_id, datname, monitored, skip_reason, last_seen)
			 VALUES ($1,$2,$3,$4,now())
			 ON CONFLICT (instance_id, datname) DO UPDATE SET monitored=EXCLUDED.monitored, skip_reason=EXCLUDED.skip_reason, last_seen=now()`,
			instUUID, db.Name, db.Monitored, nullableString(db.SkipReason)); err != nil {
			return fmt.Errorf("upsert database %s for instance %s: %w", db.Name, inst.InstanceID, err)
		}
	}
	return nil
}

// upsertCluster handles the cluster row with identifier-over-manual priority.
// If a row already exists with system_identifier and the incoming source is
// manual, the existing row is kept and a cluster_identity_conflict event is emitted.
func (inv *Inventory) upsertCluster(ctx context.Context, tenantID string, cidDB int64, cidStr, source string, instID uuid.UUID) error {
	if source != string(pgtype.IDSourceSystemIdentifier) && source != string(pgtype.IDSourceManual) {
		source = string(pgtype.IDSourceManual)
	}
	var existingSource string
	err := inv.pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE tenant_id=$1 AND cluster_id=$2`, tenantID, cidDB).Scan(&existingSource)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup cluster %s: %w", cidStr, err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, execErr := inv.pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, tenantID, cidDB, source); execErr != nil {
			return fmt.Errorf("insert cluster %s: %w", cidStr, execErr)
		}
		return nil
	}
	// Row exists.
	if existingSource == string(pgtype.IDSourceSystemIdentifier) && source == string(pgtype.IDSourceManual) {
		payload := map[string]any{
			"cluster_id":       cidStr,
			"existing_source":  existingSource,
			"attempted_source": source,
			"instance_id":      instID.String(),
		}
		if emitErr := inv.emitRepeatable(ctx, tenantID, "cluster_identity_conflict", cidDB, instID, payload, "attempted_source", source); emitErr != nil {
			return fmt.Errorf("emit cluster_identity_conflict: %w", emitErr)
		}
		// Keep existing row.
		return nil
	}
	if existingSource != source {
		// Identifier outranks manual: allow system_identifier to overwrite manual.
		if existingSource == string(pgtype.IDSourceManual) && source == string(pgtype.IDSourceSystemIdentifier) {
			if _, execErr := inv.pool.Exec(ctx, `UPDATE clusters SET id_source=$1 WHERE tenant_id=$2 AND cluster_id=$3`, source, tenantID, cidDB); execErr != nil {
				return fmt.Errorf("update cluster %s source: %w", cidStr, execErr)
			}
		}
	}
	return nil
}

// checkDuplicateInstance emits duplicate_instance_suspected when another live
// instance shares (tenant_id, cluster_id, addr, port) and was seen within the
// last 5 minutes.
func (inv *Inventory) checkDuplicateInstance(ctx context.Context, tenantID string, cidDB int64, inst wire.Instance, newID uuid.UUID) error {
	rows, err := inv.pool.Query(ctx,
		`SELECT instance_id FROM instances WHERE tenant_id=$1 AND cluster_id=$2 AND addr=$3 AND port=$4 AND instance_id != $5 AND last_seen > now() - interval '5 minutes'`,
		tenantID, cidDB, inst.Addr, inst.Port, newID)
	if err != nil {
		return fmt.Errorf("duplicate check for %s: %w", inst.InstanceID, err)
	}
	defer rows.Close()

	var duplicates []string
	for rows.Next() {
		var existingID uuid.UUID
		if scanErr := rows.Scan(&existingID); scanErr != nil {
			return fmt.Errorf("scan duplicate instance: %w", scanErr)
		}
		duplicates = append(duplicates, existingID.String())
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("iterate duplicate rows: %w", rowsErr)
	}
	if len(duplicates) > 0 {
		payload := map[string]any{
			"new_instance_id":       inst.InstanceID,
			"existing_instance_ids": duplicates,
			"addr":                  inst.Addr,
			"port":                  inst.Port,
			"cluster_id":            inst.ClusterID,
		}
		if emitErr := inv.emitEvent(ctx, tenantID, time.Now().UTC(), "duplicate_instance_suspected", &cidDB, &newID, payload); emitErr != nil {
			return fmt.Errorf("emit duplicate_instance_suspected: %w", emitErr)
		}
	}
	return nil
}

// repeatedEventWindow bounds how often an event that a push repeats is written
// again for the same instance. A misidentified agent pushes every few seconds,
// and the same event on every push says nothing new; a mismatch that stands is
// still reported once a day.
const repeatedEventWindow = 24 * time.Hour

// emitRepeatable writes an event for a refusal that the same instance keeps
// sending, unless the instance already has an event of this type for the same
// attempted value (payload[attemptedKey]) inside repeatedEventWindow. The check
// and the insert are one statement, so two replicas taking the same push at
// once do not both write it.
func (inv *Inventory) emitRepeatable(ctx context.Context, tenantID, typ string, clusterID int64, instanceID uuid.UUID, payload map[string]any, attemptedKey, attempted string) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	ts := time.Now().UTC()
	if _, execErr := inv.pool.Exec(ctx,
		`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload)
		 SELECT $1::text, $2::timestamptz, $3::text, $4::bigint, $5::uuid, $6::jsonb
		 WHERE NOT EXISTS (
		   SELECT 1 FROM events
		   WHERE tenant_id=$1::text AND cluster_id=$4::bigint AND type=$3::text AND instance_id=$5::uuid
		     AND ts > $7::timestamptz AND payload->>$8::text = $9::text)`,
		tenantID, ts, typ, clusterID, instanceID, string(payloadJSON), ts.Add(-repeatedEventWindow), attemptedKey, attempted); execErr != nil {
		return fmt.Errorf("insert event %s: %w", typ, execErr)
	}
	return nil
}

// emitEvent inserts a row into events.
func (inv *Inventory) emitEvent(ctx context.Context, tenantID string, ts time.Time, typ string, clusterID *int64, instanceID *uuid.UUID, payload map[string]any) error {
	var payloadJSON []byte
	var err error
	if payload != nil {
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
	} else {
		payloadJSON = []byte(`{}`)
	}
	// Use non-nil uuid values for nullable column handling.
	var cidParam any
	if clusterID != nil {
		cidParam = *clusterID
	}
	var iidParam any
	if instanceID != nil {
		iidParam = *instanceID
	}
	if _, execErr := inv.pool.Exec(ctx,
		`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
		tenantID, ts, typ, cidParam, iidParam, string(payloadJSON)); execErr != nil {
		return fmt.Errorf("insert event %s: %w", typ, execErr)
	}
	return nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
