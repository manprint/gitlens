//go:build e2e

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/test/scenario"
)

// Topology specifies the PostgreSQL deployment shape.
type Topology string

const (
	TopologyStandalone     Topology = "standalone"
	TopologyPrimaryStandby Topology = "primary-standby"
)

// AgentMode specifies how the agent is deployed.
type AgentMode string

const (
	AgentModeContainer            AgentMode = "container"
	AgentModeBinary               AgentMode = "binary"
	AgentModeContainerNoVolume    AgentMode = "container-no-volume"
	AgentModeContainerASHDisabled AgentMode = "container-ash-disabled"
	AgentModeContainerTinyBuffer  AgentMode = "container-tiny-buffer"
	AgentModeContainerClockSkew   AgentMode = "container-clock-skew"
)

// Config configures a Harness.
type Config struct {
	Topology  Topology
	PGVersion int // default from PGLENS_PG_VERSIONS
	AgentMode AgentMode
	Toxiproxy bool
	Pgbouncer bool // reserved; not used in this plan
}

// Harness orchestrates an E2E stack via docker-compose.
type Harness struct {
	t                 *testing.T
	projectName       string
	baseFile          string
	topologyFile      string
	agentFile         string
	toxiproxyFile     string // "" unless Config.Toxiproxy is set
	composeDir        string
	containerName     string
	serverPort        int
	agentPort         int
	topology          Topology
	agentMode         AgentMode
	agentCmd          *exec.Cmd // non-nil only for AgentModeBinary: the running host subprocess
	agentLogFile      *os.File  // kept open until cleanup() closes it, after Dump has read agentLogPath
	agentRunDir       string    // os.MkdirTemp'd (not h.t.TempDir(): see startAgentBinary), removed by cleanup()
	agentBinaryPath   string    // built cmd/pglens-agent binary path, AgentModeBinary only
	agentBinaryDSN    string    // the DSN the binary agent itself uses to reach "pg", AgentModeBinary only
	agentLogPath      string    // stdout/stderr capture file for the binary-mode subprocess
	agentConfigPath   string    // agent.yaml config file path, AgentModeBinary only, preserved across restarts
	agentIdentityPath string    // identity.json file path, AgentModeBinary only, preserved across restarts
	agentPrimaryPort  int       // host port for "pg"/"pg-primary" baked into the currently-running agent config
	agentStandbyPort  int       // host port for "pg-standby" baked into the currently-running agent config (0 for standalone)
	agentServerPort   int       // host port for pglens-server baked into the currently-running agent config
	pools             map[string]*pgxpool.Pool
	poolMu            sync.Mutex
	apiClient         *APIClient
	workloadBin       string
	workloadMu        sync.Mutex
	cleaned           bool
	cleanMu           sync.Mutex
}

