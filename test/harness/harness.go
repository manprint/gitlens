//go:build e2e

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	AgentModeContainer         AgentMode = "container"
	AgentModeBinary            AgentMode = "binary"
	AgentModeContainerNoVolume AgentMode = "container-no-volume"
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
	t             *testing.T
	projectName   string
	baseFile      string
	topologyFile  string
	agentFile     string
	toxiproxyFile string // "" unless Config.Toxiproxy is set
	composeDir    string
	containerName string
	serverPort    int
	agentPort     int
	pools         map[string]*pgxpool.Pool
	poolMu        sync.Mutex
	apiClient     *APIClient
	cleaned       bool
	cleanMu       sync.Mutex
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
	} else if cfg.Toxiproxy {
		// Points the agent's target DSN at toxiproxy's proxy instead of "pg"
		// directly (test/fixtures/agent-standalone-toxic.yaml) — without
		// this, InitToxiproxy()'s proxies exist but nothing ever sends
		// traffic through them. Toxiproxy fault injection is only wired up
		// for AgentModeContainer/"" (the default) currently.
		agentFile = filepath.Join(composeDir, "agent-container-toxic.yml")
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

	if cfg.AgentMode != "" {
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

// composeUp brings up the stack via docker-compose.
func (h *Harness) composeUp() error {
	args := append([]string{"compose", "-p", h.projectName}, h.composeFiles()...)
	args = append(args, "up", "-d")
	cmd := exec.Command("docker", args...)
	cmd.Dir = h.composeDir
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

// Compose runs a docker-compose command.
func (h *Harness) Compose(argv ...string) error {
	_, err := h.composeOutput(argv...)
	return err
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
		Compose:      h.Compose,
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
