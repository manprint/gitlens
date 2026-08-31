//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"strings"
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
			// distinct queries actually executed (V001-F11's own /metrics
			// endpoint — the check_error_total/series_total gap this
			// scenario was blocked on until it existed).
			metricsBody, err := e.API.RawGet("/metrics")
			if err != nil {
				return fmt.Errorf("GET /metrics: %w", err)
			}
			seriesTotal, ok := parseSeriesTotal(metricsBody, instanceID)
			if !ok {
				return fmt.Errorf("pglens_series_total{instance_id=%q} not found in /metrics output", instanceID)
			}
			// internal/check/stat_statements.go emits SIX separate metrics per
			// kept queryid (calls_total, total_exec_time_ms, rows_total,
			// shared_blks_hit_total, shared_blks_read_total, wal_bytes_total)
			// — each is its own pgtype.SeriesKey in internal/delta.Engine, so
			// pglens_series_total (Engine.CountByInstance) is really
			// "distinct queryids kept" × 6, not "distinct queryids kept".
			// With the check's own production MaxKeys=200,
			// stat_statements alone can legitimately reach 200*6=1200 —
			// before ANY other instance-level series and before accounting
			// for cardinality.Selector's Hysteresis window legitimately
			// keeping a few prior cycles' worth of now-stale queryids
			// briefly overlapping the current fresh set. Found live: an
			// earlier 500 bound here failed on every correctly-bounded run
			// (~1857-2217 observed, never approaching the true
			// 5000-queries-leaked-through-uncapped disaster of ~30000) —
			// the bound was simply never derived from this fanout. 3000
			// covers the observed full-suite peak (2552) plus scheduling and
			// hysteresis-overlap variation while remaining an order of
			// magnitude below what an actual leak would produce.
			const seriesBudgetSanityBound = 3000
			if seriesTotal > seriesBudgetSanityBound {
				return fmt.Errorf("pglens_series_total=%d exceeds the cardinality budget sanity bound (%d) — 5000 distinct queries may have leaked through uncapped", seriesTotal, seriesBudgetSanityBound)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}

// parseSeriesTotal extracts pglens_series_total{instance_id="<id>"} <value>
// from a raw Prometheus text-exposition body.
func parseSeriesTotal(body, instanceID string) (int, bool) {
	prefix := fmt.Sprintf(`pglens_series_total{instance_id=%q}`, instanceID)
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(fields[len(fields)-1], "%g", &v); err != nil {
			continue
		}
		return int(v), true
	}
	return 0, false
}