// Start brings up the stack and blocks until every service is healthy.
// It registers teardown on t.Cleanup, so a panicking test never leaks containers.
func Start(t *testing.T, cfg Config) *Harness {
	t.Helper()

	projectName := fmt.Sprintf("pglens-%s", uuid.New().String()[:8])
	composeDir := composeDirectory()

	// base.yml (timescaledb + pglens-server) is always included: every
	// topology and agent fragment depends on the server it defines.
	baseFile := filepath.Join(composeDir, "base.yml")

	// Determine topology and agent compose files
	var topologyFile string
	if cfg.Topology == TopologyPrimaryStandby {
		topologyFile = filepath.Join(composeDir, "topo-primary-standby.yml")
	} else {
		topologyFile = filepath.Join(composeDir, "topo-standalone.yml")
	}

	agentFile := filepath.Join(composeDir, "agent-container.yml")
	if cfg.AgentMode == AgentModeBinary {
		agentFile = filepath.Join(composeDir, "agent-binary.yml")
	} else if cfg.AgentMode == AgentModeContainerNoVolume {
		agentFile = filepath.Join(composeDir, "agent-container-no-volume.yml")
	} else if cfg.AgentMode == AgentModeContainerASHDisabled {
		agentFile = filepath.Join(composeDir, "agent-container-ash-disabled.yml")
	} else if cfg.AgentMode == AgentModeContainerTinyBuffer {
		agentFile = filepath.Join(composeDir, "agent-container-tiny-buffer.yml")
	} else if cfg.AgentMode == AgentModeContainerClockSkew {
		agentFile = filepath.Join(composeDir, "agent-container-clock-skew.yml")
	} else if cfg.Toxiproxy {
		// Points the agent's target DSN at toxiproxy's proxy instead of "pg"
		// directly (test/fixtures/agent-standalone-toxic.yaml) — without
		// this, InitToxiproxy()'s proxies exist but nothing ever sends
		// traffic through them. Toxiproxy fault injection is only wired up
		// for AgentModeContainer/"" (the default) currently.
		agentFile = filepath.Join(composeDir, "agent-container-toxic.yml")
	} else if cfg.Topology == TopologyPrimaryStandby {
		// agent-standalone.yaml's single "pg" target doesn't exist as a
		// service in this topology at all (it's "pg-primary"/"pg-standby")
		// — found live running SYS-REPL-001: the agent silently monitored
		// nothing for the entire primary-standby topology all session,
		// with no error beyond one stderr warning after waitForTarget's
		// 60s timeout (cmd/pglens-agent/run.go), because every dependent
		// scenario before this one asserted only against PostgreSQL
		// directly (e.PG), never against agent-derived data.
		agentFile = filepath.Join(composeDir, "agent-container-primary-standby.yml")
	}

	var toxiproxyFile string
	if cfg.Toxiproxy {
		toxiproxyFile = filepath.Join(composeDir, "toxiproxy.yml")
	}

	h := &Harness{
		t:             t,
		projectName:   projectName,
		baseFile:      baseFile,
		topologyFile:  topologyFile,
		agentFile:     agentFile,
		toxiproxyFile: toxiproxyFile,
		composeDir:    composeDir,
		topology:      cfg.Topology,
		agentMode:     cfg.AgentMode,
		pools:         make(map[string]*pgxpool.Pool),
	}

	// Register cleanup before attempting to start. Dump must run BEFORE
	// cleanup: it inspects the still-running (or just-exited) containers
	// via `docker compose logs`/`ps`/API calls — cleanup's `docker compose
	// down -v` removes them, so calling it first made every failure
	// artifact come back empty regardless of what actually went wrong.
	t.Cleanup(func() {
		h.Dump(t)
		h.cleanup()
	})

	// Bring up the stack
	if err := h.composeUp(); err != nil {
		t.Fatalf("failed to start stack: %v", err)
	}

	// Wait for every service docker-compose reports to reach "healthy"
	// (or "running" for services with no healthcheck defined).
	if err := h.waitHealthy(); err != nil {
		t.Fatalf("stack failed health check: %v", err)
	}

	port, err := h.getServicePort("pglens-server", 8080)
	if err != nil {
		t.Fatalf("resolve pglens-server port: %v", err)
	}
	h.serverPort = port

	if cfg.AgentMode == AgentModeBinary {
		// agent-binary.yml deliberately defines no pglens-agent service (see
		// its own comment), so there is no container port to resolve —
		// startAgentBinary spawns the real host subprocess and sets
		// h.agentPort itself, once its healthz port is actually known.
		if err := h.startAgentBinary(); err != nil {
			t.Fatalf("start agent binary: %v", err)
		}
	} else if cfg.AgentMode != "" {
		if agentPort, err := h.getServicePort("pglens-agent", 9187); err == nil {
			h.agentPort = agentPort
		}
		// A missing agent port is tolerated: some scenarios bring up the
		// server/database only and drive the agent as a binary subprocess
		// (AgentModeBinary) rather than through the compose-managed container.
	}

	if cfg.Toxiproxy {
		if err := h.InitToxiproxy(); err != nil {
			t.Fatalf("init toxiproxy: %v", err)
		}
	}

	h.apiClient = &APIClient{
		client:     &http.Client{Timeout: 10 * time.Second},
		baseURL:    fmt.Sprintf("http://localhost:%d", h.serverPort),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	return h
}

// startAgentBinary builds cmd/pglens-agent (once per Harness — there is only
// ever one binary-mode agent per stack) and runs it as a real host
// subprocess against the stack's already-published ports, rather than as a
// docker-compose service. This is the actual implementation
// test/compose/agent-binary.yml's own comment promises but that, until now,
// nothing provided — AgentModeBinary picked the (deliberately empty) compose
// fragment but nothing ever called os/exec, so the agent simply never ran at
// all in this mode. Supports both TopologyStandalone (single "pg" target) and
// TopologyPrimaryStandby (two targets: "pg-primary" and "pg-standby").
func (h *Harness) startAgentBinary() error {
	// A plain os.MkdirTemp, not h.t.TempDir(): TempDir()'s own auto-cleanup
	// is a t.Cleanup registered the moment it's called here, deep inside
	// Start() — LATER than the "Dump then compose down" cleanup Start()
	// registers right at its own top. Cleanups run LIFO (last registered
	// runs first), so a TempDir()-backed directory would already be deleted
	// by the time Dump() tried to read the agent's log/binary from it —
	// found live via a first attempt where every dump.go read of these
	// paths failed with "no such file or directory". This directory is
	// instead removed by h.cleanup() itself, which already runs (as part of
	// the SAME "Dump then cleanup" callback) strictly after Dump.
	// Reused on restart (Compose's "restart pglens-agent" interception calls
	// this function again): a fresh MkdirTemp every call would leak the
	// pre-restart directory forever, since cleanup() only ever removes
	// h.agentRunDir's current value.
	runDir := h.agentRunDir
	if runDir == "" {
		var err error
		runDir, err = os.MkdirTemp("", "pglens-agent-binary-*")
		if err != nil {
			return fmt.Errorf("create agent run dir: %w", err)
		}
		h.agentRunDir = runDir
	}

	binPath := filepath.Join(runDir, "pglens-agent")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/pglens-agent")
	buildCmd.Dir = filepath.Join(h.composeDir, "..", "..")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build cmd/pglens-agent: %w, output: %s", err, out)
	}

	// Resolve ports for the target(s) based on topology.
	var primaryPort, standbyPort int
	if h.topology == TopologyPrimaryStandby {
		// Primary-standby topology: resolve both pg-primary and pg-standby.
		var err error
		primaryPort, err = h.getServicePort("pg-primary", 5432)
		if err != nil {
			return fmt.Errorf("resolve pg-primary port: %w", err)
		}
		standbyPort, err = h.getServicePort("pg-standby", 5432)
		if err != nil {
			return fmt.Errorf("resolve pg-standby port: %w", err)
		}
	} else {
		// Standalone topology (default): resolve single "pg" service.
		var err error
		primaryPort, err = h.getServicePort("pg", 5432)
		if err != nil {
			return fmt.Errorf("resolve pg port: %w", err)
		}
	}

	// The healthz port has no CLI flag, only PGLENS_HEALTHZ_LISTEN (env) — an
	// OS-assigned free port is picked the same way a *_test.go net/http test
	// server would: bind, read back the port, close, reuse the number. The
	// small window between close and the subprocess's own bind is the same
	// race docker-compose's own random host-port publishing already carries
	// throughout this harness.
	healthzPort, err := freeTCPPort()
	if err != nil {
		return fmt.Errorf("pick healthz port: %w", err)
	}

	// Reuse the identity file across restarts to preserve the instance ID
	// (important for scenarios that test agent restart behavior).
	var identityPath string
	if h.agentIdentityPath != "" {
		identityPath = h.agentIdentityPath
	} else {
		identityPath = filepath.Join(runDir, "identity.json")
		h.agentIdentityPath = identityPath
	}
	bufferPath := filepath.Join(runDir, "buffer")
	if err := os.MkdirAll(bufferPath, 0o755); err != nil {
		return fmt.Errorf("create buffer dir: %w", err)
	}

	// Reuse config path across restarts
	var configPath string
	if h.agentConfigPath != "" {
		configPath = h.agentConfigPath
	} else {
		configPath = filepath.Join(runDir, "agent.yaml")
		h.agentConfigPath = configPath
	}
	var targetsYAML string
	if h.topology == TopologyPrimaryStandby {
		// Two targets for primary-standby topology.
		targetsYAML = fmt.Sprintf(`targets:
  - name: pg-primary
    dsn: postgres://pglens:pglens-monitoring-test@localhost:%d/postgres?sslmode=disable
    databases:
      max: 10
  - name: pg-standby
    dsn: postgres://pglens:pglens-monitoring-test@localhost:%d/postgres?sslmode=disable
    databases:
      max: 10`, primaryPort, standbyPort)
	} else {
		// Single target for standalone topology.
		targetsYAML = fmt.Sprintf(`targets:
  - name: pg
    dsn: postgres://pglens:pglens-monitoring-test@localhost:%d/postgres?sslmode=disable
    databases:
      max: 10`, primaryPort)
	}

	config := fmt.Sprintf(`server:
  url: http://localhost:%d
  token: dev-token
identity_path: %s
push_interval: 5s
buffer:
  path: %s
  max_size: 64MiB
  max_age: 1h
%s
checks:
  activity: { interval: 5s }
  database_stats: { interval: 5s }
  # Keep the cadence aligned with the cardinality workload's 65s rotations.
  # Selector hysteresis is measured in scrape cycles, so a 10s cadence would
  # forget each previous hot group before the next rotation and never reach
  # MaxKeys in binary mode (the container fixture uses the same 60s cadence).
  stat_statements: { interval: 60s, top_n: 50 }
`, h.serverPort, identityPath, bufferPath, targetsYAML)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		return fmt.Errorf("write agent config: %w", err)
	}

	cmd := exec.Command(binPath, "run", "--config", configPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGLENS_HEALTHZ_LISTEN=:%d", healthzPort))
	logPath := filepath.Join(runDir, "agent.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create agent log file: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start pglens-agent: %w", err)
	}

	h.agentPrimaryPort = primaryPort
	h.agentStandbyPort = standbyPort
	h.agentServerPort = h.serverPort

	h.agentCmd = cmd
	h.agentLogFile = logFile
	h.agentPort = healthzPort
	h.agentBinaryPath = binPath
	// For primary-standby, store the primary's DSN; for standalone, store the
	// single pg DSN. This field is diagnostic-only, used by dump.go's check
	// invocation — not correctness-critical.
	h.agentBinaryDSN = fmt.Sprintf("postgres://pglens:pglens-monitoring-test@localhost:%d/postgres?sslmode=disable", primaryPort)
	h.agentLogPath = logPath

	return nil
}

