package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&replicationReceiverCheck{}) }

type replicationReceiverCheck struct{}

func (c *replicationReceiverCheck) Name() string { return "replication_receiver" }
func (c *replicationReceiverCheck) Requires() Requirements {
	return Requirements{
		Roles:    []pgtype.Role{pgtype.RoleStandby},
		Scope:    ScopeInstance,
		PermTier: pgtype.TierReadOnly,
	}
}
func (c *replicationReceiverCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *replicationReceiverCheck) Timeout() time.Duration         { return 2 * time.Second }

type replicationReceiverRow struct {
	Status       string
	SenderHost   string
	SenderPort   int
	SlotName     string
	ReceiveLSN   string
	ReplayLSN    string
	ReplayLagSec *float64
}

func (c *replicationReceiverCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()

	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, `
SELECT COALESCE(w.status, 'disconnected')      AS status,
       COALESCE(w.sender_host, '')             AS sender_host,
       COALESCE(w.sender_port, 0)              AS sender_port,
       COALESCE(w.slot_name, '')               AS slot_name,
       pg_last_wal_receive_lsn()::text         AS receive_lsn,
       pg_last_wal_replay_lsn()::text          AS replay_lsn,
       CASE
         WHEN pg_last_wal_receive_lsn() = pg_last_wal_replay_lsn() THEN 0
         ELSE EXTRACT(epoch FROM now() - pg_last_xact_replay_timestamp())
       END                                     AS replay_lag_sec
FROM (SELECT 1) dummy
LEFT JOIN pg_stat_wal_receiver w ON true
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	var metrics []pgtype.Metric
	for rows.Next() {
		var row replicationReceiverRow
		if err := rows.Scan(
			&row.Status,
			&row.SenderHost,
			&row.SenderPort,
			&row.SlotName,
			&row.ReceiveLSN,
			&row.ReplayLSN,
			&row.ReplayLagSec,
		); err != nil {
			return Result{}, err
		}

		labels := map[string]string{
			"status":      row.Status,
			"sender_host": row.SenderHost,
			"slot_name":   row.SlotName,
		}

		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_receiver_status",
			Value:  statusToFloat(row.Status),
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "replication_receiver_sender_port",
			Value:  float64(row.SenderPort),
			Kind:   pgtype.KindGauge,
			Labels: labels,
		})
		if row.ReplayLagSec != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_receiver_replay_lag_seconds",
				Value:  *row.ReplayLagSec,
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	return Result{Metrics: metrics}, nil
}

func statusToFloat(status string) float64 {
	switch status {
	case "streaming":
		return 1
	case "catchup":
		return 2
	case "disconnected":
		return 0
	default:
		return -1
	}
}
