package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&replicationStreamingCheck{}) }

type replicationStreamingCheck struct{}

func (c *replicationStreamingCheck) Name() string { return "replication_streaming" }
func (c *replicationStreamingCheck) Requires() Requirements {
	return Requirements{
		Roles:    []pgtype.Role{pgtype.RolePrimary},
		Scope:    ScopeInstance,
		PermTier: pgtype.TierReadOnly,
	}
}
func (c *replicationStreamingCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *replicationStreamingCheck) Timeout() time.Duration         { return 2 * time.Second }

type replicationStreamingRow struct {
	ApplicationName string
	ClientAddr      string
	State           string
	SyncState       string
	WriteLagBytes   *int64
	FlushLagBytes   *int64
	ReplayLagBytes  *int64
	WriteLagSec     *float64
	FlushLagSec     *float64
	ReplayLagSec    *float64
}

func (c *replicationStreamingCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()

	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, `
SELECT application_name,
       COALESCE(client_addr::text, '')           AS client_addr,
       state,
       COALESCE(sync_state, 'async')             AS sync_state,
       pg_wal_lsn_diff(sent_lsn, write_lsn)::bigint   AS write_lag_bytes,
       pg_wal_lsn_diff(sent_lsn, flush_lsn)::bigint   AS flush_lag_bytes,
       pg_wal_lsn_diff(sent_lsn, replay_lsn)::bigint  AS replay_lag_bytes,
       EXTRACT(epoch FROM write_lag)              AS write_lag_sec,
       EXTRACT(epoch FROM flush_lag)              AS flush_lag_sec,
       EXTRACT(epoch FROM replay_lag)             AS replay_lag_sec
FROM pg_stat_replication
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	var metrics []pgtype.Metric
	for rows.Next() {
		var row replicationStreamingRow
		if err := rows.Scan(
			&row.ApplicationName,
			&row.ClientAddr,
			&row.State,
			&row.SyncState,
			&row.WriteLagBytes,
			&row.FlushLagBytes,
			&row.ReplayLagBytes,
			&row.WriteLagSec,
			&row.FlushLagSec,
			&row.ReplayLagSec,
		); err != nil {
			return Result{}, err
		}

		// The label MUST be called sync_state: that is the name
		// internal/server/pipeline.go reads to fill
		// metrics_replication.sync_state. It was emitted as "sync_mode", so
		// the column stayed NULL on every row ever written, and the value
		// pg_stat_replication reports here — the one thing that says whether
		// a standby is synchronous — never reached the server at all. The
		// consequences were all silent: the Sync column of the cluster page's
		// lag table read "Unknown" for every standby in every cluster,
		// GET /api/v1/clusters/{id}/replication returned sync_state: null,
		// and the replica.no_sync_standby alert rule had nothing to count.
		labels := map[string]string{
			"standby":    row.ApplicationName,
			"client":     row.ClientAddr,
			"state":      row.State,
			"sync_state": row.SyncState,
		}

		if row.WriteLagBytes != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_write_lag_bytes",
				Value:  float64(*row.WriteLagBytes),
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
		if row.FlushLagBytes != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_flush_lag_bytes",
				Value:  float64(*row.FlushLagBytes),
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
		if row.ReplayLagBytes != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_replay_lag_bytes",
				Value:  float64(*row.ReplayLagBytes),
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
		if row.WriteLagSec != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_write_lag_seconds",
				Value:  *row.WriteLagSec,
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
		if row.FlushLagSec != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_flush_lag_seconds",
				Value:  *row.FlushLagSec,
				Kind:   pgtype.KindGauge,
				Labels: labels,
			})
		}
		if row.ReplayLagSec != nil {
			metrics = append(metrics, pgtype.Metric{
				Name:   "replication_replay_lag_seconds",
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
