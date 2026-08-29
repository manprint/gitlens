package check

import (
	"context"
	"sort"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&activityCheck{})
}

type activityCheck struct{ byApplication bool }

func (c *activityCheck) Name() string { return "activity" }
func (c *activityCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *activityCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *activityCheck) Timeout() time.Duration         { return 2 * time.Second }
func (c *activityCheck) SetByApplication(enabled bool)  { c.byApplication = enabled }

type activityRow struct {
	State         *string
	WaitEventType *string
	Count         int
	MaxXactAge    float64
	MaxIdleInTxn  float64
}

func (c *activityCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, `
SELECT COALESCE(state, 'unknown') AS state,
       COALESCE(wait_event_type, 'CPU') AS wait_event_type,
       count(*) AS n,
       COALESCE(max(EXTRACT(epoch FROM now() - xact_start)), 0) AS max_xact_age,
       COALESCE(max(EXTRACT(epoch FROM now() - state_change)) FILTER (WHERE state = 'idle in transaction'), 0) AS max_idle_in_txn
FROM pg_stat_activity
WHERE backend_type = 'client backend'
  AND pid <> pg_backend_pid()
GROUP BY 1, 2
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []activityRow
	for rows.Next() {
		var state, waitType string
		var n int
		var maxAge, idleInTxn float64
		if err := rows.Scan(&state, &waitType, &n, &maxAge, &idleInTxn); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, activityRow{State: &state, WaitEventType: &waitType, Count: n, MaxXactAge: maxAge, MaxIdleInTxn: idleInTxn})
	}
	// Get max_connections
	var maxConnStr string
	_ = conn.QueryRow(ctx, "SELECT current_setting('max_connections')").Scan(&maxConnStr)
	databaseRows := queryActivityGroups(ctx, conn, `SELECT COALESCE(datname, ''), count(*)::float8 FROM pg_stat_activity WHERE backend_type='client backend' AND pid <> pg_backend_pid() GROUP BY 1`)
	applicationRows := queryActivityGroups(ctx, conn, `SELECT COALESCE(application_name, ''), count(*)::float8 FROM pg_stat_activity WHERE backend_type='client backend' AND pid <> pg_backend_pid() GROUP BY 1`)
	stateAgeRows := queryActivityGroups(ctx, conn, `SELECT COALESCE(state, 'unknown'), COALESCE(max(EXTRACT(epoch FROM now() - state_change)), 0) FROM pg_stat_activity WHERE backend_type='client backend' AND pid <> pg_backend_pid() GROUP BY 1`)
	var prepared, oldestPrepared float64
	_ = conn.QueryRow(ctx, `SELECT count(*)::float8, COALESCE(EXTRACT(epoch FROM now() - min(prepared)), 0)::float8 FROM pg_prepared_xacts`).Scan(&prepared, &oldestPrepared)
	var frozenAge float64
	_ = conn.QueryRow(ctx, `SELECT COALESCE(max(age(datfrozenxid)), 0)::float8 FROM pg_database`).Scan(&frozenAge)
	result := activityCheckResult(parsed, maxConnStr, c.byApplication, databaseRows, applicationRows, stateAgeRows, prepared, oldestPrepared, frozenAge)
	var total, waiting int
	for _, row := range parsed {
		total += row.Count
		if row.WaitEventType != nil && *row.WaitEventType != "CPU" {
			waiting += row.Count
		}
	}
	if total > 0 {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_backends_waiting_ratio", Value: float64(waiting) / float64(total), Kind: pgtype.KindGauge})
	}
	return result, nil
}

func queryActivityGroups(ctx context.Context, conn Conn, query string) []activityGroup {
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []activityGroup
	for rows.Next() {
		var label string
		var value float64
		if err := rows.Scan(&label, &value); err == nil {
			result = append(result, activityGroup{Label: label, Value: value})
		}
	}
	return result
}

func buildActivityResult(rows []activityRow, maxConnStr string) Result {
	var metrics []pgtype.Metric
	var maxXact, maxIdle float64
	for _, r := range rows {
		state := "unknown"
		if r.State != nil {
			state = *r.State
		}
		waitType := "CPU"
		if r.WaitEventType != nil {
			waitType = *r.WaitEventType
		}
		labels := map[string]string{"state": state, "wait_event_type": waitType}
		metrics = append(metrics, pgtype.Metric{Name: "pg_backends", Value: float64(r.Count), Kind: pgtype.KindGauge, Labels: labels})
		if r.MaxXactAge > maxXact {
			maxXact = r.MaxXactAge
		}
		if r.MaxIdleInTxn > maxIdle {
			maxIdle = r.MaxIdleInTxn
		}
	}
	metrics = append(metrics, pgtype.Metric{Name: "pg_max_xact_age_seconds", Value: maxXact, Kind: pgtype.KindGauge})
	metrics = append(metrics, pgtype.Metric{Name: "pg_max_idle_in_transaction_seconds", Value: maxIdle, Kind: pgtype.KindGauge})
	if maxConnStr != "" {
		maxConn := 0
		for _, c := range maxConnStr {
			if c >= '0' && c <= '9' {
				maxConn = maxConn*10 + int(c-'0')
			}
		}
		if maxConn > 0 {
			sum := 0
			for _, m := range metrics {
				if m.Name == "pg_backends" {
					sum += int(m.Value)
				}
			}
			metrics = append(metrics, pgtype.Metric{Name: "pg_connections_used", Value: float64(sum), Kind: pgtype.KindGauge})
			metrics = append(metrics, pgtype.Metric{Name: "pg_connections_limit", Value: float64(maxConn), Kind: pgtype.KindGauge})
		}
	}
	// The aggregate query above intentionally remains unchanged. These bounded
	// views are derived from separate statements so the legacy metric contract
	// remains stable across agent upgrades.
	return Result{Metrics: metrics}
}

// activityCheckResult augments the legacy aggregate with optional bounded
// views. It is kept separate so unit callers of buildActivityResult retain the
// exact pre-phase-3 behavior.
func activityCheckResult(rows []activityRow, maxConnStr string, byApplication bool, databaseRows, applicationRows, stateAgeRows []activityGroup, prepared, oldestPrepared, frozenAge float64) Result {
	result := buildActivityResult(rows, maxConnStr)
	maxConn := 0
	for _, r := range maxConnStr {
		if r >= '0' && r <= '9' {
			maxConn = maxConn*10 + int(r-'0')
		}
	}
	if maxConn > 0 {
		used := 0
		for _, row := range rows {
			used += row.Count
		}
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_connections_used_ratio", Value: float64(used) / float64(maxConn), Kind: pgtype.KindGauge})
	}
	result.Metrics = append(result.Metrics, boundedActivityMetrics("pg_connections_by_database", "datname", databaseRows)...)
	if byApplication {
		result.Metrics = append(result.Metrics, boundedActivityMetrics("pg_connections_by_application", "application_name", applicationRows)...)
	}
	for _, row := range stateAgeRows {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_max_state_age_seconds", Value: row.Value, Kind: pgtype.KindGauge, Labels: map[string]string{"state": row.Label}})
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_prepared_xacts", Value: prepared, Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_oldest_prepared_xact_seconds", Value: oldestPrepared, Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_max_datfrozenxid_age", Value: frozenAge, Kind: pgtype.KindGauge},
	)
	return result
}

type activityGroup struct {
	Label string
	Value float64
}

func boundedActivityMetrics(name, label string, rows []activityGroup) []pgtype.Metric {
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Value > rows[j].Value })
	metrics := make([]pgtype.Metric, 0, minInt(len(rows), 11))
	other := 0.0
	for i, row := range rows {
		if i < 10 {
			metrics = append(metrics, pgtype.Metric{Name: name, Value: row.Value, Kind: pgtype.KindGauge, Labels: map[string]string{label: row.Label}})
		} else {
			other += row.Value
		}
	}
	if other > 0 {
		metrics = append(metrics, pgtype.Metric{Name: name, Value: other, Kind: pgtype.KindGauge, Labels: map[string]string{label: "other"}})
	}
	return metrics
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
