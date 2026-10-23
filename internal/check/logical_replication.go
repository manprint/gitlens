package check

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&logicalReplicationCheck{}) }

// subscriptionFactKind is the fact kind of one subscription of the
// instance. The server replaces the instance's facts of the kind on every
// complete result, so a dropped subscription stops being reported.
const subscriptionFactKind = "subscription"

// subscriptionFactLimit bounds the subscriptions one result reports.
const subscriptionFactLimit = 200

// subscriptionsQuery lists the subscriptions with their workers and error
// counters. pg_subscription is read column by column, and never as a whole
// row: subconninfo holds the connection string, password included, and is
// neither read nor stored. The statistics view is read as JSON because its
// columns change between versions (sync_error_count splits in two in 19,
// conflict counters arrive in 18).
const subscriptionsQuery = `SELECT s.subname, COALESCE(d.datname, ''), pg_catalog.pg_get_userbyid(s.subowner),
       s.subenabled, COALESCE(s.subslotname::pg_catalog.text, ''),
       pg_catalog.array_to_string(s.subpublications, ','),
       COALESCE(w.apply, false), COALESCE(w.sync, 0),
       COALESCE(pg_catalog.date_part('epoch', w.receipt)::pg_catalog.int8, 0),
       COALESCE(pg_catalog.to_jsonb(st)::pg_catalog.text, '{}')
  FROM pg_catalog.pg_subscription s
  LEFT JOIN pg_catalog.pg_database d ON d.oid = s.subdbid
  LEFT JOIN LATERAL (
       -- Parallel apply workers run only under a leader, so any worker
       -- without a table is a running apply.
       SELECT pg_catalog.bool_or(ss.pid IS NOT NULL AND ss.relid IS NULL) AS apply,
              pg_catalog.count(*) FILTER (WHERE ss.pid IS NOT NULL AND ss.relid IS NOT NULL) AS sync,
              pg_catalog.max(ss.last_msg_receipt_time) FILTER (WHERE ss.relid IS NULL) AS receipt
         FROM pg_catalog.pg_stat_subscription ss
        WHERE ss.subid = s.oid) w ON true
  LEFT JOIN pg_catalog.pg_stat_subscription_stats st ON st.subid = s.oid
 ORDER BY s.subname
 LIMIT $1`

// logicalSlotStatsQuery reads the decoding statistics of the logical slots,
// as JSON for the same reason.
const logicalSlotStatsQuery = `SELECT st.slot_name::pg_catalog.text, pg_catalog.to_jsonb(st)::pg_catalog.text
  FROM pg_catalog.pg_stat_replication_slots st
 ORDER BY 1`

// logicalReplicationCheck reports the subscriptions of the instance (the
// subscriber side of logical replication) and the decoding statistics of
// its logical slots (the publisher side): what pg_replication_slots, read
// by replication_slots, does not say.
type logicalReplicationCheck struct{}

func (c *logicalReplicationCheck) Name() string { return "logical_replication" }
func (c *logicalReplicationCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *logicalReplicationCheck) DefaultInterval() time.Duration { return time.Minute }
func (c *logicalReplicationCheck) Timeout() time.Duration         { return 10 * time.Second }

