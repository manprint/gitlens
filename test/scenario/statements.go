//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-LOAD-008",
		Title:    "workloadctl distinct-queries --count 5000: cardinality budget holds",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-8"},
		Smoke:    true,
		Expect: Expectations{
			Invariants: []string{"I-1", "I-2", "I-3", "I-4"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve instance: %w", err)
			}

			cfg := e.PG("pg").Config().ConnConfig
			dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
				cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

			// pglens's own statement metrics are RATES (calls_rate,
			// exec_time_rate_ms — internal/delta.Engine computes them
			// between consecutive scrapes), so a query executed exactly
			// once can never show a non-zero rate on any scrape after the
			// first — found live: 5000 genuinely one-shot queries, even
			// individually weighted (pg_sleep), never competed with
			// pglens's own repeating monitoring queries for
			// stat_statements.go's top-N-by-significance selection, no
			// matter the count. Re-executing the whole batch across
			// multiple stat_statements scrape intervals (60s-plus-jitter)
			// gives every one of the 5000 queryids a genuine,
			// non-zero, mutually-competitive rate.
			//
			// `--duration` must also span enough of test/workload/distinct.go's
			// own hot-group rotations (65s each) to make
			// internal/cardinality.Selector's Truncated flag genuinely
			// fire: Truncated only fires once the fresh∪retained set
			// exceeds MaxKeys (200), which needs at least 5 rotations of
			// its 50-query hot group (found live: a workload with a
			// stable, non-rotating per-query weight never grows past
			// ~100 kept keys, no matter how long it runs). 6 rotations
			// (390s) leaves a safety margin above that minimum.
			if _, err := e.Workload("distinct-queries", "--dsn", dsn, "--count", "5000", "--duration", "390s"); err != nil {
				return fmt.Errorf("workloadctl distinct-queries: %w", err)
			}

			// stat_statements.go's own cardinality.Selector (TopN: 50) caps
			// what gets emitted long before it ever reaches the API — 5000
			// distinct queryids must show up as "truncated", not silently
			// accepted wholesale.
			statementsCtx, cancelStatements := context.WithTimeout(ctx, 200*time.Second)
			defer cancelStatements()
			var lastResp map[string]interface{}
			if err := poll(statementsCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				resp, err := e.API.Get(fmt.Sprintf("/api/v1/statements?instance_id=%s", instanceID))
				if err != nil {
					return false, err
				}
				lastResp = resp
				truncated, _ := resp["truncated"].(bool)
				stmts, _ := resp["statements"].([]interface{})
				return truncated && len(stmts) > 0, nil
			}); err != nil {
				return fmt.Errorf("statements never reported truncated=true with results (last response: %v): %w", lastResp, err)
			}

			// Top queries by calls must also be present — a different
			// order_by, same underlying cardinality-capped set.
			byCalls, err := e.API.Get(fmt.Sprintf("/api/v1/statements?instance_id=%s&order_by=calls", instanceID))
			if err != nil {
				return fmt.Errorf("GET /api/v1/statements?order_by=calls: %w", err)
			}
			stmtsByCalls, _ := byCalls["statements"].([]interface{})
			if len(stmtsByCalls) == 0 {
				return fmt.Errorf("no statements present when ordered by calls")
			}

			// The cardinality budget itself must hold: pglens_series_total
			// for this instance must stay bounded, nowhere near the 5000
			// distinct queries actually executed. AssertInvariants holds it to
			// I-8 (harness.DefaultMetricBudget), the one budget the suite has;
			// a bound kept in this scenario would only disagree with it.
			//
			// stat_statements.go's MaxKeys of 200 per check, times the six
			// series each kept queryid emits, reaches 1200 before any other
			// instance-level series or the Selector's hysteresis overlap. The
			// full-suite values observed so far, 1857 to 2552, are above I-8's
			// 2000, so this scenario can fail I-8. Whether the budget or the
			// fan-out is wrong needs measured data; this scenario should not
			// settle it with a number of its own.
			e.AssertInvariants(e.T)
			return nil
		},
	})
}
