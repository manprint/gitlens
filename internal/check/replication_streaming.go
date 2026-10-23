package check

import (
	"context"
	"fmt"
	"sort"
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
	// WalSenderTimeoutSec is the server's wal_sender_timeout, which bounds how
	// old a standby's reply may get; 0 disables that bound.
	WalSenderTimeoutSec float64
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
       EXTRACT(epoch FROM now() - r.reply_time) AS reply_age_sec,
       EXTRACT(epoch FROM current_setting('wal_sender_timeout')::interval) AS wal_sender_timeout_sec
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
			&row.WalSenderTimeoutSec,
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

// staleReplyMargin is the slack past the walsender's reply request. The request
// and its reply take a round trip, and reply_time carries the standby's own
// clock, so a host clock that runs ahead or behind moves the age.
const staleReplyMargin = 5.0

// staleReplyBound is the reply age past which a standby reads as stalled, or 0
// when the walsender sets no bound. The walsender asks a silent standby for a
// reply once wal_sender_timeout/2 has passed, and a live standby answers, so a
// reply older than that is missing. A standby that sends its own status less
// often than that does not change the bound.
func staleReplyBound(walSenderTimeout float64) float64 {
	if walSenderTimeout <= 0 {
		return 0
	}
	return walSenderTimeout/2 + staleReplyMargin
}

// staleLag raises a time lag to the age of the standby's last reply once that
// reply is older than staleReplyBound. The walsender recomputes write, flush
// and replay lag only when a reply arrives, so a standby that stops replying
// (hung, paused or partitioned) kept its last small value through a growing
// backlog. A busy standby that is a few bytes behind keeps the lag it reports,
// and so does a caught-up standby that has gone quiet, which is not lagging.
// The trade-off is that a stall shorter than the bound is not raised: with the
// default wal_sender_timeout of 60 s that is about 35 s, and the walsender drops
// the connection at 60 s, after which the standby leaves the sender view.
func staleLag(lag, replyAge *float64, behindBytes *int64, bound float64) *float64 {
	if replyAge == nil || behindBytes == nil || *behindBytes <= 0 || bound <= 0 {
		return lag
	}
	age := *replyAge
	// now() is the statement's start, so a reply that arrived after it reads
	// as a negative age, which is no lag at all.
	if age < 0 {
		age = 0
	}
	if age <= bound {
		return lag
	}
	if lag != nil && *lag >= age {
		return lag
	}
	return &age
}

// standbyIdentity names each walsender row. The server keys a standby's lag
// row by its application_name label, and standbys whose primary_conninfo sets
// none all report the default "walreceiver": the client address keeps them
// apart instead of letting one overwrite the other. The port is left out of
// the name, because a reconnected standby gets a new one. While its old row
// was still in the lookback it would be counted twice. Standbys that still
// share both name and address are numbered in port order; after a reconnect
// the numbers of such a pair can swap, but no extra identity appears.
func standbyIdentity(rows []replicationStreamingRow) []string {
	seen := make(map[string]int, len(rows))
	for _, r := range rows {
		seen[r.ApplicationName]++
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ApplicationName
		if seen[r.ApplicationName] > 1 || r.ApplicationName == "" {
			ids[i] = r.ApplicationName + "@" + r.ClientAddr
		}
	}
	members := make(map[string][]int, len(rows))
	for i, id := range ids {
		members[id] = append(members[id], i)
	}
	for id, group := range members {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(a, b int) bool {
			return rows[group[a]].ClientPort < rows[group[b]].ClientPort
		})
		for n, i := range group {
			ids[i] = fmt.Sprintf("%s#%d", id, n+1)
		}
	}
	return ids
}

func replicationStreamingMetrics(rows []replicationStreamingRow) []pgtype.Metric {
	var metrics []pgtype.Metric
	ids := standbyIdentity(rows)
	for i, row := range rows {
		bound := staleReplyBound(row.WalSenderTimeoutSec)
		row.WriteLagSec = staleLag(row.WriteLagSec, row.ReplyAgeSec, row.WriteLagBytes, bound)
		row.FlushLagSec = staleLag(row.FlushLagSec, row.ReplyAgeSec, row.FlushLagBytes, bound)
		row.ReplayLagSec = staleLag(row.ReplayLagSec, row.ReplyAgeSec, row.ReplayLagBytes, bound)

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
