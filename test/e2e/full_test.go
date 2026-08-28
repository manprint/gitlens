//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/test/harness"
)

// TestFull_DatabaseBudget runs SYS-DB-001 (test/scenario/db.go). Not part of
// the smoke suite (make test-e2e): it takes longer (15 databases created and
// exercised one at a time) and covers a secondary invariant, not the
// acceptance-critical path.
func TestFull_DatabaseBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-DB-001")
}

// TestFull_StatStatementsResetDiscardsInterval runs SYS-RESET-002
// (test/scenario/reset.go) — pg_stat_statements_reset() from an external
// session discards that interval's delta, counter_reset_detected names the
// affected metric, and the next interval produces a normal rate again.
func TestFull_StatStatementsResetDiscardsInterval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyStandalone,
	})
	h.Scenario(t, "SYS-RESET-002")
}

// TestFull_AgentRecreateWithoutVolumeNewIdentity runs SYS-AGENT-002
// (test/scenario/agent.go) — asserts the documented failure mode, not
// success (see the scenario's own comment).
func TestFull_AgentRecreateWithoutVolumeNewIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainerNoVolume,
	})
	h.Scenario(t, "SYS-AGENT-002")
}

// TestFull_SplitBrainWithoutStoppingPrimary runs SYS-REPL-002
// (test/scenario/replication.go) — promotes the standby while the primary
// keeps running, asserting split_brain_detected and health=critical rather
// than a clean failover (SYS-REPL-001's case).
func TestFull_SplitBrainWithoutStoppingPrimary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-REPL-002")
}

// TestFull_OrphanStandbyAfterPrimaryStopped runs SYS-REPL-003
// (test/scenario/replication.go) — stops the primary and leaves the standby
// unpromoted: asserts orphan_standby and no_primary_in_cluster rather than
// any role change. Long-running (up to 4 minutes): PostgreSQL's own
// wal_receiver_timeout, the topology engine's orphan window, and
// staleness.go's no_primary_in_cluster threshold each independently gate on
// ~60s with no shared clock.
func TestFull_OrphanStandbyAfterPrimaryStopped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-REPL-003")
}

// TestFull_ReplicationDelayLagRisesThenClears runs SYS-REPL-004
// (test/scenario/replication.go) — recovery_min_apply_delay makes
// replay_lag_sec rise on the standby and health degrade, then clearing the
// delay proves the lag genuinely returns to 0, not just settles naturally.
func TestFull_ReplicationDelayLagRisesThenClears(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-REPL-004")
}

// TestFull_InactiveSlotRetainsWALAndFires runs SYS-SLOT-001
// (test/scenario/replication.go) — stops the standby (keeping its slot),
// asserts the slot reports inactive, slot_retained_bytes grows, and
// slot_inactive fires after internal/server/staleness.go's 30s window.
func TestFull_InactiveSlotRetainsWALAndFires(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-SLOT-001")
}

// TestFull_DoubleRoleReversal runs SYS-REPL-005 (test/scenario/replication.go)
// — a real failover, then rebuilds the old primary as a fresh standby of the
// new one (wipe + pg_basebackup against the same named volume, since the
// compose service itself has no role-swap script), asserting identity and
// cluster_id survive both reversals and only one failover_detected exists.
func TestFull_DoubleRoleReversal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-REPL-005")
}

// TestFull_PgPausedTreatedAsUnreachable runs SYS-NET-005
// (test/scenario/net.go) — `docker pause` freezes PostgreSQL at the cgroup
// level, unlike SYS-NET-003's black-hole toxic; asserts the instance
// genuinely crosses the staleness threshold (agent_down/instance_unreachable
// fire), then recovers cleanly after unpause (agent_up/instance_reachable,
// fresh samples resume) with no duplicate rows (I-3).
func TestFull_PgPausedTreatedAsUnreachable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-NET-005")
}

