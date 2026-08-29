//go:build e2e

package e2e

import (
	"testing"

	"github.com/manprint/pglens/test/harness"
)

// TestFull_CascadingReplicationTopology runs SYS-REPL-006: a real
// primary -> standby A -> standby B chain must preserve B's upstream after A
// stops, exposing A as down rather than silently reparenting B.
func TestFull_CascadingReplicationTopology(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyCascading,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
	})
	h.Scenario(t, "SYS-REPL-006")
}
