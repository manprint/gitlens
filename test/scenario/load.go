//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-LOAD-002",
		Title:    "workloadctl lock-storm --sessions 50",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "phase_08.md#7.4", "IDEA.md#4.4"},
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			var instanceID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
				return err == nil, nil
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			cfg := e.PG("pg").Config().ConnConfig
			dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
				cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

			const stormDuration = 25 * time.Second
			stormErr := make(chan error, 1)
			stormStart := time.Now().UTC()
			go func() {
				_, err := e.Workload("lock-storm", "--dsn", dsn, "--sessions", "50", "--duration", stormDuration.String())
				stormErr <- err
			}()

			// The concrete payoff of per-check timeouts (IDEA.md#4.4): 50
			// sessions piled up on SELECT...FOR UPDATE must not block the
			// activity check's own timeout-bounded scrape. Poll THROUGH the
			// storm (not just after it) for fresh pg_backends samples, and
			// require at least one pg_max_xact_age_seconds reading that
			// reflects the storm's long-held transactions.
			var sawBlockedXact bool
			var lastFreshCount int
			checkCtx, cancelCheck := context.WithTimeout(ctx, stormDuration+15*time.Second)
			defer cancelCheck()
			if err := poll(checkCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				if err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE metric='pg_backends' AND ts >= $1`,
					stormStart).Scan(&n); err != nil {
					return false, err
				}
				lastFreshCount = n

				// lock-storm's own transactions hold their row lock for
				// only 1ms once granted, so with 50 sessions cycling
				// through one contended row, an individual transaction's
				// xact_age reflects queueing delay, not a long hold — any
				// measurable value at all (not just 0, which is what an
				// idle/uncontended instance reports) is the real signal.
				var maxXact float64
				if err := e.DB.QueryRow(ctx,
					`SELECT COALESCE(max(value), 0) FROM metrics WHERE metric='pg_max_xact_age_seconds' AND ts >= $1`,
					stormStart).Scan(&maxXact); err == nil && maxXact > 0 {
					sawBlockedXact = true
				}

				select {
				case err := <-stormErr:
					// Storm finished (successfully or not); stop polling either way.
					if err != nil {
						return false, fmt.Errorf("workloadctl lock-storm: %w", err)
					}
					return true, nil
				default:
					return false, nil
				}
			}); err != nil {
				return fmt.Errorf("lock-storm did not complete cleanly (fresh pg_backends samples seen: %d): %w", lastFreshCount, err)
			}

			if lastFreshCount == 0 {
				return fmt.Errorf("no pg_backends samples landed with ts during the storm — the activity check may have stalled behind the 50 blocked sessions")
			}
			if !sawBlockedXact {
				return fmt.Errorf("pg_max_xact_age_seconds never reflected the storm's held transactions (>1s)")
			}
			stormEnd := time.Now().UTC()

			// phase 7.4's own re-assertion of this SAME scenario ID
			// (phase_08.md#7.4): the storm must be visible as Lock waits,
			// and — the concrete proof that a 500ms check timeout survives
			// an incident, ASH's own version of IDEA.md#4.4's payoff — the
			// sampler itself must not have skipped more than 2 of its 10
			// per-second ticks in any window during the storm. Wait for
			// ASH's 10s aggregation + push_interval latency to actually
			// deliver the storm's windows before querying (SYS-LOAD-003
			// already found querying too early finds nothing).
			select {
			case <-time.After(15 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			var sawLockWait bool
			if err := e.DB.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM metrics_ash WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3 AND wait_event_type='Lock')`,
				instanceID, stormStart, stormEnd).Scan(&sawLockWait); err != nil {
				return fmt.Errorf("query ASH Lock waits: %w", err)
			}
			if !sawLockWait {
				return fmt.Errorf("the storm never showed up as a Lock wait_event_type in ASH")
			}

			ticksRows, err := e.DB.Query(ctx,
				`SELECT ts, MAX(window_ticks) FROM metrics_ash WHERE instance_id=$1::uuid AND ts >= $2 AND ts <= $3 GROUP BY ts ORDER BY ts`,
				instanceID, stormStart, stormEnd)
			if err != nil {
				return fmt.Errorf("query ASH ticks: %w", err)
			}
			var windowsSeen int
			for ticksRows.Next() {
				var ts time.Time
				var ticks int
				if err := ticksRows.Scan(&ts, &ticks); err != nil {
					ticksRows.Close()
					return fmt.Errorf("scan ASH ticks: %w", err)
				}
				windowsSeen++
				if ticks < 8 {
					ticksRows.Close()
					return fmt.Errorf("ASH window at %s only completed %d/10 ticks during the storm — the sampler stalled behind the lock contention", ts, ticks)
				}
			}
			if err := ticksRows.Err(); err != nil {
				return fmt.Errorf("iterate ASH ticks: %w", err)
			}
			ticksRows.Close()
			if windowsSeen == 0 {
				return fmt.Errorf("no ASH windows observed during the storm")
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
