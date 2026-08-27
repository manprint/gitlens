//go:build e2e

package e2e

import (
	"testing"

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
