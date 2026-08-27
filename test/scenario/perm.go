//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-PERM-001",
		Title:    "whole stack green on tier T0 only",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			// test/compose's monitoring_user.sql (deploy/sql/monitoring_user.sql,
			// applied verbatim per §10's do-not-repeat rule) grants only
			// pg_monitor plus the two pg_control_system()/pg_control_checkpoint()
			// EXECUTE grants — T1 (pg_read_all_data) and T2 (pg_signal_backend)
			// are commented out. So the standalone topology already is the T0-only
			// fixture this scenario needs; no separate "rds-like" variant exists
			// yet (test/fixtures/sql/02_rds_like.sql, from sub-phase 2.2, is used
			// only by the L2 pgtest harness, not the L3 compose stack).
			clustersCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var clusters []map[string]interface{}
			if err := poll(clustersCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var err error
				clusters, err = e.API.Clusters()
				return err == nil && len(clusters) > 0, err
			}); err != nil {
				return fmt.Errorf("resolve clusters from API: %w", err)
			}

			found := false
			for _, c := range clusters {
				instances, _ := c["instances"].([]interface{})
				for _, raw := range instances {
					inst, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					tier, _ := inst["perm_tier"].(string)
					if tier != "T0" {
						return fmt.Errorf("instance %v reports perm_tier=%q, want T0", inst["instance_id"], tier)
					}
					found = true
				}
			}
			if !found {
				return fmt.Errorf("API reported no instances to check perm_tier on")
			}

			// Every mandatory check (instance_info, activity, database_stats)
			// must actually be producing fresh samples under T0 — a silent
			// per-check failure would starve one of these series instead of
			// surfacing anywhere else. check_error_total does not exist yet
			// (STATE.md §6/§9 V001-F04, same gap already noted for I-8 in
			// sub-phase 5.6) so this is the honest proxy available today.
			// Polled, not a one-shot count: each check runs on its own
			// hardcoded DefaultInterval() (activity 10s, database_stats
			// 30s — the agent config's per-check `interval:` override is
			// parsed but never actually consulted by the scheduler, a real
			// gap STATE.md §8 records), plus scheduling jitter, plus
			// database_stats needs two raw observations before its first
			// rate exists at all.
			backendsCtx, cancel2 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel2()
			if err := poll(backendsCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_backends'`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pg_backends samples — activity check is not producing data under T0: %w", err)
			}
			xactCtx, cancel3 := context.WithTimeout(ctx, 90*time.Second)
			defer cancel3()
			if err := poll(xactCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit_total'`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no pg_xact_commit_total samples — database_stats check is not producing data under T0: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
