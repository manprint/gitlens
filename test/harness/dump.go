//go:build e2e

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// dumpContainerInspect writes docker inspect JSON for every container of the
// stack, stopped ones included, so the failure shows the state each container
// was left in. The compose files are passed explicitly for the same reason as
// dumpComposeLogs: without them, project discovery misses fragment services.
func dumpContainerInspect(h *Harness, artifactDir string, t *testing.T) {
	args := append([]string{"compose", "-p", h.projectName}, h.composeFiles()...)
	args = append(args, "ps", "-a", "-q")
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	ids, err := cmd.Output()
	if err != nil {
		t.Logf("docker compose ps failed: %v", err)
		return
	}
	containers := strings.Fields(string(ids))
	if len(containers) == 0 {
		return
	}
	// Inspect may fail for a container that vanished meanwhile; its error goes
	// to the log, and the JSON for the containers that answered is still kept.
	inspect := exec.Command("docker", append([]string{"inspect"}, containers...)...)
	output, err := inspect.Output()
	if err != nil {
		t.Logf("docker inspect failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "docker-inspect.json"), output, 0644); err != nil {
		t.Logf("failed to write docker-inspect.json: %v", err)
	}
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

// dumpTableRecentRows is how many of the newest rows dumpMetrics keeps per
// hypertable.
const dumpTableRecentRows = 100

// dumpMetrics writes, for every TimescaleDB hypertable, its row count and its
// newest rows to metrics-tables.json. It does not go through h.DB: a failure
// there calls t.Fatalf, which would stop the cleanup before the stack is torn
// down. It connects on its own and logs any error, so the rest of the dump
// still runs.
func dumpMetrics(h *Harness, artifactDir string, t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	port, err := h.getServicePort("timescaledb", 5432)
	if err != nil {
		t.Logf("metrics dump: resolve timescaledb port: %v", err)
		return
	}
	dsn := fmt.Sprintf("postgres://pglens:pglens@localhost:%d/pglens?sslmode=disable", port)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Logf("metrics dump: connect to timescaledb: %v", err)
		return
	}
	defer pool.Close()

	writeMetricTables(ctx, pool, artifactDir, t)
}

// tableDump is one hypertable's entry in metrics-tables.json.
type tableDump struct {
	Rows   int64             `json:"rows"`
	Recent []json.RawMessage `json:"recent"`
}

// writeMetricTables writes the row count and newest rows of every hypertable
// of the pool's schema to metrics-tables.json.
func writeMetricTables(ctx context.Context, pool *pgxpool.Pool, artifactDir string, t *testing.T) {
	tables := make(map[string]tableDump)

	names, err := hypertableNames(ctx, pool)
	if err != nil {
		t.Logf("metrics dump: list hypertables: %v", err)
	}
	for _, name := range names {
		quoted := pgx.Identifier{name}.Sanitize()
		var dump tableDump
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+quoted).Scan(&dump.Rows); err != nil {
			t.Logf("metrics dump: count %s: %v", name, err)
			continue
		}
		rows, err := pool.Query(ctx, fmt.Sprintf(
			"SELECT row_to_json(r)::text FROM (SELECT * FROM %s ORDER BY ts DESC LIMIT %d) r",
			quoted, dumpTableRecentRows))
		if err != nil {
			t.Logf("metrics dump: sample %s: %v", name, err)
			continue
		}
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Logf("metrics dump: scan %s: %v", name, err)
				break
			}
			dump.Recent = append(dump.Recent, json.RawMessage(row))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Logf("metrics dump: read %s: %v", name, err)
		}
		tables[name] = dump
	}
	writeJSON(filepath.Join(artifactDir, "metrics-tables.json"), tables, t)
}

// hypertableNames lists the TimescaleDB hypertables of the current schema.
func hypertableNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT hypertable_name FROM timescaledb_information.hypertables
		WHERE hypertable_schema = current_schema() ORDER BY hypertable_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
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
