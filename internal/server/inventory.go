package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// Upsert processes all instances in the envelope. Instances are handled
// independently: a per-instance failure increments Rejected and is recorded in
// Errors but does not abort the envelope. It returns an UpsertResult and, only
// on a top-level failure (e.g. context cancellation), a non-nil error.
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

// upsertOne handles a single wire Instance.
func (inv *Inventory) upsertOne(ctx context.Context, tenantID string, agentID uuid.UUID, inst wire.Instance) error {
	// Parse and validate cluster_id.
	cid, err := pgtype.ParseClusterID(inst.ClusterID)
	if err != nil {
		return fmt.Errorf("invalid cluster_id %q: %w", inst.ClusterID, err)
	}
	cidDB := store.ToDB(cid)

	// Parse instance_id.
	instUUID, err := uuid.Parse(inst.InstanceID)
	if err != nil {
		return fmt.Errorf("invalid instance_id %q: %w", inst.InstanceID, err)
	}

	// Validate role – keep as-is but normalize unknown values to "unknown".
	role := inst.Role
	switch role {
	case "primary", "standby", "unknown":
	default:
		role = "unknown"
	}

	// 1. Upsert cluster row (must precede instance FK).
	if err := inv.upsertCluster(ctx, tenantID, cidDB, inst.ClusterID, inst.ClusterIDSource, instUUID); err != nil {
		return err
	}

	// 2. Check existing instance for I-1 immutability and role changes.
	var existingCID int64
	var existingRole string
	err = inv.pool.QueryRow(ctx, `SELECT cluster_id, role FROM instances WHERE instance_id=$1`, instUUID).Scan(&existingCID, &existingRole)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lookup instance %s: %w", instUUID, err)
	}
	if err == nil {
		// Existing instance – enforce cluster_id immutability (I-1).
		if existingCID != cidDB {
			payload := map[string]any{
				"instance_id":          inst.InstanceID,
				"existing_cluster_id":  pgtype.ClusterID(uint64(existingCID)).String(),
				"attempted_cluster_id": inst.ClusterID,
			}
			if emitErr := inv.emitEvent(ctx, tenantID, time.Now().UTC(), "cluster_id_changed", &existingCID, &instUUID, payload); emitErr != nil {
				return fmt.Errorf("emit cluster_id_changed: %w", emitErr)
			}
			return fmt.Errorf("cluster_id_changed for instance %s: existing %s attempted %s", inst.InstanceID, pgtype.ClusterID(uint64(existingCID)).String(), inst.ClusterID)
		}
		// Role change detection – only primary<->standby transitions.
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
		if emitErr := inv.emitEvent(ctx, tenantID, time.Now().UTC(), "cluster_identity_conflict", &cidDB, &instID, payload); emitErr != nil {
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