// TestFull_BufferFillsAndDropsWithoutCrashing runs SYS-AGENT-003
// (test/scenario/agent.go) — a real (tiny) tmpfs genuinely fills from
// ordinary agent traffic while the server is down; asserts
// buffer_stats.dropped grows, the agent never crashes, and PostgreSQL is
// unaffected. Deliberately does NOT assert the backlog ever drains once the
// server is back — see the scenario's own comment (V001-F12, STATE.md §9):
// once a buffer this small hits real ENOSPC, it can never accept another
// write until the agent process itself restarts.
func TestFull_BufferFillsAndDropsWithoutCrashing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainerTinyBuffer,
	})
	h.Scenario(t, "SYS-AGENT-003")
}

// TestFull_ClockSkewDetectedSamplesStillAccepted runs SYS-AGENT-004
// (test/scenario/agent.go) — the agent's own clock (injected via
// PGLENS_DEBUG_CLOCK_OFFSET, not the container's real time) is 5 minutes
// behind; asserts clock_skew_seconds settles around 300, a one-time warning
// is logged, and samples keep landing (12h maxSampleAge is nowhere close).
func TestFull_ClockSkewDetectedSamplesStillAccepted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainerClockSkew,
	})
	h.Scenario(t, "SYS-AGENT-004")
}

// TestFull_RevokedAgentStopsCollecting runs SYS-AGENT-005
// (test/scenario/agent.go) — UPDATE agents SET revoked_at = now() ->
// the next push returns 401, /healthz reports state=revoked, and no further
// metrics land for a full 30s consistency window.
func TestFull_RevokedAgentStopsCollecting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-AGENT-005")
}

// TestFull_LockStormActivityKeepsAdvancing runs SYS-LOAD-002
// (test/scenario/load.go) — workloadctl lock-storm --sessions 50 piles up
// 50 sessions on one contended row; asserts the activity check's own
// timeout keeps it scraping throughout (IDEA.md#4.4's concrete payoff) and
// pg_max_xact_age_seconds reflects the contention.
func TestFull_LockStormActivityKeepsAdvancing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-LOAD-002")
}

// TestFull_RaceTransactionIDContention runs SYS-LOAD-005
// (test/scenario/ash.go) — workloadctl race --workers 20 --rows 100 (a new
// test/workload subcommand, since 'race' did not previously exist) creates
// UPDATE-UPDATE contention on a shared row pool; asserts the dominant ASH
// wait event is Lock/transactionid and avg_active_sessions rises above 5.
func TestFull_RaceTransactionIDContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyStandalone,
	})
	h.Scenario(t, "SYS-LOAD-005")
}

// TestFull_AgentModeBinaryReachesServer proves phase_06.md's own minimum
// AgentModeBinary criterion directly: "all three AGENT_MODE values bring up
// a stack whose agent reaches the server." AgentModeBinary's compose
// fragment (agent-binary.yml) deliberately defines no pglens-agent service —
// until this session, nothing ever actually spawned the agent binary as a
// host subprocess in this mode, so the agent simply never ran and no
// scenario ever noticed because every existing scenario uses
// AgentModeContainer. This does not attempt phase_06.md's larger,
// not-yet-built vision of running the WHOLE suite under both AGENT_MODE
// values via a CI matrix (no such wiring exists in the Makefile or test
// code) — it verifies the underlying capability genuinely works: a real
// `go build`-produced pglens-agent binary, run with `os/exec` against the
// stack's published ports, registers an instance with the server exactly
// like the containerized agent does.
func TestFull_AgentModeBinaryReachesServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeBinary,
	})

	db := h.DB(t)
	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var instanceID string
	for {
		err := db.QueryRow(context.Background(), `SELECT instance_id::text FROM instances LIMIT 1`).Scan(&instanceID)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("binary-mode agent never registered an instance within 30s: %v", err)
		}
		<-ticker.C
	}

	health, err := h.AgentHealthz()
	if err != nil {
		t.Fatalf("binary-mode agent /healthz unreachable: %v", err)
	}
	if health["state"] == nil {
		t.Fatalf("binary-mode agent /healthz missing 'state' field: %+v", health)
	}
}