// freeTCPPort asks the OS for an unused TCP port by binding to port 0 and
// immediately releasing it.
func freeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// composeService is one row of `docker compose ps --format json`.
type composeService struct {
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

// composeFiles returns this harness's full set of `-f` compose-file
// arguments, in the order every compose invocation must use. Centralized so
// a command like `logs` can never again silently drop a fragment file (see
// the fix note on dumpComposeLogs in dump.go).
func (h *Harness) composeFiles() []string {
	args := []string{"-f", h.baseFile, "-f", h.topologyFile, "-f", h.agentFile}
	if h.toxiproxyFile != "" {
		args = append(args, "-f", h.toxiproxyFile)
	}
	return args
}

// composeUp brings up the stack via docker-compose. In AgentModeBinary, pins
// the PG target(s)' host port via PG_HOST_PORT/PG_PRIMARY_HOST_PORT/
// PG_STANDBY_HOST_PORT (see the comment on these in test/compose/topo-*.yml)
// instead of letting Docker assign an ephemeral one, so a mid-test container
// restart can never churn the port the binary-mode agent's DSN — and
// therefore its identity fingerprint — depends on.
func (h *Harness) composeUp() error {
	args := append([]string{"compose", "-p", h.projectName}, h.composeFiles()...)
	args = append(args, "up", "-d")
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	if h.agentMode == AgentModeBinary {
		env := os.Environ()
		if h.topology == TopologyPrimaryStandby {
			primaryPort, err := freeTCPPort()
			if err != nil {
				return fmt.Errorf("pick fixed pg-primary host port: %w", err)
			}
			standbyPort, err := freeTCPPort()
			if err != nil {
				return fmt.Errorf("pick fixed pg-standby host port: %w", err)
			}
			env = append(env,
				fmt.Sprintf("PG_PRIMARY_HOST_PORT=%d", primaryPort),
				fmt.Sprintf("PG_STANDBY_HOST_PORT=%d", standbyPort))
		} else {
			pgPort, err := freeTCPPort()
			if err != nil {
				return fmt.Errorf("pick fixed pg host port: %w", err)
			}
			env = append(env, fmt.Sprintf("PG_HOST_PORT=%d", pgPort))
		}
		cmd.Env = env
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker compose up: %w, output: %s", err, output)
	}
	return nil
}

// waitHealthy polls `docker compose ps` until every service is healthy (or,
// for a service with no healthcheck defined, simply running), and fails
// immediately if any service has exited — a crashed container that isn't
// coming back on its own is not something more polling will fix.
func (h *Harness) waitHealthy() error {
	deadline := time.Now().Add(2 * time.Minute)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastStatus string
	for {
		services, err := h.composePS()
		if err == nil {
			allUp := len(services) > 0
			var statuses []string
			var exited []string
			for _, s := range services {
				statuses = append(statuses, fmt.Sprintf("%s=%s/%s", s.Service, s.State, s.Health))
				if s.State == "exited" || s.State == "dead" {
					exited = append(exited, s.Service)
					continue
				}
				healthy := s.Health == "" || s.Health == "healthy"
				running := s.State == "running" || s.State == "Up"
				if !healthy || !running {
					allUp = false
				}
			}
			lastStatus = strings.Join(statuses, " ")
			if len(exited) > 0 {
				return fmt.Errorf("service(s) exited during startup: %s; last status: %s", strings.Join(exited, ", "), lastStatus)
			}
			if allUp {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stack did not reach healthy state within timeout; last status: %s", lastStatus)
		}
		<-ticker.C
	}
}

// composePS runs `docker compose ps --all --format json` and parses its
// output. `--all` is required: without it, a container that has already
// exited simply does not appear in the listing at all (rather than
// appearing with State "exited") — waitHealthy() previously judged the
// stack healthy as soon as every *still-running* service looked good,
// silently ignoring any service that had already crashed and stopped.
// Compose v2 emits one JSON object per line (NDJSON); tolerate a single
// top-level JSON array too, since the exact shape has varied across versions.
func (h *Harness) composePS() ([]composeService, error) {
	cmd := exec.Command("docker", "compose", "-p", h.projectName, "ps", "--all", "--format", "json")
	cmd.Dir = h.composeDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker compose ps: %w, output: %s", err, output)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var services []composeService
		if err := json.Unmarshal([]byte(trimmed), &services); err != nil {
			return nil, fmt.Errorf("parse compose ps array: %w", err)
		}
		return services, nil
	}

	var services []composeService
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var s composeService
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("parse compose ps line %q: %w", line, err)
		}
		services = append(services, s)
	}
	return services, nil
}

