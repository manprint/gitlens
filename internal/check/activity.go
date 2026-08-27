package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&activityCheck{})
}

type activityCheck struct{}

func (c *activityCheck) Name() string { return "activity" }
func (c *activityCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *activityCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *activityCheck) Timeout() time.Duration         { return 2 * time.Second }

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
	if err := conn.QueryRow(ctx, "SELECT current_setting('max_connections')").Scan(&maxConnStr); err == nil {
		// Use helper to build metrics
		return buildActivityResult(parsed, maxConnStr), nil
	}
	return buildActivityResult(parsed, ""), nil
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
	return Result{Metrics: metrics}
}
