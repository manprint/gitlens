//go:build e2e

package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Dump writes artifacts to test/e2e/_artifacts on test failure.
// Called from Harness cleanup after test completion; only writes if test failed.
func (h *Harness) Dump(t *testing.T) {
	t.Helper()
	if !t.Failed() {
		return
	}

	// Anchored to this source file's own directory (like composeDirectory()),
	// not the test binary's working directory: `go test ./test/e2e/...` runs
	// with cwd already inside test/e2e, so a "test/e2e/_artifacts"-relative
	// path landed under the wrong, doubled test/e2e/test/e2e/_artifacts/.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "../..")
	artifactDir := filepath.Join(repoRoot, "test/e2e/_artifacts", t.Name())
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		t.Logf("failed to create artifact directory: %v", err)
		return
	}

	// docker compose logs — one file per service
	dumpComposeLogs(h, artifactDir, t)

	// docker inspect — full container state JSON
	dumpContainerInspect(h, artifactDir, t)

	// API endpoints: /api/v1/clusters, /api/v1/events, /healthz — only if
	// Start() got far enough to construct a client. A failure early in
	// Start() (e.g. `docker compose up` itself failing) means there is no
	// live server to ask, and h.apiClient is still nil.
	if h.apiClient != nil {
		dumpAPIClusters(h, artifactDir, t)
		dumpAPIEvents(h, artifactDir, t)
		dumpAgentHealth(h, artifactDir, t)
	}

	// pglens-agent check output and its own stdout/stderr log (binary mode only)
	if h.agentMode == AgentModeBinary {
		dumpAgentCheck(h, artifactDir, t)
		dumpAgentBinaryLog(h, artifactDir, t)
	}

	// Metric table row counts and recent rows
	dumpMetrics(h, artifactDir, t)
}

// dumpComposeLogs writes the output of docker compose logs to a file. The
// compose files must be passed explicitly (like every other docker compose
// invocation in this package) — without them, `docker compose logs` only
// returns whatever subset of services its own project-label discovery
// happens to find (empirically: just the two services defined in the
// FIRST -f file passed to `up`, silently dropping every service the
// topology/agent fragment files add), which made every failure dump
// missing exactly the agent/server logs a debugging session needs most.
func dumpComposeLogs(h *Harness, artifactDir string, t *testing.T) {
	args := append([]string{"compose", "-p", h.projectName}, h.composeFiles()...)
	args = append(args, "logs")
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("docker compose logs failed: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "docker-compose.log"), output, 0644); err != nil {
		t.Logf("failed to write docker-compose.log: %v", err)
	}
}

// dumpContainerInspect writes docker inspect JSON for all containers.
func dumpContainerInspect(h *Harness, artifactDir string, t *testing.T) {
	cmd := exec.Command("docker", "compose", "-p", h.projectName, "ps", "-a", "--format", "{{.Names}}")
	cmd.Dir = h.composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	// For each container, run docker inspect
	// Placeholder: collect all and write to file
	_ = output
}

// dumpAPIClusters writes /api/v1/clusters response.
func dumpAPIClusters(h *Harness, artifactDir string, t *testing.T) {
	clusters, err := h.apiClient.Clusters()
	if err != nil {
		t.Logf("failed to fetch /api/v1/clusters: %v", err)
		return
	}
	writeJSON(filepath.Join(artifactDir, "api-clusters.json"), clusters, t)
}

// dumpAPIEvents writes /api/v1/events response.
func dumpAPIEvents(h *Harness, artifactDir string, t *testing.T) {
	events, err := h.apiClient.Events()
	if err != nil {
		t.Logf("failed to fetch /api/v1/events: %v", err)
		return
	}
	writeJSON(filepath.Join(artifactDir, "api-events.json"), events, t)
}

// dumpAgentHealth writes the agent's own /healthz response (not the
// server's — see Harness.AgentHealthz).
func dumpAgentHealth(h *Harness, artifactDir string, t *testing.T) {
	health, err := h.AgentHealthz()
	if err != nil {
		t.Logf("failed to fetch agent /healthz: %v", err)
		return
	}
	writeJSON(filepath.Join(artifactDir, "agent-health.json"), health, t)
}

// dumpAgentCheck writes `pglens-agent check` output (AgentModeBinary only —
// runs the already-built subprocess binary directly against its own target
// DSN; there is no compose service to `docker compose exec` into in this
// mode, unlike every other AgentMode variant).
func dumpAgentCheck(h *Harness, artifactDir string, t *testing.T) {
	if h.agentBinaryPath == "" {
		return
	}
	cmd := exec.Command(h.agentBinaryPath, "check", "--dsn", h.agentBinaryDSN)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("pglens-agent check failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "agent-check.log"), output, 0644); err != nil {
		t.Logf("failed to write agent-check.log: %v", err)
	}
}

// dumpAgentBinaryLog copies the binary-mode agent subprocess's own captured
// stdout/stderr into the artifact directory — the equivalent of
// dumpComposeLogs for a process that was never a docker-compose service.
func dumpAgentBinaryLog(h *Harness, artifactDir string, t *testing.T) {
	if h.agentLogPath == "" {
		return
	}
	data, err := os.ReadFile(h.agentLogPath)
	if err != nil {
		t.Logf("failed to read agent binary log: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "agent-binary.log"), data, 0644); err != nil {
		t.Logf("failed to write agent-binary.log: %v", err)
	}
}

// dumpMetrics writes metric table statistics (row counts, sample rows).
func dumpMetrics(h *Harness, artifactDir string, t *testing.T) {
	// Placeholder: query TimescaleDB metric tables (if accessible via DB())
	// and write table sizes and sample rows to artifactDir
}

// writeJSON marshals and writes JSON to a file.
func writeJSON(path string, data interface{}, t *testing.T) {
	file, err := os.Create(path)
	if err != nil {
		t.Logf("failed to create %s: %v", path, err)
		return
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		t.Logf("failed to marshal JSON to %s: %v", path, err)
	}
}