// API returns a client for the pglens server HTTP API.
func (h *Harness) API() *APIClient {
	return h.apiClient
}

// AgentHealthz fetches the agent's own /healthz (pusher.HealthzHandler,
// distinct from the server's plain-text /healthz that APIClient.Healthz
// hits) — its real JSON body carries state/buffer_stats/last_error/
// clock_skew_seconds. Requires the harness to have been started with an
// AgentMode that publishes port 9187 (container modes do; binary mode does
// not run through compose at all).
func (h *Harness) AgentHealthz() (map[string]interface{}, error) {
	if h.agentPort == 0 {
		return nil, fmt.Errorf("agent healthz port not resolved (AgentMode not set, or the compose fragment doesn't publish 9187)")
	}
	url := fmt.Sprintf("http://localhost:%d/healthz", h.agentPort)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %d, body: %s", url, resp.StatusCode, body)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode agent healthz body: %w", err)
	}
	return result, nil
}

// DB returns a connection pool to the TimescaleDB instance (for invariants).
func (h *Harness) DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return h.pgPool(t, "timescaledb", "pglens", "pglens", "pglens")
}

// PG returns a connection pool to a named monitored PostgreSQL service
// ("pg" for a standalone topology, "pg-primary"/"pg-standby" for
// primary-standby). These containers use the default postgres/postgres
// credentials, matching test/compose/topo-*.yml.
func (h *Harness) PG(t *testing.T, service string) *pgxpool.Pool {
	t.Helper()
	return h.pgPool(t, service, "postgres", "postgres", "postgres")
}

