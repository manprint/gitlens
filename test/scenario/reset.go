//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-RESET-001",
		Title:    "restart: no negative rate, no spike",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-2"},
		Smoke:    true,
		Expect: Expectations{
			Events:     []string{"counter_reset_detected"},
			Invariants: []string{"I-2"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&instanceID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			// Establish a meaningful steady-state commit rate before the crash.
			// On an otherwise idle CI database the baseline can be close to zero,
			// so normal post-startup transactions look like a false 10x spike.
			// This loop is deliberately owned by the scenario and uses the same
			// pool that reconnects after PostgreSQL comes back.
			loadCtx, cancelLoad := context.WithCancel(ctx)
			loadDone := make(chan struct{})
			defer func() {
				cancelLoad()
				<-loadDone
			}()
			pg := e.PG("pg")
			go func() {
				defer close(loadDone)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-loadCtx.Done():
						return
					case <-ticker.C:
						_, _ = pg.Exec(loadCtx, "SELECT 1")
					}
				}
			}()

			// Wait for a real pre-restart rate to exist, not just a fixed
			// sleep: database_stats.DefaultInterval() is 30s (agent config's
			// per-check `interval:` setting is parsed but never actually
			// consulted by the scheduler — a real gap, STATE.md §8 records
			// it — every check always runs on its own hardcoded interval),
			// and the delta engine needs two raw observations before it can
			// emit a rate at all, so the first rate row can take ~35-65s to
			// appear depending on scheduling jitter.
			baselineCtx, cancel1 := context.WithTimeout(ctx, 90*time.Second)
			defer cancel1()
			if err := poll(baselineCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_xact_commit_total'`,
					instanceID).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pre-restart pg_xact_commit_total rate ever appeared: %w", err)
			}
			restartTS := time.Now().UTC()

			var baseline float64
			if err := e.DB.QueryRow(ctx,
				`SELECT COALESCE(avg(value), 0) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_xact_commit_total' AND ts < $2`,
				instanceID, restartTS).Scan(&baseline); err != nil {
				return fmt.Errorf("query baseline rate: %w", err)
			}

			// A graceful `docker compose restart` does NOT reset
			// pg_stat_database's counters on PostgreSQL 15+: durable stats
			// (the default since 15) are checkpointed to disk on a clean
			// shutdown and restored on the next startup, so xact_commit
			// keeps climbing straight through a graceful restart —
			// empirically verified live. Only an UNclean shutdown (crash,
			// OOM-kill, SIGKILL) loses the stats file and genuinely resets
			// the counters, which is what this scenario needs to exercise
			// the delta engine's reset-detection path at all. This is a
			// correction to phase_06.md's original "docker restart
			// PostgreSQL" wording (STATE.md §8 records it).
			if err := e.Compose("kill", "-s", "SIGKILL", "pg"); err != nil {
				return fmt.Errorf("kill pg: %w", err)
			}
			if err := e.Compose("start", "pg"); err != nil {
				return fmt.Errorf("restart pg: %w", err)
			}

			// PostgreSQL restarting resets pg_stat_database's own counters,
			// so the agent's next sample looks like a counter went
			// backwards — the delta engine must recognize that as a reset,
			// not a negative rate. The window covers a full
			// database_stats.DefaultInterval() (30s) plus scheduling
			// jitter plus margin: pg itself comes back up in a few seconds
			// (well inside one interval), so the circuit breaker realistically
			// never opens here — the wait is dominated by simply reaching
			// the check's next scheduled tick, not by any failure/backoff.
			eventCtx, cancel2 := context.WithTimeout(ctx, 150*time.Second)
			defer cancel2()
			if err := poll(eventCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE instance_id=$1::uuid AND type='counter_reset_detected' AND ts >= $2`,
					instanceID, restartTS).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("counter_reset_detected event never appeared: %w", err)
			}

			// Give the series one more interval to resume normally.
			time.Sleep(10 * time.Second)

			// The generic metrics table also contains raw setting gauges. PostgreSQL
			// uses -1 for "unlimited" byte/time settings; that sentinel is not a
			// negative rate and is intentionally excluded by invariant I-2 too.
			var negCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM metrics
				 WHERE instance_id=$1::uuid AND ts >= $2 AND value < 0
				   AND metric NOT IN ('pg_setting_bytes', 'pg_setting_seconds')`,
				instanceID, restartTS).Scan(&negCount); err != nil {
				return fmt.Errorf("query negative rates: %w", err)
			}
			if negCount > 0 {
				return fmt.Errorf("found %d negative rate samples after restart", negCount)
			}

			if baseline > 0 {
				var spikeCount int
				threshold := baseline * 10
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_xact_commit_total' AND ts >= $2 AND value > $3`,
					instanceID, restartTS, threshold).Scan(&spikeCount); err != nil {
					return fmt.Errorf("query post-restart spikes: %w", err)
				}
				if spikeCount > 0 {
					return fmt.Errorf("found %d samples exceeding 10x the pre-restart rate (%.4f)", spikeCount, baseline)
				}
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-RESET-002",
		Title:    "pg_stat_statements_reset(): discarded delta, named reset event, recovers",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-2"},
		Expect: Expectations{
			Events:     []string{"counter_reset_detected"},
			Invariants: []string{"I-2"},
		},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&instanceID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			// Generate some statement activity so pg_stat_statements has
			// something real to report, then wait for a genuine pre-reset
			// rate to land — stat_statements.DefaultInterval() is 60s (the
			// only ScopeDatabase check; per-check `interval:` config is
			// still dead for it, same as every check but ash — STATE.md
			// §6/V001-F09), so this takes a while.
			if _, err := e.Exec("pg", "psql", "-U", "postgres", "-d", "postgres", "-c",
				"SELECT count(*) FROM generate_series(1,1000)"); err != nil {
				return fmt.Errorf("generate statement activity: %w", err)
			}
			baselineCtx, cancel1 := context.WithTimeout(ctx, 200*time.Second)
			defer cancel1()
			if err := poll(baselineCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics_statements WHERE instance_id=$1::uuid`,
					instanceID).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pre-reset metrics_statements row ever appeared: %w", err)
			}
			resetTS := time.Now().UTC()

			// pg_stat_statements_reset() requires superuser (pglens has no
			// EXECUTE grant on it — a real external session doing this
			// would be an admin, not the monitoring role).
			if _, err := e.Exec("pg", "psql", "-U", "postgres", "-d", "postgres", "-c",
				"SELECT pg_stat_statements_reset()"); err != nil {
				return fmt.Errorf("pg_stat_statements_reset: %w", err)
			}
			// Generate more activity after the reset so the next scrape has
			// something to report (and something for the delta engine to
			// compute a fresh, non-reset rate from on the interval after
			// this one).
			if _, err := e.Exec("pg", "psql", "-U", "postgres", "-d", "postgres", "-c",
				"SELECT count(*) FROM generate_series(1,1000)"); err != nil {
				return fmt.Errorf("generate post-reset statement activity: %w", err)
			}

			// counter_reset_detected names a pg_stat_statements_* metric.
			eventCtx, cancel2 := context.WithTimeout(ctx, 200*time.Second)
			defer cancel2()
			var metricName string
			if err := poll(eventCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx,
					`SELECT payload->>'metric' FROM events WHERE instance_id=$1::uuid AND type='counter_reset_detected' AND ts >= $2 AND payload->>'metric' LIKE '%stat_statements%' LIMIT 1`,
					instanceID, resetTS).Scan(&metricName)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("counter_reset_detected naming a stat_statements metric never appeared: %w", err)
			}

			// The next interval produces a normal (non-negative) rate again.
			normalCtx, cancel3 := context.WithTimeout(ctx, 200*time.Second)
			defer cancel3()
			if err := poll(normalCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics_statements WHERE instance_id=$1::uuid AND ts > $2 AND calls_rate >= 0`,
					instanceID, resetTS).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no normal post-reset rate ever appeared: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
