//go:build e2e

package e2e

import (
	"testing"

	"github.com/manprint/pglens/test/harness"
)

func runAlertingScenario(t *testing.T, id string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping L3 alerting test in short mode")
	}
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyStandalone,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
		Alerting:  true,
	})
	h.Scenario(t, id)
}

func TestFull_AlertEpisode(t *testing.T)        { runAlertingScenario(t, "SYS-ALERT-001") }
func TestFull_AlertRetry(t *testing.T)          { runAlertingScenario(t, "SYS-ALERT-002") }
func TestFull_AlertLeaderFailover(t *testing.T) { runAlertingScenario(t, "SYS-ALERT-003") }
func TestFull_AlertSilence(t *testing.T)        { runAlertingScenario(t, "SYS-ALERT-004") }
