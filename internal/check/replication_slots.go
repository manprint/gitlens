package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&replicationSlotsCheck{}) }

type replicationSlotsCheck struct{}

func (c *replicationSlotsCheck) Name() string { return "replication_slots" }
func (c *replicationSlotsCheck) Requires() Requirements {
	return Requirements{
		Scope:    ScopeInstance,
		PermTier: pgtype.TierReadOnly,
	}
}
func (c *replicationSlotsCheck) DefaultInterval() time.Duration { return 15 * time.Second }
func (c *replicationSlotsCheck) Timeout() time.Duration         { return 2 * time.Second }

type replicationSlotsRow struct {
	SlotName      string
	SlotType      string
	Active        bool
	ActivePID     int
	WALStatus     string
	SafeWALSize   int64
	RetainedBytes int64
}

func (c *replicationSlotsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()

	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, `
SELECT slot_name,
       slot_type,
       active,
       COALESCE(active_pid, 0)                    AS active_pid,
       COALESCE(wal_status, 'unknown')           AS wal_status,
       COALESCE(safe_wal_size, 0)                AS safe_wal_size,
       COALESCE(pg_wal_lsn_diff(
         CASE WHEN pg_is_in_recovery()
              THEN pg_last_wal_replay_lsn()
              ELSE pg_current_wal_lsn() END,
         restart_lsn)::bigint, 0)                AS retained_bytes
FROM pg_replication_slots
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	var metrics []pgtype.Metric
	for rows.Next() {
		var row replicationSlotsRow
		if err := rows.Scan(
			&row.SlotName,
			&row.SlotType,
			&row.Active,
			&row.ActivePID,
			&row.WALStatus,
			&row.SafeWALSize,
			&row.RetainedBytes,
		); err != nil {
			return Result{}, err
		}

		labels := map[string]string{
			"slot_name": row.SlotName,
			"slot_type": row.SlotType,
			"status":    row.WALStatus,
		}

		active := 0.0
		if row.Active {
			active = 1.0
		}

		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_slot_active",
			Value:  active,
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_slot_active_pid",
			Value:  float64(row.ActivePID),
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_slot_safe_wal_size_bytes",
			Value:  float64(row.SafeWALSize),
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_slot_retained_bytes",
			Value:  float64(row.RetainedBytes),
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
	}

	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	return Result{Metrics: metrics}, nil
}
