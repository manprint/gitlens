package server

import (
	"context"
	"math"
	"time"
)

// Rollup read path (plan 007 unit 5.3). A request whose range starts before
// the raw retention horizon plus one hour is answered entirely from the hourly
// rollup of its family (migration 0035); the response says which with
// resolution "raw" or "1h".

const (
	resolutionRaw    = "raw"
	resolutionHourly = "1h"
)

// rollupSources maps a raw family to its rollup, read as a relation shaped
// like the raw table: bucket stands in for ts, so the raw query text runs on
// it unchanged once given an alias. A family missing here has no rollup
// reader; metrics_1h is kept for a later reader (A15). Each value is a
// constant: no request text reaches it.
var rollupSources = map[string]string{
	"metrics_statements": `(SELECT bucket AS ts, tenant_id, cluster_id, instance_id, datname, queryid,
       calls_rate, exec_time_rate_ms, rows_rate, shared_blks_hit_rate, shared_blks_read_rate,
       wal_bytes_rate, temp_blks_written_rate, COALESCE(sample_seconds, samples * 60) AS sample_seconds
  FROM metrics_statements_1h)`,
	"metrics_ash": `(SELECT bucket AS ts, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event,
       ''::text AS state, queryid, samples, window_ticks, window_seconds
  FROM metrics_ash_1h)`,
}

// rollupDropped are the dimensions the ASH rollup does not keep: a request on
// a rollup range that groups or filters by one is refused.
var rollupDropped = []string{"state", "usename", "client_addr", "application_name", "service", "endpoint"}

func rollupDroppedMessage(name string) string {
	return "this range is served from hourly rollups, which do not keep " + name
}

// rollupFor is the read rule: the range starting at from is served from the
// rollup when from is before now − rawDays + 1 hour. A family without a rollup
// or without a raw retention (rawDays ≤ 0) always reads raw.
func rollupFor(family string, from, now time.Time, rawDays int) bool {
	if _, ok := rollupSources[family]; !ok || rawDays <= 0 {
		return false
	}
	horizon := now.Add(-time.Duration(rawDays) * 24 * time.Hour)
	return from.Before(horizon.Add(time.Hour))
}

// rawRetentionDays is the live raw retention of the family in whole days
// (rounded down, at least 1), the family default when the policy cannot be
// read, and 0 when the family keeps its raw rows without a retention policy.
func rawRetentionDays(ctx context.Context, ps policyStore, family string) int {
	f, ok := lookupRetentionFamily(family)
	if !ok {
		return 0
	}
	if ps == nil {
		return f.DefaultDays
	}
	policies, err := ps.Policies(ctx)
	if err != nil {
		return f.DefaultDays
	}
	p, ok := policies[f.Hypertable]
	if !ok || p.DropAfter == nil {
		return 0
	}
	return max(1, int(math.Floor(*p.DropAfter/secondsPerDay)))
}

// routeToRollup applies rollupFor with the live retention. A range starting
// within the retention floor never reaches the rollup, so it skips the policy
// read; a policy set by hand below that floor is not followed.
func routeToRollup(ctx context.Context, ps policyStore, family string, from, now time.Time) bool {
	if _, ok := rollupSources[family]; !ok {
		return false
	}
	if !from.Before(now.Add(-retentionFloorDays*24*time.Hour + time.Hour)) {
		return false
	}
	return rollupFor(family, from, now, rawRetentionDays(ctx, ps, family))
}

func resolutionOf(rollup bool) string {
	if rollup {
		return resolutionHourly
	}
	return resolutionRaw
}
