package check

import (
	"context"
	"fmt"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&replicationStreamingCheck{}) }

type replicationStreamingCheck struct{}

func (c *replicationStreamingCheck) Name() string { return "replication_streaming" }

// A cascading standby has walsenders of its own, and the lag of the standbys
// behind it is reported here too, so the check runs on a standby as well as
// on a primary. The query's recovery branch measures from the position the
// standby has received.
func (c *replicationStreamingCheck) Requires() Requirements {
	return Requirements{
		Scope:    ScopeInstance,
		PermTier: pgtype.TierReadOnly,
	}
}
func (c *replicationStreamingCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *replicationStreamingCheck) Timeout() time.Duration         { return 2 * time.Second }

type replicationStreamingRow struct {
	ApplicationName string
	ClientAddr      string
	ClientPort      int
	State           string
	SyncState       string
	WriteLagBytes   *int64
	FlushLagBytes   *int64
	ReplayLagBytes  *int64
	WriteLagSec     *float64
	FlushLagSec     *float64
	ReplayLagSec    *float64
	ReplyAgeSec     *float64
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

	// Byte lag is measured from the WAL this server has generated, not from
	// sent_lsn: WAL that the walsender has not even sent yet is lag too, and
	// measuring from sent_lsn hid exactly the case of a sender falling
	// behind. A cascading standby's own position is what it has received.
	// reply_time is when the standby last answered; the time lags are
	// refreshed only on such an answer, so their age is kept as well.
	rows, err := conn.Query(ctx, `
SELECT COALESCE(r.application_name, '')        AS application_name,
       COALESCE(r.client_addr::text, '')       AS client_addr,
       COALESCE(r.client_port, 0)              AS client_port,
       COALESCE(r.state, '')                   AS state,
       COALESCE(r.sync_state, 'async')         AS sync_state,
       pg_wal_lsn_diff(p.pos, r.write_lsn)::bigint  AS write_lag_bytes,
       pg_wal_lsn_diff(p.pos, r.flush_lsn)::bigint  AS flush_lag_bytes,
       pg_wal_lsn_diff(p.pos, r.replay_lsn)::bigint AS replay_lag_bytes,
       EXTRACT(epoch FROM r.write_lag)          AS write_lag_sec,
       EXTRACT(epoch FROM r.flush_lag)          AS flush_lag_sec,
       EXTRACT(epoch FROM r.replay_lag)         AS replay_lag_sec,
       EXTRACT(epoch FROM now() - r.reply_time) AS reply_age_sec
FROM pg_stat_replication r,
     (SELECT CASE WHEN pg_is_in_recovery() THEN pg_last_wal_receive_lsn()
                  ELSE pg_current_wal_lsn() END AS pos) p
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	var parsed []replicationStreamingRow
	for rows.Next() {
		var row replicationStreamingRow
		if err := rows.Scan(
			&row.ApplicationName,
			&row.ClientAddr,
			&row.ClientPort,
			&row.State,
			&row.SyncState,
			&row.WriteLagBytes,
			&row.FlushLagBytes,
			&row.ReplayLagBytes,
			&row.WriteLagSec,
			&row.FlushLagSec,
			&row.ReplayLagSec,
			&row.ReplyAgeSec,
		); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, row)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return Result{Metrics: replicationStreamingMetrics(parsed)}, nil
}

// staleLag raises a time lag to the age of the standby's last reply while the
// standby is behind in bytes. The walsender recomputes write, flush and replay
// lag only when a reply arrives, so a standby that stops replying (hung,
// paused or partitioned) kept its last small value through a growing backlog.
// A caught-up standby is not lagging because it has gone quiet, so its lag is
// left as reported.
func staleLag(lag, replyAge *float64, behindBytes *int64) *float64 {
	if replyAge == nil || behindBytes == nil || *behindBytes <= 0 {
		return lag
	}
	age := *replyAge
	// now() is the statement's start, so a reply that arrived after it reads
	// as a negative age, which is no lag at all.
	if age < 0 {
		age = 0
	}
	if lag != nil && *lag >= age {
		return lag
	}
	return &age
}

// standbyIdentity names each walsender row. The server keys a standby's lag
// row by its application_name label, and standbys whose primary_conninfo sets
// none all report the default "walreceiver": the client address keeps them
// apart instead of letting one overwrite the other.
func standbyIdentity(rows []replicationStreamingRow) []string {
	seen := make(map[string]int, len(rows))
	for _, r := range rows {
		seen[r.ApplicationName]++
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ApplicationName
		if seen[r.ApplicationName] > 1 || r.ApplicationName == "" {
			ids[i] = fmt.Sprintf("%s@%s:%d", r.ApplicationName, r.ClientAddr, r.ClientPort)
		}
	}
	return ids
}

func replicationStreamingMetrics(rows []replicationStreamingRow) []pgtype.Metric {
	var metrics []pgtype.Metric
	ids := standbyIdentity(rows)
	for i, row := range rows {
		row.WriteLagSec = staleLag(row.WriteLagSec, row.ReplyAgeSec, row.WriteLagBytes)
		row.FlushLagSec = staleLag(row.FlushLagSec, row.ReplyAgeSec, row.FlushLagBytes)
		row.ReplayLagSec = staleLag(row.ReplayLagSec, row.ReplyAgeSec, row.ReplayLagBytes)

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
		//
		// application_name is the label the server keys each standby's row
		// by. It used to be emitted as "standby", which the server does not
		// read, so every standby of a primary collapsed into one unnamed row
		// and only the last one's lag survived.
		labels := map[string]string{
			"application_name": ids[i],
			"client":           row.ClientAddr,
			"state":            row.State,
			"sync_state":       row.SyncState,
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
	return metrics
}
