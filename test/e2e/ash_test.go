//go:build e2e

package e2e

import (
	"testing"

	"github.com/manprint/pglens/test/harness"
)

// TestFull_ASHConservation runs SYS-ASH-001 (test/scenario/ash.go) — 10 known
// concurrent sessions, asserting the sum of ASH samples over the window
// conserves against ticks x observed sessions within a 10% tolerance, end to
// end through the real sampler+aggregator pipeline (cmd/pglens-agent/ash.go).
func TestFull_ASHConservation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology: harness.TopologyStandalone,
	})
	h.Scenario(t, "SYS-ASH-001")
}

// TestFull_ASHDisabled runs SYS-ASH-002 (test/scenario/ash.go) — with ASH
// disabled in the agent's own config, no metrics_ash rows are ever written
// and /api/v1/ash reports an explicit enabled:false rather than an empty
// result indistinguishable from an idle database.
func TestFull_ASHDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: harness.AgentModeContainerASHDisabled,
	})
	h.Scenario(t, "SYS-ASH-002")
}
