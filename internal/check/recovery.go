package check

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&recoveryCheck{}) }

// recoveryStateQuery tells whether the server replays WAL and, if it does,
// whether replay is paused (pg_wal_replay_pause, or a recovery target with
// recovery_target_action = pause). The CASE keeps the pause function from
// running on a primary, where it raises an error.
const recoveryStateQuery = `SELECT pg_catalog.pg_is_in_recovery(),
       CASE WHEN pg_catalog.pg_is_in_recovery() THEN pg_catalog.pg_get_wal_replay_pause_state() END`

// recoveryConflictsQuery reads the queries each database's standby sessions
// lost to recovery conflicts, by reason. It is read as JSON because 16 adds
// confl_active_logicalslot.
const recoveryConflictsQuery = `SELECT c.datname, pg_catalog.to_jsonb(c)::text
  FROM pg_catalog.pg_stat_database_conflicts c
 WHERE c.datname IS NOT NULL`

// recoveryPrefetchQuery reads the WAL prefetcher's counters.
const recoveryPrefetchQuery = `SELECT pg_catalog.to_jsonb(p)::text FROM pg_catalog.pg_stat_recovery_prefetch p`

// recoveryPrefetchOutcomes are the columns of pg_stat_recovery_prefetch that
// count blocks referenced by WAL, by what the prefetcher did with them.
var recoveryPrefetchOutcomes = []string{"prefetch", "hit", "skip_init", "skip_new", "skip_fpw", "skip_rep"}

// recoveryPrefetchGauges are its current-state columns, by metric.
var recoveryPrefetchGauges = map[string]string{
	"wal_distance":   "pg_recovery_prefetch_wal_distance_bytes",
	"block_distance": "pg_recovery_prefetch_block_distance",
	"io_depth":       "pg_recovery_prefetch_io_depth",
}

// recoveryCheck reports what WAL replay costs a standby: the queries it
// cancels by conflict reason, whether replay is paused, and the prefetcher's
// counters. It runs on every server and reports nothing on a primary, where
// the conflict counters stay at zero, so no rule that reads it is ever left
// without its input on a primary.
type recoveryCheck struct{}

func (c *recoveryCheck) Name() string { return "recovery" }
func (c *recoveryCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *recoveryCheck) DefaultInterval() time.Duration { return 30 * time.Second }
func (c *recoveryCheck) Timeout() time.Duration         { return 5 * time.Second }

func (c *recoveryCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var inRecovery bool
	var pauseState *string
	if err := conn.QueryRow(ctx, recoveryStateQuery).Scan(&inRecovery, &pauseState); err != nil {
		return Result{}, err
	}
	if !inRecovery {
		return Result{}, nil
	}
	paused := 0.0
	if pauseState != nil && *pauseState != "not paused" {
		paused = 1
	}
	result := Result{Metrics: []pgtype.Metric{{Name: "pg_recovery_replay_paused", Value: paused, Kind: pgtype.KindGauge}}}

	conflicts, err := recoveryConflictMetrics(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, conflicts...)
	prefetch, err := recoveryPrefetchMetrics(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, prefetch...)
	return result, nil
}

func recoveryConflictMetrics(ctx context.Context, conn Conn) ([]pgtype.Metric, error) {
	rows, err := conn.Query(ctx, recoveryConflictsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var metrics []pgtype.Metric
	for rows.Next() {
		var datname, raw string
		if err := rows.Scan(&datname, &raw); err != nil {
			return nil, err
		}
		var stats map[string]any
		if err := json.Unmarshal([]byte(raw), &stats); err != nil {
			return nil, err
		}
		counts, _ := jsonPrefixCounts(stats, "confl_")
		for _, reason := range slices.Sorted(maps.Keys(counts)) {
			metrics = append(metrics, pgtype.Metric{Name: "pg_recovery_conflicts_total", Value: float64(counts[reason]), Kind: pgtype.KindCounter,
				Labels: map[string]string{"database": datname, "reason": reason}})
		}
	}
	return metrics, rows.Err()
}

func recoveryPrefetchMetrics(ctx context.Context, conn Conn) ([]pgtype.Metric, error) {
	rows, err := conn.Query(ctx, recoveryPrefetchQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var metrics []pgtype.Metric
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var stats map[string]any
		if err := json.Unmarshal([]byte(raw), &stats); err != nil {
			return nil, err
		}
		for _, outcome := range recoveryPrefetchOutcomes {
			if value, ok := stats[outcome].(float64); ok {
				metrics = append(metrics, pgtype.Metric{Name: "pg_recovery_prefetch_blocks_total", Value: value, Kind: pgtype.KindCounter,
					Labels: map[string]string{"outcome": outcome}})
			}
		}
		for _, column := range slices.Sorted(maps.Keys(recoveryPrefetchGauges)) {
			if value, ok := stats[column].(float64); ok {
				metrics = append(metrics, pgtype.Metric{Name: recoveryPrefetchGauges[column], Value: value, Kind: pgtype.KindGauge})
			}
		}
	}
	return metrics, rows.Err()
}
