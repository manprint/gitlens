//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

func init() {
	Register(Scenario{
		ID:       "SYS-NET-001",
		Title:    "server outage: buffered samples survive, none dropped, none duplicated",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-3"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
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

			// Keep one real client backend on the monitored PostgreSQL alive
			// across the outage. Without this, a quiet target can legitimately
			// have no pg_stat_activity client rows once the server-side
			// connections disappear, so the activity check emits no
			// pg_backends metric even though the agent continues collecting.
			// The held backend makes the backlog assertion about delivery and
			// original timestamps, not about incidental target concurrency.
			targetConn, err := e.PG("pg").Acquire(ctx)
			if err != nil {
				return fmt.Errorf("hold monitored PostgreSQL client: %w", err)
			}
			defer targetConn.Release()

			outageStart := time.Now().UTC()
			if err := e.Compose("stop", "pglens-server"); err != nil {
				return fmt.Errorf("stop pglens-server: %w", err)
			}

			// The plan's own text (phase_06.md) names
			// `agent_buffer_bytes` and `agent_samples_dropped_total`, which
			// the agent does not export. What is checkable: the agent's
			// /healthz buffer_stats (acked and dropped envelopes, where
			// dropped includes the disk buffer's own retention drops), and,
			// once the server returns, that the samples the agent kept
			// producing during the outage reach the server with their
			// original timestamps. I-3 (no duplicate (series_id,ts)) is
			// enforced by the schema's own unique index.
			outageDeadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(outageDeadline) {
				health, err := e.AgentHealthz()
				if err != nil {
					return fmt.Errorf("agent /healthz errored during the outage: %w", err)
				}
				stats, _ := health["buffer_stats"].(map[string]interface{})
				if dropped, ok := stats["dropped"].(float64); ok && dropped > 0 {
					return fmt.Errorf("agent_samples_dropped_total-equivalent (buffer_stats.dropped) is %v, want 0 during a 60s outage this small", dropped)
				}
				time.Sleep(5 * time.Second)
			}

			if err := e.Compose("start", "pglens-server"); err != nil {
				return fmt.Errorf("start pglens-server: %w", err)
			}

			// Wait for the backlog to drain. The agent queues envelopes in
			// its disk buffer and pushFromBuffer sends them in order, so a
			// sample with a post-recovery timestamp can only be delivered
			// after the backlog ahead of it. The drain is not instant: the
			// envelope in flight when the server went down keeps retrying
			// in pushOne with exponential backoff, which grows to about a
			// minute, so the backlog can take longer than the outage. The
			// scenario waits for that rather than asserting a drain speed
			// the design does not promise.
			recoverCtx, cancel2 := context.WithTimeout(ctx, 4*time.Minute)
			defer cancel2()
			recoveryTime := time.Now().UTC()
			if err := poll(recoverCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_backends' AND ts >= $2`,
					instanceID, recoveryTime).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("agent never resumed pushing after the server recovered: %w", err)
			}

			// Coverage across the whole outage: at least one pg_backends
			// sample (activity's 10s cadence) landed with a ts inside the
			// outage window, proving the queued backlog was delivered
			// rather than silently skipped once the server came back.
			var duringOutage int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_backends' AND ts >= $2 AND ts < $3`,
				instanceID, outageStart, recoveryTime).Scan(&duringOutage); err != nil {
				return fmt.Errorf("query outage-window samples: %w", err)
			}
			if duringOutage == 0 {
				return fmt.Errorf("no pg_backends samples carry a timestamp from during the outage — the backlog was not delivered, or its original timestamps were lost")
			}

			// Continuity across the outage. The activity check emits
			// pg_backends_waiting_ratio on every tick, whatever the backends
			// are doing, at the 5s interval the harness configures
			// (test/harness/harness.go, test/fixtures/agent-standalone.yaml).
			// A replay that skips a segment leaves a hole in these timestamps,
			// which the count above cannot see. Consecutive ticks must stay
			// within two intervals, and the ticks must reach both edges of
			// the outage window.
			const tick = 5 * time.Second
			tickRows, err := e.DB.Query(ctx,
				`SELECT DISTINCT ts FROM metrics WHERE instance_id=$1::uuid AND metric='pg_backends_waiting_ratio' AND ts >= $2 AND ts < $3 ORDER BY ts`,
				instanceID, outageStart, recoveryTime)
			if err != nil {
				return fmt.Errorf("query outage-window ticks: %w", err)
			}
			defer tickRows.Close()
			var ticks []time.Time
			for tickRows.Next() {
				var ts time.Time
				if err := tickRows.Scan(&ts); err != nil {
					return fmt.Errorf("scan outage-window tick: %w", err)
				}
				ticks = append(ticks, ts)
			}
			if err := tickRows.Err(); err != nil {
				return fmt.Errorf("read outage-window ticks: %w", err)
			}
			if len(ticks) == 0 {
				return fmt.Errorf("no activity tick falls inside the outage window")
			}
			if first := ticks[0].Sub(outageStart); first > 2*tick {
				return fmt.Errorf("the first activity tick of the outage is %v after it began: the backlog's first samples are missing", first)
			}
			for i := 1; i < len(ticks); i++ {
				if gap := ticks[i].Sub(ticks[i-1]); gap > 2*tick {
					return fmt.Errorf("no activity tick for %v between %s and %s: a span of the backlog was not delivered", gap, ticks[i-1].Format(time.RFC3339Nano), ticks[i].Format(time.RFC3339Nano))
				}
			}
			if last := recoveryTime.Sub(ticks[len(ticks)-1]); last > 2*tick {
				return fmt.Errorf("the last activity tick of the outage is %v before recovery: the backlog's final samples are missing", last)
			}

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-NET-003",
		Title:    "black hole between agent and PostgreSQL: agent stays responsive",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Smoke:    true,
		Expect:   Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
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

			remove, err := e.Toxic(LinkAgentToPG("pg"), ToxicTimeout{})
			if err != nil {
				return fmt.Errorf("inject black-hole toxic: %w", err)
			}
			removed := false
			defer func() {
				if !removed {
					_ = remove()
				}
			}()

			// Consistently, not once: /healthz must answer within 1s on
			// every single poll for the full black-hole duration. This is
			// the scenario's real payoff — per-check timeouts (4.2,
			// INT-SCHED-001) mean a dead PostgreSQL connection can never
			// block the agent's own liveness surface, no matter how long
			// the outage lasts.
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				start := time.Now()
				if _, err := e.AgentHealthz(); err != nil {
					return fmt.Errorf("agent /healthz errored during black hole: %w", err)
				}
				if elapsed := time.Since(start); elapsed > time.Second {
					return fmt.Errorf("agent /healthz took %v (>1s) to respond during the black hole — it may be hanging on the dead connection", elapsed)
				}
				time.Sleep(2 * time.Second)
			}

			// instance_unreachable/agent_down are driven by the SERVER
			// observing instances.last_seen go stale (internal/server/
			// staleness.go) — they detect the agent process itself going
			// silent, not one check's target becoming unreachable. The
			// agent keeps pushing (with per-check errors embedded in the
			// envelope, not omitted) throughout this scenario, so these
			// must NOT fire; conflating "one check's DB is unreachable"
			// with "the whole agent is down" would be the wrong behavior,
			// not the right one. (phase_06.md's original text for this
			// scenario named `instance_unreachable` as the expected event —
			// verified live this session to be structurally unreachable
			// from a PG-only fault, since nothing about a failed check
			// stops the agent from still pushing its other results; STATE.md
			// §8 records the correction.)
			var downCount int
			if err := e.DB.QueryRow(ctx,
				`SELECT count(*) FROM events WHERE instance_id=$1::uuid AND type IN ('instance_unreachable','agent_down')`,
				instanceID).Scan(&downCount); err != nil {
				return fmt.Errorf("query agent_down/instance_unreachable events: %w", err)
			}
			if downCount > 0 {
				return fmt.Errorf("unexpected agent_down/instance_unreachable event: a black-holed PG connection alone must not mark the agent down")
			}

			if err := remove(); err != nil {
				return fmt.Errorf("remove black-hole toxic: %w", err)
			}
			removed = true

			e.AssertInvariants(e.T)
			return nil
		},
	})

	Register(Scenario{
		ID:       "SYS-NET-005",
		Title:    "docker pause on PostgreSQL, then unpause: treated as unreachable, recovers cleanly, no duplicate rows",
		Topology: TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7", "I-3"},
		Expect:   Expectations{Events: []string{"agent_down", "agent_up"}, Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
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

			// `docker pause` freezes every process in the container at the
			// cgroup level (SIGSTOP-equivalent) — unlike SYS-NET-003's
			// toxic, which only black-holes the agent<->PG TCP link, a
			// paused PostgreSQL cannot answer ANY connection, including the
			// one RefreshRole itself uses. In this standalone topology that
			// is the agent's only target, so RefreshRole fails on every
			// tick and the instance is excluded from every envelope
			// (staleness.go's flushEnvelope skip, established fixing
			// orphan_standby earlier this session) — instances.last_seen
			// genuinely stops advancing, so unlike SYS-NET-003 this SHOULD
			// cross the staleness evaluator's threshold() (3x
			// defaultExpectedInterval, 90s default) and fire agent_down /
			// instance_unreachable. This is the behavioral distinction the
			// plan's own wording ("treated as unreachable") calls for.
			pauseStart := time.Now().UTC()
			if err := e.Compose("pause", "pg"); err != nil {
				return fmt.Errorf("pause pg: %w", err)
			}
			paused := true
			defer func() {
				if paused {
					_ = e.Compose("unpause", "pg")
				}
			}()

			downCtx, cancel2 := context.WithTimeout(ctx, 150*time.Second)
			defer cancel2()
			if err := poll(downCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE instance_id=$1::uuid AND type IN ('instance_unreachable','agent_down') AND ts >= $2`,
					instanceID, pauseStart).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("agent_down/instance_unreachable never fired while pg was paused: %w", err)
			}

			// The agent process itself is unaffected (only pg's container
			// is paused) — /healthz must keep answering throughout, same
			// liveness guarantee as SYS-NET-003.
			if _, err := e.AgentHealthz(); err != nil {
				return fmt.Errorf("agent /healthz errored while pg was paused: %w", err)
			}

			recoverTS := time.Now().UTC()
			if err := e.Compose("unpause", "pg"); err != nil {
				return fmt.Errorf("unpause pg: %w", err)
			}
			paused = false

			recoverCtx, cancel3 := context.WithTimeout(ctx, 150*time.Second)
			defer cancel3()
			if err := poll(recoverCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM events WHERE instance_id=$1::uuid AND type IN ('instance_reachable','agent_up') AND ts >= $2`,
					instanceID, recoverTS.Add(-1*time.Second)).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("agent_up/instance_reachable never fired after unpause: %w", err)
			}

			freshCtx, cancel4 := context.WithTimeout(ctx, 180*time.Second)
			defer cancel4()
			if err := poll(freshCtx, 3*time.Second, func(ctx context.Context) (bool, error) {
				var n int
				err := e.DB.QueryRow(ctx,
					`SELECT count(*) FROM metrics WHERE instance_id=$1::uuid AND metric='pg_backends' AND ts >= $2`,
					instanceID, recoverTS).Scan(&n)
				return err == nil && n > 0, err
			}); err != nil {
				return fmt.Errorf("no fresh pg_backends sample landed after unpause: %w", err)
			}

			// I-3 (no duplicate (series_id,ts)) is the strongest "no
			// duplicate rows after resume" check available: the schema's
			// own unique index enforces it, so AssertInvariants is the
			// authoritative assertion rather than an ad-hoc count query.
			e.AssertInvariants(e.T)
			return nil
		},
	})
}
