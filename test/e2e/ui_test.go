//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	registerUIScenario("SYS-UI-000", "server serves the authenticated application shell", scenario.TopologyPrimaryStandby, "phase_18.md#17.1")
	registerUIScenario("SYS-UI-001", "failover preserves identity and raises an alert", scenario.TopologyPrimaryStandby, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-002", "unauthenticated navigation cannot restore fleet data", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-003", "agent outage is visible as stale fleet data", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-004", "standalone replay lag stays unknown", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-005", "disabled ASH explains its configuration", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-006", "plan-only execution is audited without query text", scenario.TopologyPrimaryStandby, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-007", "replication and ASH charts render data", scenario.TopologyPrimaryStandby, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-008", "blocking tree shows the lock root and child", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-009", "signal controls enforce tier and confirmation", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-010", "finding mute and unmute update state", scenario.TopologyStandalone, "phase_18.md#17.2")
	registerUIScenario("SYS-UI-011", "phase seven routes pass accessibility checks", scenario.TopologyStandalone, "phase_18.md#17.2")
}

func registerUIScenario(id, title string, topology scenario.Topology, cover string) {
	scenario.Register(scenario.Scenario{
		ID:       id,
		Title:    title,
		Topology: topology,
		Covers:   []string{cover},
		Expect:   scenario.Expectations{Invariants: []string{"I-1"}},
		Run: func(ctx context.Context, e *scenario.Env) error {
			workloadDone, err := prepareUIScenario(e, id)
			if err != nil {
				return err
			}
			if err := runUISpec(ctx, e, id); err != nil {
				if workloadDone != nil {
					<-workloadDone
				}
				return err
			}
			if workloadDone != nil {
				if err := <-workloadDone; err != nil {
					return fmt.Errorf("UI workload: %w", err)
				}
			}
			e.AssertInvariants(e.T)
			return nil
		},
	})
}

// prepareUIScenario performs only the external state transition that the
// browser test cannot perform itself. The browser then polls the resulting UI
// state instead of sleeping for a guessed propagation interval.
func prepareUIScenario(e *scenario.Env, id string) (chan error, error) {
	switch id {
	case "SYS-UI-003":
		if err := e.Compose("stop", "pglens-agent"); err != nil {
			return nil, fmt.Errorf("stop pglens-agent: %w", err)
		}
	case "SYS-UI-008":
		return startUIWorkload(e, "pg", "lock-storm", "--sessions", "8", "--duration", "30s"), nil
	case "SYS-UI-006":
		return startUIWorkload(e, "pg-standby", "slow-query", "--sleep", "30s", "--count", "1"), nil
	case "SYS-UI-009":
		target := "pg-primary"
		if os.Getenv("PGLENS_UI_CANCEL_MODE") == "t0" {
			target = "pg"
		}
		return startUIWorkload(e, target, "slow-query", "--sleep", "45s", "--count", "1"), nil
	}
	return nil, nil
}

func startUIWorkload(e *scenario.Env, target string, command string, args ...string) chan error {
	cfg := e.PG(target).Config().ConnConfig
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	workloadArgs := append([]string{command, "--dsn", dsn}, args...)
	done := make(chan error, 1)
	go func() {
		_, err := e.Workload(workloadArgs...)
		done <- err
	}()
	return done
}

func runUISpec(ctx context.Context, e *scenario.Env, id string) error {
	root := repositoryRoot(e.T)
	cmd := exec.CommandContext(ctx, "pnpm", "exec", "playwright", "test", "--grep", id)
	cmd.Dir = filepath.Join(root, "web")
	cmd.Env = os.Environ()
	logWriter := testingLogWriter{t: e.T}
	cmd.Stdout = io.MultiWriter(logWriter)
	cmd.Stderr = io.MultiWriter(logWriter)

	if id != "SYS-UI-001" {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pnpm exec playwright test --grep %s: %w", id, err)
		}
		return nil
	}

	readyFile := filepath.Join(e.T.TempDir(), "ui-ready")
	e.T.Setenv("PGLENS_UI_READY_FILE", readyFile)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Playwright %s: %w", id, err)
	}
	if err := waitForFile(ctx, readyFile); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("wait for Playwright %s readiness: %w", id, err)
	}
	if err := e.Compose("kill", "-s", "SIGKILL", "pg-primary"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("kill pg-primary: %w", err)
	}
	if _, err := e.ExecAs("pg-standby", "postgres", "pg_ctl", "promote", "-D", "/var/lib/postgresql/data"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("promote pg-standby: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("pnpm exec playwright test --grep %s: %w", id, err)
	}
	return nil
}

func waitForFile(ctx context.Context, path string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func startUIHarness(t *testing.T, cfg harness.Config) *harness.Harness {
	t.Helper()
	t.Setenv("PGLENS_UI_PASSWORD", uiE2EPassword)
	h := harness.Start(t, cfg)
	harness.Eventually(t, 90*time.Second, func() error {
		clusters, err := h.API().Clusters()
		if err != nil {
			return err
		}
		if len(clusters) == 0 {
			return fmt.Errorf("API returned no clusters yet")
		}
		for _, cluster := range clusters {
			if fmt.Sprint(cluster["health"]) == "ok" {
				return nil
			}
		}
		return fmt.Errorf("API returned no healthy clusters yet: %v", clusters)
	})
	t.Setenv("PGLENS_UI_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d", h.ServerPort()))
	t.Setenv("PGLENS_BOOTSTRAP_TOKEN", uiBootstrapToken)
	return h
}

func runUI(t *testing.T, h *harness.Harness, id string) {
	t.Helper()
	h.Scenario(t, id)
}

// TestUI_Smoke wires the real server and primary-standby agent stack to the
// Playwright suite. The API poll proves the UI is tested against collected
// data, not merely a healthy but empty server.
func TestUI_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping UI E2E test in short mode")
	}
	h := startUIHarness(t, harness.Config{
		Topology:  harness.TopologyPrimaryStandby,
		AgentMode: resolveAgentMode(harness.AgentModeContainer),
		UI:        true,
	})
	runUI(t, h, "SYS-UI-000")
}

func TestUI_Failover(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyPrimaryStandby, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-001")
}

func TestUI_UnauthenticatedNavigation(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-002")
}

func TestUI_AgentOutage(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-003")
}

func TestUI_StandaloneLag(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-004")
}

func TestUI_DisabledASH(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: harness.AgentModeContainerASHDisabled, UI: true})
	runUI(t, h, "SYS-UI-005")
}

func TestUI_PlanOnly(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyPrimaryStandby, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-006")
}

func TestUI_Charts(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyPrimaryStandby, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-007")
}

func TestUI_BlockingTree(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-008")
}

func TestUI_Cancel(t *testing.T) {
	t.Setenv("PGLENS_UI_CANCEL_MODE", "t0")
	t0 := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, t0, "SYS-UI-009")

	t.Setenv("PGLENS_UI_CANCEL_MODE", "t2")
	t2 := startUIHarness(t, harness.Config{Topology: harness.TopologyPrimaryStandby, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, t2, "SYS-UI-009")
}

func TestUI_FindingMute(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-010")
}

func TestUI_Accessibility(t *testing.T) {
	h := startUIHarness(t, harness.Config{Topology: harness.TopologyStandalone, AgentMode: resolveAgentMode(harness.AgentModeContainer), UI: true})
	runUI(t, h, "SYS-UI-011")
}
