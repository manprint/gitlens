//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-AGENT-001",
		Title:    "agent restart with its volume keeps the instance id",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-1"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1"}},
		Run: func(ctx context.Context, e *Env) error {
			var beforeID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&beforeID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve instance before restart: %w", err)
			}

			if err := e.Compose("restart", "pglens-agent"); err != nil {
				return fmt.Errorf("restart agent: %w", err)
			}
			restartTS := time.Now().UTC()

			// The identity file lives on the agent's persistent volume
			// (agent-container.yml), so the restarted process must resume
			// reporting under the exact same instance_id.
			resumeCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()
			if err := poll(resumeCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM instances WHERE instance_id=$1::uuid AND last_seen >= $2`,
					beforeID, restartTS).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("agent did not resume reporting under the same instance_id: %w", err)
			}

			// Polled, not a one-shot count: the `instances` row updates on
			// any push (including a bare re-registration), but an actual
			// metric sample needs the next scheduled check tick — activity/
			// instance_info run every 10s (plus jitter) on their own
			// hardcoded DefaultInterval(), so a single immediate query here
			// raced the agent's own scheduler and failed on a fresh restart
			// where the very first push after coming back up carries no
			// results yet.
			seriesCtx, cancel3 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel3()
			if err := poll(seriesCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND ts >= $2`,
					beforeID, restartTS).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no metric samples landed for instance %s after the agent restart: %w", beforeID, err)
			}

			var dupCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM events WHERE type='duplicate_instance_suspected' AND ts >= $1`,
				restartTS.Add(-time.Minute)).Scan(&dupCount); err != nil {
				return fmt.Errorf("query duplicate_instance_suspected events: %w", err)
			}
			if dupCount > 0 {
				return fmt.Errorf("unexpected duplicate_instance_suspected event after an in-volume agent restart")
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-AGENT-002",
		Title:    "agent restart WITHOUT its volume: a new instance_id appears (documented failure)",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "IDEA.md#2.2"},
		Smoke:    false,
		Expect:   Expectations{Events: []string{"duplicate_instance_suspected"}},
		Run: func(ctx context.Context, e *Env) error {
			// This scenario intentionally asserts the DOCUMENTED FAILURE,
			// not success (phase_06.md is explicit about this): without a
			// persistent volume, /var/lib/pglens/identity.json lives only
			// in the container's writable layer, so a container recreate
			// (not merely a process restart — a `docker compose restart`
			// reuses the same writable layer and would NOT reproduce this)
			// loses it, and the agent allocates a brand new instance_id.
			// If this test ever starts failing because the agent learned
			// to recover its identity without a volume, IDEA.md's own
			// documented limitation needs updating, not this test.
			var beforeID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT instance_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&beforeID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve instance before recreate: %w", err)
			}

			if err := e.Compose("up", "-d", "--force-recreate", "pglens-agent"); err != nil {
				return fmt.Errorf("force-recreate agent: %w", err)
			}
			recreateTS := time.Now().UTC()

			var afterID string
			resumeCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()
			if err := poll(resumeCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx,
					`SELECT instance_id::text FROM instances WHERE last_seen >= $1 ORDER BY last_seen DESC LIMIT 1`,
					recreateTS).Scan(&afterID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("agent never resumed reporting after being recreated: %w", err)
			}
			if afterID == beforeID {
				return fmt.Errorf("instance_id unchanged (%s) after a volume-less recreate — either identity somehow survived (update IDEA.md if intentional) or the agent never actually restarted", beforeID)
			}

			dupCtx, cancel3 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel3()
			if err := poll(dupCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE type='duplicate_instance_suspected' AND ts >= $1`,
					recreateTS.Add(-time.Minute)).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("duplicate_instance_suspected event never appeared: %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
