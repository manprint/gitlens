//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Topology specifies the PostgreSQL deployment shape (mirrored from harness for registry independence).
type Topology string

const (
	TopologyStandalone     Topology = "standalone"
	TopologyPrimaryStandby Topology = "primary-standby"
)

// Scenario defines a reusable E2E test scenario identified by a stable ID.
// Each scenario is registered once and invocable from any test and from the command line.
type Scenario struct {
	ID          string                                  // Stable identifier, e.g. "SYS-REPL-001"
	Title       string                                  // Human-readable title
	Topology    Topology                                // Standalone or PrimaryStandby
	EstDuration time.Duration                           // Estimated runtime
	Covers      []string                                // What this scenario covers, e.g. "IDEA.md#5.1", "I-1"
	Run         func(ctx context.Context, e *Env) error // Scenario implementation
	Expect      Expectations                            // Events and invariants this scenario asserts
	Smoke       bool                                    // If true, included in smoke test suite
}

// Expectations describes the events and invariants a scenario must verify.
type Expectations struct {
	Events     []string // Event types expected to appear
	Invariants []string // Invariant conditions that must hold
}

// APIClient is the subset of test/harness.APIClient's real methods a
// scenario needs. Declared locally (rather than importing test/harness,
// which imports this package to run scenarios by ID) so *harness.APIClient
// satisfies it purely by structural typing — no import cycle.
type APIClient interface {
	Clusters() ([]map[string]interface{}, error)
	Instance(id string) (map[string]interface{}, error)
	Events() ([]map[string]interface{}, error)
	Readyz() (bool, error)
	Healthz() (map[string]interface{}, error)
	WaitReadyz(timeout time.Duration) error
	// Get fetches an arbitrary JSON-object endpoint (path includes the query
	// string, e.g. "/api/v1/ash?instance_id=..."). Endpoints with a
	// dedicated typed method above should keep using it; this exists for
	// endpoints (like /api/v1/ash's "enabled" field) that don't have one yet.
	Get(path string) (map[string]interface{}, error)
	// RawGet fetches an arbitrary endpoint and returns its raw text body —
	// for non-JSON responses like /metrics (Prometheus text exposition).
	RawGet(path string) (string, error)
	// AnonymousGet issues a GET with no credentials whatsoever and returns
	// only the status code, so a scenario can assert that the API is not an
	// anonymous surface.
	AnonymousGet(path string) (int, error)
	Request(method, path string, payload []byte) (interface{}, error)
}

// Env provides scenario execution context: DB/API access, container control,
// and the invariant check every scenario must call at the end. Populated by
// test/harness.Harness.Scenario() from its own live resources — see that
// method for exactly how each field is wired.
type Env struct {
	T *testing.T

	// DB is a pool to the TimescaleDB (server) instance.
	DB *pgxpool.Pool
	// PG returns a pool to a named monitored PostgreSQL service ("pg" for
	// standalone, "pg-primary"/"pg-standby" for primary-standby).
	PG func(service string) *pgxpool.Pool
	// API is a client for the pglens server HTTP API.
	API APIClient
	// APIs contains every server replica when a scenario explicitly scales the
	// server. API remains the first replica for existing scenarios.
	APIs []APIClient
	// MockReceiverURL is populated by the alerting compose fragment.
	MockReceiverURL string
	// AgentHealthz fetches the agent's own /healthz (distinct from the
	// server's — see Harness.AgentHealthz). Only populated when the
	// harness was started with an AgentMode that publishes port 9187.
	AgentHealthz func() (map[string]interface{}, error)

	// Exec runs a command inside a named service container.
	Exec func(service string, argv ...string) (string, error)
	// ExecAs runs a command as a specific user — needed for anything
	// postgres-privileged (`pg_ctl promote` refuses to run as root).
	ExecAs func(service, user string, argv ...string) (string, error)
	// Compose runs a docker-compose command against the running stack.
	Compose func(argv ...string) error
	// Logs returns a service's captured stdout/stderr (docker compose logs).
	Logs func(service string) (string, error)
	// Workload runs test/workload (workloadctl) as a host subprocess and
	// returns its combined output (its JSON report on success).
	Workload func(args ...string) (string, error)
	// Toxic injects a fault on a link (requires harness.Config{Toxiproxy:
	// true}) and returns a function to remove it.
	Toxic func(link ToxicLink, t ToxicPayload) (RemoveFunc, error)

	// AssertInvariants runs the global invariant checks (phase_06.md §5.6).
	// Every scenario must call this at the end, per phase_06.md §5.7.
	AssertInvariants func(t *testing.T)
	// KillServerReplica terminates exactly one scaled server container. L3
	// alert tests use it to exercise advisory-lock failover.
	KillServerReplica func() error
}

