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
			// The 2000 budget is a ceiling measured on this stack, not one the
			// code guarantees. Each (instance, database) scope adds up to 1400
			// series from stat_statements (200 queryids at seven counters each),
			// 1600 from table_stats (200 tables at eight) and 1000 from
			// index_stats (200 indexes at five), plus seven from database_stats:
			// 4007 at the caps, before the instance-level counters. This stack
			// has one connectable database, "postgres", because discovery skips
			// the templates and this scenario creates none, so only one set of
			// those terms counts. The measured total, 1256 series on the run of
			// 2026-10-23 (63% of the budget), is evidence for this stack, not a
			// bound. The 2552 of the 2026-08-31 full run
			// (docs/plans/003_plan-Frontend/STATE.md) came from a build whose
			// gauge counted the delta engine's retained state, which fa6de00
			// replaced with the current envelope's series. The cause of that
			// count is not recorded, so this comment does not rely on it. With
			// two databases at the caps the per-database terms reach 8014, so
			// the per-instance budget does not hold on a multi-database stack.
			e.AssertInvariants(e.T)
			return nil
		},
	})
}
