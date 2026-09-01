//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/manprint/pglens/test/harness"
	"github.com/manprint/pglens/test/scenario"
)

const (
	uiE2EPassword    = "pglens-ui-e2e"
	uiBootstrapToken = "dev-token"
)

type testingLogWriter struct {
	t *testing.T
}

func (w testingLogWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.t.Log(line)
		}
	}
	return len(p), nil
}

func init() {
	scenario.Register(scenario.Scenario{
		ID:       "SYS-UI-000",
		Title:    "server serves the authenticated application shell",
		Topology: scenario.TopologyPrimaryStandby,
		Covers:   []string{"phase_18.md#17.1"},
		Expect:   scenario.Expectations{Invariants: []string{"I-1"}},
		Run: func(ctx context.Context, e *scenario.Env) error {
			cmd := exec.CommandContext(ctx, "pnpm", "exec", "playwright", "test")
			cmd.Dir = repositoryRoot(e.T) + "/web"
			cmd.Env = os.Environ()
			logWriter := testingLogWriter{t: e.T}
			cmd.Stdout = io.MultiWriter(logWriter)
			cmd.Stderr = io.MultiWriter(logWriter)
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("pnpm exec playwright test: %w", err)
			}
			e.AssertInvariants(e.T)
			return nil
		},
	})
}

// TestUI_Smoke wires the real server and primary-standby agent stack to the
// Playwright suite. The API poll proves the UI is tested against collected
// data, not merely a healthy but empty server.
func TestUI_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI E2E test in short mode")
	}

	t.Setenv("PGLENS_UI_PASSWORD", uiE2EPassword)
	h := harness.Start(t, harness.Config{
		Topology:  harness.TopologyPrimaryStandby,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
		UI:        true,
	})

	harness.Eventually(t, 90*time.Second, func() error {
		clusters, err := h.API().Clusters()
		if err != nil {
			return err
		}
		if len(clusters) == 0 {
			return fmt.Errorf("API returned no clusters yet")
		}
		return nil
	})

	t.Setenv("PGLENS_UI_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d", h.ServerPort()))
	t.Setenv("PGLENS_BOOTSTRAP_TOKEN", uiBootstrapToken)
	h.Scenario(t, "SYS-UI-000")
}