func (c *logicalReplicationCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var result Result
	if err := c.subscriptions(ctx, conn, &result); err != nil {
		return Result{}, err
	}
	if err := c.slots(ctx, conn, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (c *logicalReplicationCheck) subscriptions(ctx context.Context, conn Conn, result *Result) error {
	rows, err := conn.Query(ctx, subscriptionsQuery, subscriptionFactLimit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	var enabled, running int
	for rows.Next() {
		var name, datname, owner, slot, publications, rawStats string
		var isEnabled, apply bool
		var sync, receipt int64
		if err := rows.Scan(&name, &datname, &owner, &isEnabled, &slot, &publications, &apply, &sync, &receipt, &rawStats); err != nil {
			return err
		}
		if len(result.Facts) == subscriptionFactLimit {
			result.Truncated = true
			break
		}
		stats := map[string]any{}
		_ = json.Unmarshal([]byte(rawStats), &stats)
		applyErrors, _ := jsonCount(stats, "apply_error_count")
		syncErrors, _ := jsonCount(stats, "sync_error_count", "sync_seq_error_count", "sync_table_error_count")
		conflicts, hasConflicts := jsonPrefixCounts(stats, "confl_")

		state := "disabled"
		if isEnabled {
			state = "enabled"
			enabled++
			if apply {
				running++
			}
		}
		labels := map[string]string{
			"database": datname, "owner": owner, "slot_name": slot, "publications": publications,
			"apply_worker": strconv.FormatBool(apply), "sync_workers": strconv.FormatInt(sync, 10),
			"last_msg_receipt_at": "", "apply_errors": strconv.FormatInt(applyErrors, 10),
			"sync_errors": strconv.FormatInt(syncErrors, 10), "conflicts": "",
		}
		if receipt > 0 {
			labels["last_msg_receipt_at"] = time.Unix(receipt, 0).UTC().Format(time.RFC3339)
		}
		series := map[string]string{"subscription": name}
		result.Metrics = append(result.Metrics,
			pgtype.Metric{Name: "pg_subscription_apply_errors_total", Labels: series, Value: float64(applyErrors), Kind: pgtype.KindCounter},
			pgtype.Metric{Name: "pg_subscription_sync_errors_total", Labels: series, Value: float64(syncErrors), Kind: pgtype.KindCounter},
		)
		if hasConflicts {
			var total int64
			for _, kind := range slices.Sorted(maps.Keys(conflicts)) {
				total += conflicts[kind]
				result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_subscription_conflicts_total",
					Labels: map[string]string{"subscription": name, "conflict": kind}, Value: float64(conflicts[kind]), Kind: pgtype.KindCounter})
			}
			labels["conflicts"] = strconv.FormatInt(total, 10)
		}
		result.Facts = append(result.Facts, Fact{Kind: subscriptionFactKind, Key: name, ValueText: state, Labels: labels})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_subscriptions", Value: float64(len(result.Facts)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_subscriptions_enabled", Value: float64(enabled), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_subscriptions_running", Value: float64(running), Kind: pgtype.KindGauge},
	)
	return nil
}

// logicalSlotCounters are the decoding counters of pg_stat_replication_slots
// reported as metrics, by column.
var logicalSlotCounters = []struct{ column, metric string }{
	{"spill_txns", "pg_logical_slot_spill_txns_total"},
	{"spill_bytes", "pg_logical_slot_spill_bytes_total"},
	{"stream_txns", "pg_logical_slot_stream_txns_total"},
	{"stream_bytes", "pg_logical_slot_stream_bytes_total"},
	{"total_txns", "pg_logical_slot_txns_total"},
	{"total_bytes", "pg_logical_slot_bytes_total"},
}

func (c *logicalReplicationCheck) slots(ctx context.Context, conn Conn, result *Result) error {
	rows, err := conn.Query(ctx, logicalSlotStatsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var slot, raw string
		if err := rows.Scan(&slot, &raw); err != nil {
			return err
		}
		stats := map[string]any{}
		_ = json.Unmarshal([]byte(raw), &stats)
		for _, counter := range logicalSlotCounters {
			if value, ok := jsonCount(stats, counter.column); ok {
				result.Metrics = append(result.Metrics, pgtype.Metric{Name: counter.metric,
					Labels: map[string]string{"slot_name": slot}, Value: float64(value), Kind: pgtype.KindCounter})
			}
		}
	}
	return rows.Err()
}

// jsonCount sums the integer columns of a row read as JSON, and reports
// whether any of them was there.
func jsonCount(row map[string]any, columns ...string) (int64, bool) {
	var sum int64
	found := false
	for _, column := range columns {
		if v, ok := row[column].(float64); ok {
			sum += int64(v)
			found = true
		}
	}
	return sum, found
}

// jsonPrefixCounts returns the integer columns of a row read as JSON whose
// name starts with prefix, by name without it.
func jsonPrefixCounts(row map[string]any, prefix string) (map[string]int64, bool) {
	out := map[string]int64{}
	for column, value := range row {
		if v, ok := value.(float64); ok && strings.HasPrefix(column, prefix) {
			out[strings.TrimPrefix(column, prefix)] = int64(v)
		}
	}
	return out, len(out) > 0
}
