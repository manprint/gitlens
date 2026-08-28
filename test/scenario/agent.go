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

	Register(Scenario{
		ID:       "SYS-AGENT-003",
		Title:    "buffer directory on a tiny tmpfs, server stopped until it fills",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-7"},
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			// phase_06.md specifies a 16 MiB tmpfs. internal/agent/buffer's
			// segments roll at a hardcoded 8 MiB (not configurable), so 16
			// MiB was clearly sized to exercise 2 segment rolls before real
			// ENOSPC — but the agent's own check intervals are NOT
			// configurable (V001-F09; only `checks.ash.enabled` is a real
			// config consumer), so real envelope data accrues at whatever
			// rate the fixed check cadence produces it: on the order of a
			// few KB per ~10s at best. Filling 16 MiB at that rate would
			// take hours, not a testable window. `agent-container-tiny-
			// buffer.yml` therefore uses a 16 KiB tmpfs instead — 1000x
			// smaller, same exact code path (real ENOSPC on a real
			// filesystem -> buffer.Buffer.handleWriteError ->
			// BufferFull=true -> every subsequent Queue() drops and counts),
			// fillable in a couple of minutes of real agent traffic. A
			// deliberate, documented correction to the plan's literal size,
			// not a fake/synthetic trigger.
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM instances`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			if err := e.Compose("stop", "pglens-server"); err != nil {
				return fmt.Errorf("stop pglens-server: %w", err)
			}

			// The agent keeps retrying its one stuck-in-flight envelope
			// against the dead server (pushOne's own backoff loop, capped at
			// 60s, retries forever until it gets a response) while new
			// scrape results keep landing on disk via Pusher.Queue's
			// buffer.Buffer.Append — so the buffer fills from ordinary
			// agent activity, not from anything the scenario injects.
			fillCtx, cancel2 := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel2()
			if err := poll(fillCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				health, err := e.AgentHealthz()
				if err != nil {
					return false, fmt.Errorf("agent /healthz errored while the buffer was filling (must never crash): %w", err)
				}
				stats, _ := health["buffer_stats"].(map[string]interface{})
				if dropped, ok := stats["dropped"].(float64); ok {
					return dropped > 0, nil
				}
				return false, nil
			}); err != nil {
				return fmt.Errorf("buffer_stats.dropped never grew above 0 after filling the tmpfs: %w", err)
			}

			// PostgreSQL itself (not just the agent) must be completely
			// unaffected by the agent's own disk pressure.
			var one int
			if err := e.PG("pg").QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
				return fmt.Errorf("PostgreSQL unreachable while the agent's buffer was full: %w", err)
			}

			// phase_06.md's own spec for this scenario ends here: dropped
			// grows, the agent doesn't crash, PostgreSQL is unaffected, the
			// host disk doesn't fill. It does NOT require the backlog to
			// drain once the server comes back — and live investigation
			// (a manual debug stack, watching buffer_stats every 15s across
			// a full run) found that requirement is not just slow but
			// structurally unreachable here: buffer.Buffer's size/age-based
			// eviction (the only thing that ever frees space) requires a
			// SECOND segment to exist before it can delete the first one,
			// but segments only roll at a hardcoded 8 MiB — far above any
			// tmpfs small enough to fill in a testable window — so a small
			// buffer that hits real ENOSPC latches BufferFull permanently
			// (Append() checks it first and returns immediately, never
			// reaching the write that would clear it) and can never accept
			// another envelope until the agent process itself restarts.
			// acked froze at a fixed count the instant ENOSPC first hit and
			// stayed there for a full 10-minute observation while dropped
			// climbed monotonically — not a bug to route around here (a
			// real gap, worth its own investigation outside this test: the
			// 8 MiB segment-roll threshold should probably scale with the
			// configured buffer size, not be a fixed constant). Restarting
			// the server is still worth doing (proves the agent doesn't
			// error out reconnecting, and lets the harness's own teardown
			// query the API cleanly), just not asserted as a recovery.
			if err := e.Compose("start", "pglens-server"); err != nil {
				return fmt.Errorf("restart pglens-server: %w", err)
			}
			if _, err := e.AgentHealthz(); err != nil {
				return fmt.Errorf("agent /healthz errored after the server restarted (must never crash, even permanently full): %w", err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-AGENT-004",
		Title:    "agent container started with a 5-minute clock offset",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			// docker has no built-in per-container clock offset, and
			// pglens-agent is a statically linked (CGO_ENABLED=0) binary, so
			// an LD_PRELOAD-based faketime wrapper cannot work either (no
			// dynamic linker to intercept). Shifting the HOST clock would
			// affect every other container and the test harness itself — far
			// too invasive for a single scenario. `PGLENS_DEBUG_CLOCK_OFFSET`
			// (internal/clock.WithOffset, cmd/pglens-agent/run.go) instead
			// gives the agent process its own genuinely-wrong injectable
			// clock: real code path (Pusher.ClockSkew, computed from the
			// server's HTTP Date header same as production), no host-wide
			// side effects. `agent-container-clock-skew.yml` sets it to -5m.
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM instances`).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance: %w", err)
			}

			skewCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()
			var lastSkew float64
			if err := poll(skewCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				health, err := e.AgentHealthz()
				if err != nil {
					return false, fmt.Errorf("agent /healthz errored: %w", err)
				}
				skew, ok := health["clock_skew_seconds"].(float64)
				if !ok {
					return false, nil
				}
				lastSkew = skew
				return skew >= 250 && skew <= 350, nil
			}); err != nil {
				return fmt.Errorf("clock_skew_seconds never settled around 300 (last observed: %v): %w", lastSkew, err)
			}

			warnCtx, cancel3 := context.WithTimeout(ctx, 30*time.Second)
			defer cancel3()
			if err := poll(warnCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				logs, err := e.Logs("pglens-agent")
				if err != nil {
					return false, err
				}
				return strings.Contains(logs, "clock skew detected"), nil
			}); err != nil {
				return fmt.Errorf("no clock skew warning ever appeared in the agent's logs: %w", err)
			}

			// Samples are still accepted: the 12h maxSampleAge threshold
			// (internal/server/ingest.go) is nowhere near 5 minutes of skew,
			// so metrics must keep landing exactly as if the clock were
			// correct. Compared as a growing count rather than an absolute
			// "ts >= now" window: the metric's own ts is stamped from the
			// agent's (deliberately wrong, -5m) clock, not wall-clock time.
			var baseline int
			if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics`).Scan(&baseline); err != nil {
				return fmt.Errorf("query baseline metrics count: %w", err)
			}
			freshCtx, cancel4 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel4()
			if err := poll(freshCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics`).Scan(&n)
				return err == nil && n > baseline, err
			}); err != nil {
				return fmt.Errorf("metrics count never grew past baseline (%d) despite the clock skew being well under the 12h rejection threshold: %w", baseline, err)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-AGENT-005",
		Title:    "UPDATE agents SET revoked_at = now(): next push returns 401, agent stops collecting",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *Env) error {
			var agentID string
			resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := poll(resolveCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				err := e.DB.QueryRow(ctx, `SELECT agent_id::text FROM instances ORDER BY last_seen DESC LIMIT 1`).Scan(&agentID)
				return err == nil, err
			}); err != nil {
				return fmt.Errorf("resolve monitored instance's agent_id: %w", err)
			}

			var baseline int
			if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics`).Scan(&baseline); err != nil {
				return fmt.Errorf("query baseline metrics count: %w", err)
			}

			if _, err := e.DB.Exec(ctx, `UPDATE agents SET revoked_at = now() WHERE agent_id = $1::uuid`, agentID); err != nil {
				return fmt.Errorf("revoke agent: %w", err)
			}
			revokeTS := time.Now()

			// internal/server/ingest.go's revocation check runs on every
			// push regardless of the shared bootstrap token, and
			// internal/agent/pusher.go's pushOne distinguishes the 401
			// body to set Health = "revoked" specifically (not just
			// "unauthorized") — /healthz's own state field mirrors that
			// Health value directly.
			stateCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
			defer cancel2()
			if err := poll(stateCtx, 2*time.Second, func(ctx context.Context) (bool, error) {
				health, err := e.AgentHealthz()
				if err != nil {
					return false, fmt.Errorf("agent /healthz errored: %w", err)
				}
				state, _ := health["state"].(string)
				return state == "revoked", nil
			}); err != nil {
				return fmt.Errorf("agent /healthz never reported state=revoked: %w", err)
			}

			// The agent stops collecting: cmd/pglens-agent/run.go's push
			// loop stops the scheduler once it observes Health ==
			// HealthRevoked, so no further envelope is ever built. Proven
			// as a genuine "Consistently," not a one-shot count: poll for
			// the full window and fail immediately the instant it grows,
			// rather than only checking once at the end (which would miss
			// a late, still-wrong delivery landing just before the check).
			consistentDeadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(consistentDeadline) {
				var n int
				if err := e.DB.QueryRow(ctx, `SELECT count(*) FROM metrics`).Scan(&n); err != nil {
					return fmt.Errorf("query metrics count during consistency window: %w", err)
				}
				if n > baseline {
					return fmt.Errorf("metrics count grew from %d to %d after revocation (ts >= %v) — the agent kept collecting/pushing after being revoked", baseline, n, revokeTS)
				}
				time.Sleep(3 * time.Second)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})
}