// ToxicLink identifies a connection to inject a fault into (mirrors
// test/harness.Link — declared locally for the same import-cycle reason as
// APIClient above).
type ToxicLink struct {
	Kind    string // "server" or "pg"
	Service string // compose service name, only used for "pg"
}

// LinkAgentToServer returns a ToxicLink for the agent-to-server connection.
func LinkAgentToServer() ToxicLink { return ToxicLink{Kind: "server"} }

// LinkAgentToPG returns a ToxicLink for the agent-to-database connection.
func LinkAgentToPG(service string) ToxicLink { return ToxicLink{Kind: "pg", Service: service} }

// ToxicPayload is a fault-injection payload (mirrors test/harness.Toxic).
type ToxicPayload interface {
	ToxicType() string
	ToxicAttrs() map[string]interface{}
}

// RemoveFunc removes a previously-injected toxic.
type RemoveFunc func() error

// ToxicTimeout simulates a black hole: the connection is accepted but never
// responds.
type ToxicTimeout struct{}

func (ToxicTimeout) ToxicType() string { return "timeout" }
func (ToxicTimeout) ToxicAttrs() map[string]interface{} {
	return map[string]interface{}{"timeout": int64(0)}
}

// registry holds all registered scenarios, keyed by ID.
var (
	registry   = make(map[string]Scenario)
	registryMu sync.RWMutex
)

// Register registers a scenario. Panics if a scenario with the same ID is already registered.
func Register(s Scenario) {
	if s.ID == "" {
		panic("Scenario ID cannot be empty")
	}
	if s.Run == nil {
		panic(fmt.Sprintf("Scenario %s: Run function cannot be nil", s.ID))
	}
	if len(s.Covers) == 0 {
		panic(fmt.Sprintf("Scenario %s: must have at least one Covers entry", s.ID))
	}
	if len(s.Expect.Events) == 0 && len(s.Expect.Invariants) == 0 {
		panic(fmt.Sprintf("Scenario %s: must have at least one expected event or invariant", s.ID))
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[s.ID]; exists {
		panic(fmt.Sprintf("Scenario %s already registered", s.ID))
	}
	registry[s.ID] = s
}

// Get retrieves a scenario by ID. Returns the scenario and a boolean indicating if it was found.
func Get(id string) (Scenario, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	s, ok := registry[id]
	return s, ok
}

// All returns all registered scenarios sorted by ID.
func All() []Scenario {
	registryMu.RLock()
	defer registryMu.RUnlock()

	scenarios := make([]Scenario, 0, len(registry))
	for _, s := range registry {
		scenarios = append(scenarios, s)
	}
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].ID < scenarios[j].ID
	})
	return scenarios
}

// Smoke returns all scenarios marked as smoke test scenarios, sorted by ID.
func Smoke() []Scenario {
	registryMu.RLock()
	defer registryMu.RUnlock()

	scenarios := make([]Scenario, 0)
	for _, s := range registry {
		if s.Smoke {
			scenarios = append(scenarios, s)
		}
	}
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].ID < scenarios[j].ID
	})
	return scenarios
}

// Reset clears all registered scenarios. Used only in tests.
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Scenario)
}
