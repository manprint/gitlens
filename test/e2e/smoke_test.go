//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

func init() {
	scenario.Register(scenario.Scenario{
		ID:       "SYS-HARNESS-001",
		Title:    "stack starts healthy, teardown leaves nothing behind",
		Topology: scenario.TopologyStandalone,
		Covers:   []string{"phase_06.md#5.7"},
		Smoke:    true,
		Expect:   scenario.Expectations{Invariants: []string{"I-1", "I-2", "I-3", "I-4"}},
		Run: func(ctx context.Context, e *scenario.Env) error {
			// The stack is already up (Harness.Start already waited for
			// health) by the time a scenario's Run is invoked — this
			// scenario's whole point is that reaching this line at all,
			// with a live DB and a responding API, already proves the
			// "starts healthy" half. The "teardown leaves nothing behind"
			// half is proven by the harness's own t.Cleanup after this test
			// function returns, not from inside Run.
			if err := e.API.WaitReadyz(0); err != nil {
				// WaitReadyz(0) with no time to poll still makes one real
				// request; a non-2xx here means the stack is not actually
				// healthy despite waitHealthy() saying so.
				return err
			}
			e.AssertInvariants(e.T)
			return nil
		},
	})
}

// TestSmoke_HarnessStartsHealthy runs SYS-HARNESS-001 against a real
// docker-compose stack: proof that Harness.Start -> Scenario -> Env wiring
// (5.6/5.7's foundational plumbing) works end to end, not just that it
// compiles.
func TestSmoke_HarnessStartsHealthy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-HARNESS-001")
}

// TestSmoke_TopologyReplicationEstablishes runs SYS-TOPO-001 (test/scenario/topo.go)
// against the primary-standby topology.
func TestSmoke_TopologyReplicationEstablishes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyPrimaryStandby,
	})
	h.Scenario(t, "SYS-TOPO-001")
}

// TestSmoke_RestartNoNegativeRate runs SYS-RESET-001 (test/scenario/reset.go).
func TestSmoke_RestartNoNegativeRate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-RESET-001")
}

// TestSmoke_AgentRestartKeepsInstanceID runs SYS-AGENT-001 (test/scenario/agent.go).
func TestSmoke_AgentRestartKeepsInstanceID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-AGENT-001")
}

// TestSmoke_PermTierT0Only runs SYS-PERM-001 (test/scenario/perm.go).
func TestSmoke_PermTierT0Only(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-PERM-001")
}

// TestSmoke_BlackHoleAgentStaysResponsive runs SYS-NET-003 (test/scenario/net.go).
func TestSmoke_BlackHoleAgentStaysResponsive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
		Toxiproxy: true,
	})
	h.Scenario(t, "SYS-NET-003")
}

// TestSmoke_ServerOutageBacklogSurvives runs SYS-NET-001 (test/scenario/net.go).
func TestSmoke_ServerOutageBacklogSurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E smoke test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainer,
	})
	h.Scenario(t, "SYS-NET-001")
}
