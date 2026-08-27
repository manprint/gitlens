//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-TOPO-001",
		Title:    "primary and standby share one system_identifier",
		Topology: TopologyPrimaryStandby,
		Covers:   []string{"phase_07.md#4.1"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1"}},
		Run: func(ctx context.Context, e *Env) error {
			primary := e.PG("pg-primary")
			standby := e.PG("pg-standby")

			// Streaming replication takes a moment to establish after the
			// containers report healthy; poll rather than assume it is
			// already up the instant this scenario starts.
			pollCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			if err := poll(pollCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var replCount int
				if err := primary.QueryRow(ctx, "SELECT count(*) FROM pg_stat_replication").Scan(&replCount); err != nil {
					return false, err
				}
				var inRecovery bool
				if err := standby.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&inRecovery); err != nil {
					return false, err
				}
				return replCount == 1 && inRecovery, nil
			}); err != nil {
				return fmt.Errorf("replication did not establish: %w", err)
			}

			var sysIDPrimary, sysIDStandby string
			if err := primary.QueryRow(ctx, "SELECT CAST(system_identifier AS text) FROM pg_control_system()").Scan(&sysIDPrimary); err != nil {
				return fmt.Errorf("query system_identifier on primary: %w", err)
			}
			if err := standby.QueryRow(ctx, "SELECT CAST(system_identifier AS text) FROM pg_control_system()").Scan(&sysIDStandby); err != nil {
				return fmt.Errorf("query system_identifier on standby: %w", err)
			}
			if sysIDPrimary != sysIDStandby {
				return fmt.Errorf("system_identifier mismatch: primary=%s standby=%s", sysIDPrimary, sysIDStandby)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
