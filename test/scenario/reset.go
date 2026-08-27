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

			var negCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND ts >= $2 AND value < 0`,
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
}
