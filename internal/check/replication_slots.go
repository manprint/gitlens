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
	SafeWALSize   *int64
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
       CASE WHEN restart_lsn IS NOT NULL THEN safe_wal_size END AS safe_wal_size,
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
		// safe_wal_size is NULL when max_slot_wal_keep_size is -1 (no
		// limit) and for a slot already lost. It used to be reported as 0,
		// which reads as "no margin left" for the healthiest configuration.
		// A slot that has not reserved WAL yet has no restart_lsn, and the
		// server still computes a size for it against a position the slot
		// does not hold. That size goes negative as WAL is written, so the
		// query blanks it for such a slot too.
		if row.SafeWALSize != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_slot_safe_wal_size_bytes",
				Value:  float64(*row.SafeWALSize),
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
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