// pgPool resolves service's published port (caching it and the resulting
// pool for the lifetime of the harness) and connects.
func (h *Harness) pgPool(t *testing.T, service, user, password, dbname string) *pgxpool.Pool {
	t.Helper()
	h.poolMu.Lock()
	defer h.poolMu.Unlock()
	if pool, ok := h.pools[service]; ok {
		return pool
	}

	port, err := h.getServicePort(service, 5432)
	if err != nil {
		t.Fatalf("resolve %s port: %v", service, err)
		return nil
	}

	dsn := fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable", user, password, port, dbname)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", service, err)
		return nil
	}
	h.pools[service] = pool
	return pool
}

// Exec runs a command inside a named service container.
func (h *Harness) Exec(service string, argv ...string) (string, error) {
	args := []string{"compose", "-p", h.projectName, "exec", "-T", service}
	args = append(args, argv...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// ExecAs runs a command inside a named service container as a specific
// user — needed for anything postgres-privileged (e.g. `pg_ctl promote`,
// which refuses outright to run as the container's default root user:
// "pg_ctl: cannot be run as root").
func (h *Harness) ExecAs(service, user string, argv ...string) (string, error) {
	args := []string{"compose", "-p", h.projectName, "exec", "-T", "-u", user, service}
	args = append(args, argv...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// Compose runs a docker-compose command.
func (h *Harness) Compose(argv ...string) error {
	// In binary mode, the pglens-agent service doesn't exist (it's a host
	// subprocess). If a scenario tries to restart it, handle that by killing
	// and respawning the subprocess.
	if h.agentMode == AgentModeBinary && len(argv) >= 2 && argv[0] == "restart" && argv[1] == "pglens-agent" {
		if h.agentCmd == nil || h.agentCmd.Process == nil {
			return fmt.Errorf("agent subprocess not running (cannot restart)")
		}
		// Kill the subprocess
		if err := h.agentCmd.Process.Kill(); err != nil {
			return fmt.Errorf("kill agent subprocess: %w", err)
		}
		// Wait for it to exit
		if err := h.agentCmd.Wait(); err != nil {
			// Kill might exit with error; that's OK
		}
		// Close the log file
		if h.agentLogFile != nil {
			h.agentLogFile.Close()
		}
		// Respawn the agent
		return h.startAgentBinary()
	}
	_, err := h.composeOutput(argv...)
	if err != nil {
		return err
	}
	// docker compose reassigns a NEW ephemeral host port on every "start" of
	// a container, even the SAME container ID — verified live: both
	// `kill -SIGKILL`+`start` and `stop`+`start` moved the published port.
	// Container-mode agents are unaffected (they reach targets/server via
	// stable internal docker DNS names), but a binary-mode agent's config
	// bakes in the HOST port resolved once at startAgentBinary() time, so a
	// scenario that restarts "pg"/"pg-primary"/"pg-standby"/"pglens-server"
	// mid-test silently strands it pointed at a now-dead port forever (this
	// is what made SYS-RESET-001 and SYS-NET-001 hang to their own timeout
	// under AGENT_MODE=binary: "connection refused" on the stale port,
	// never recovering). Re-resolve and, if a tracked port moved, restart
	// the agent subprocess so it reconnects.
	if h.agentMode == AgentModeBinary && len(argv) >= 2 && argv[0] == "start" {
		return h.reconcileAgentBinaryPort(argv[1])
	}
	return nil
}

// reconcileAgentBinaryPort re-resolves the host port for a compose service
// the binary-mode agent (or the harness's own API client) depends on and, if
// it changed since the agent's config was last written, rewrites the config
// and restarts the agent subprocess. See the comment in Compose for why this
// is necessary. A no-op for services the binary-mode agent has no config
// entry for.
func (h *Harness) reconcileAgentBinaryPort(service string) error {
	switch service {
	case "pg", "pg-primary", "pg-standby", "pglens-server":
	default:
		return nil
	}

	newPrimaryPort := h.agentPrimaryPort
	newStandbyPort := h.agentStandbyPort
	newServerPort := h.agentServerPort

	switch service {
	case "pg", "pg-primary":
		port, err := h.getServicePort(service, 5432)
		if err != nil {
			return fmt.Errorf("re-resolve %s port: %w", service, err)
		}
		newPrimaryPort = port
	case "pg-standby":
		port, err := h.getServicePort("pg-standby", 5432)
		if err != nil {
			return fmt.Errorf("re-resolve pg-standby port: %w", err)
		}
		newStandbyPort = port
	case "pglens-server":
		port, err := h.getServicePort("pglens-server", 8080)
		if err != nil {
			return fmt.Errorf("re-resolve pglens-server port: %w", err)
		}
		newServerPort = port
		h.serverPort = port
		if h.apiClient != nil {
			h.apiClient.baseURL = fmt.Sprintf("http://localhost:%d", port)
		}
	}

	if newPrimaryPort == h.agentPrimaryPort && newStandbyPort == h.agentStandbyPort && newServerPort == h.agentServerPort {
		return nil // port unchanged, nothing to reconnect
	}

	if h.agentCmd == nil || h.agentCmd.Process == nil {
		return nil // agent subprocess not started yet
	}
	if err := h.agentCmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill agent subprocess for port reconcile: %w", err)
	}
	if err := h.agentCmd.Wait(); err != nil {
		// Kill might exit with error; that's OK
	}
	if h.agentLogFile != nil {
		h.agentLogFile.Close()
	}
	return h.startAgentBinary()
}

// Logs returns a service's captured stdout/stderr (docker compose logs).
func (h *Harness) Logs(service string) (string, error) {
	return h.composeOutput("logs", "--no-color", service)
}

// Workload runs test/workload (workloadctl) as a host subprocess with the
// given CLI args (e.g. "lock-storm", "--sessions", "50", "--dsn", dsn) and
// returns its combined output — the tool's own JSON report on success.
// test/workload has no compose service or Dockerfile of its own; it runs on
// the host, exactly like the test process itself, which is why callers pass
// a DSN pointed at a service's published port (see pg.Config().ConnConfig,
// already used this way by SYS-ASH-001) rather than a container-internal
// hostname. The binary is built once per Harness and cached.
func (h *Harness) Workload(args ...string) (string, error) {
	h.workloadMu.Lock()
	defer h.workloadMu.Unlock()

	if h.workloadBin == "" {
		bin := filepath.Join(h.t.TempDir(), "workloadctl")
		buildCmd := exec.Command("go", "build", "-o", bin, "./test/workload")
		buildCmd.Dir = filepath.Join(h.composeDir, "..", "..")
		if out, err := buildCmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("build test/workload: %w, output: %s", err, out)
		}
		h.workloadBin = bin
	}

	cmd := exec.Command(h.workloadBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("workloadctl %s: %w, output: %s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// composeOutput runs a docker-compose command and returns its trimmed
// output. Compose files are always included: some subcommands (`up`,
// notably `--force-recreate`) require them outright ("no configuration
// file provided" otherwise — found live running SYS-AGENT-002, the first
// scenario to call Compose("up", ...) rather than kill/start/stop, which
// happen to work via project-label discovery without them and masked this
// gap until now); others tolerate them being present harmlessly.
func (h *Harness) composeOutput(argv ...string) (string, error) {
	args := append([]string{"compose", "-p", h.projectName}, h.composeFiles()...)
	args = append(args, argv...)
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w, output: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// Scenario looks up a registered scenario (test/scenario, sub-phase 5.5) by
// ID and runs it, wiring test/scenario.Env to this harness's live DB/API/
// exec access. test/scenario does not import test/harness (that would
// cycle, since this method already imports test/scenario) — Env's fields
// are plain function values and a small locally-declared interface instead,
// satisfied by *Harness's real methods purely by structural typing.
func (h *Harness) Scenario(t *testing.T, id string) {
	t.Helper()
	s, ok := scenario.Get(id)
	if !ok {
		t.Fatalf("scenario %q is not registered", id)
	}

	env := &scenario.Env{
		T:            t,
		DB:           h.DB(t),
		PG:           func(service string) *pgxpool.Pool { return h.PG(t, service) },
		API:          h.apiClient,
		AgentHealthz: h.AgentHealthz,
		Exec:         h.Exec,
		ExecAs:       h.ExecAs,
		Compose:      h.Compose,
		Logs:         h.Logs,
		Workload:     h.Workload,
		Toxic: func(link scenario.ToxicLink, tox scenario.ToxicPayload) (scenario.RemoveFunc, error) {
			hLink := LinkAgentToPG(link.Service)
			if link.Kind == "server" {
				hLink = LinkAgentToServer()
			}
			remove, err := h.Toxic(hLink, toxicAdapter{tox})
			if err != nil {
				return nil, err
			}
			return scenario.RemoveFunc(remove), nil
		},
		AssertInvariants: h.AssertInvariants,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := s.Run(ctx, env); err != nil {
		t.Fatalf("scenario %s (%s) failed: %v", id, s.Title, err)
	}
}

// cleanup removes the stack and releases resources.
func (h *Harness) cleanup() {
	h.cleanMu.Lock()
	defer h.cleanMu.Unlock()
	if h.cleaned {
		return
	}
	h.cleaned = true

	// Stop the binary-mode agent subprocess before anything else — Dump has
	// already run by the time cleanup() is called (both are part of the same
	// t.Cleanup registered at the top of Start()), so this is safe to tear
	// down now, unlike a separate, earlier-registered cleanup would be (see
	// startAgentBinary's own comment on why it doesn't use h.t.TempDir()).
	if h.agentCmd != nil && h.agentCmd.Process != nil {
		_ = h.agentCmd.Process.Kill()
		_ = h.agentCmd.Wait()
	}
	if h.agentLogFile != nil {
		_ = h.agentLogFile.Close()
	}
	if h.agentRunDir != "" {
		_ = os.RemoveAll(h.agentRunDir)
	}

	// Close all pools
	h.poolMu.Lock()
	for _, pool := range h.pools {
		if pool != nil {
			pool.Close()
		}
	}
	h.pools = make(map[string]*pgxpool.Pool)
	h.poolMu.Unlock()

	// Stop and remove the compose stack
	cmd := exec.Command("docker", "compose", "-p", h.projectName, "down", "-v")
	cmd.Dir = h.composeDir
	_ = cmd.Run() // Ignore errors; best-effort cleanup
}

// composeDirectory returns the path to test/compose
func composeDirectory() string {
	// Find repo root relative to this file
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "../compose")
}
