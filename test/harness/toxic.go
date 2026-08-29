//go:build e2e

package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	toxiproxyapi "github.com/Shopify/toxiproxy/v2/client"

	"github.com/manprint/pglens/test/scenario"
)

// toxicAdapter satisfies this package's private Toxic interface by
// delegating to a test/scenario.ToxicPayload's exported methods — declared
// here (not in test/scenario) since Toxic's methods are deliberately
// unexported and test/scenario cannot import this package (import cycle:
// this package already imports test/scenario to run scenarios by ID).
type toxicAdapter struct{ p scenario.ToxicPayload }

func (a toxicAdapter) toxicType() string                  { return a.p.ToxicType() }
func (a toxicAdapter) toxicAttrs() map[string]interface{} { return a.p.ToxicAttrs() }

// Link identifies a connection between components to inject faults into.
type Link struct {
	kind string // "server" or "pg"
	svc  string // service name (only used for "pg" kind)
}

// LinkAgentToServer returns a Link for the agent-to-server connection.
func LinkAgentToServer() Link {
	return Link{kind: "server"}
}

// LinkAgentToPG returns a Link for the agent-to-database connection for a specific PostgreSQL service.
func LinkAgentToPG(service string) Link {
	return Link{kind: "pg", svc: service}
}

// Toxic is an interface implemented by fault injection payloads.
type Toxic interface {
	toxicType() string
	toxicAttrs() map[string]interface{}
}

// Latency injects network latency and jitter.
type Latency struct {
	Ms       int // milliseconds of latency
	JitterMs int // milliseconds of jitter (random variation)
}

func (t Latency) toxicType() string {
	return "latency"
}

func (t Latency) toxicAttrs() map[string]interface{} {
	return map[string]interface{}{
		"latency": int64(t.Ms),
		"jitter":  int64(t.JitterMs),
	}
}

// Timeout simulates a connection timeout (black hole).
// The connection is accepted but never responds.
type Timeout struct {
}

func (t Timeout) toxicType() string {
	return "timeout"
}

func (t Timeout) toxicAttrs() map[string]interface{} {
	return map[string]interface{}{
		"timeout": int64(0), // 0 = immediate timeout
	}
}

// Bandwidth limits connection bandwidth.
type Bandwidth struct {
	KBps int // kilobytes per second
}

func (t Bandwidth) toxicType() string {
	return "bandwidth"
}

func (t Bandwidth) toxicAttrs() map[string]interface{} {
	return map[string]interface{}{
		"rate": int64(t.KBps),
	}
}

// LimitData truncates responses after a certain number of bytes.
type LimitData struct {
	Bytes int // bytes limit
}

func (t LimitData) toxicType() string {
	return "limit_data"
}

func (t LimitData) toxicAttrs() map[string]interface{} {
	return map[string]interface{}{
		"bytes": int64(t.Bytes),
	}
}

// ResetPeer closes the connection immediately.
type ResetPeer struct {
}

func (t ResetPeer) toxicType() string {
	return "reset_peer"
}

func (t ResetPeer) toxicAttrs() map[string]interface{} {
	return map[string]interface{}{}
}

// RemoveFunc is returned by Toxic() and removes the toxic when called.
type RemoveFunc func() error

// ToxicState holds toxiproxy configuration and state.
type ToxicState struct {
	mu           sync.Mutex
	enabled      bool
	client       *toxiproxyapi.Client
	toxiproxyURL string
	proxyNames   map[string]string // maps link key to proxy name
	toxicCounter int               // for generating unique toxic names
}

// ToxicRegistry holds toxiproxy state per harness
var toxicRegistry = sync.Map{}

// getToxicState retrieves the ToxicState for a harness.
func getToxicState(h *Harness) *ToxicState {
	if val, ok := toxicRegistry.Load(h); ok {
		return val.(*ToxicState)
	}
	return nil
}

// setToxicState stores the ToxicState for a harness.
func setToxicState(h *Harness, state *ToxicState) {
	toxicRegistry.Store(h, state)
}

// InitToxiproxy initializes toxiproxy client and creates proxies for all services.
// This should be called after the harness stack is started.
func (h *Harness) InitToxiproxy() error {
	state := &ToxicState{
		enabled:    true,
		proxyNames: make(map[string]string),
	}

	// Get the toxiproxy admin port from docker compose
	port, err := h.getServicePort("toxiproxy", 8474)
	if err != nil {
		return fmt.Errorf("failed to get toxiproxy admin port: %w", err)
	}

	state.toxiproxyURL = fmt.Sprintf("http://localhost:%d", port)
	state.client = toxiproxyapi.NewClient(state.toxiproxyURL)

	// Wait for toxiproxy to be ready
	if err := h.waitToxiproxyReady(state.toxiproxyURL); err != nil {
		return fmt.Errorf("toxiproxy not ready: %w", err)
	}

	// Create proxies for server and PostgreSQL services
	// Server proxy: agent connects to 127.0.0.1:20000 which proxies to pglens-server:8080
	serverProxyName := "agent-to-server"
	if err := h.createProxyIfNeeded(state, serverProxyName, "pglens-server", 8080, 20000); err != nil {
		// Warn but don't fail - server might not be running yet
		h.t.Logf("warning: failed to create server proxy: %v", err)
	} else {
		state.proxyNames[LinkAgentToServer().kind] = serverProxyName
	}

	// Create proxies for PostgreSQL services
	pgServices := []string{"pg", "pg-primary", "pg-standby", "timescaledb"}
	basePort := 20001
	for i, svc := range pgServices {
		proxyName := fmt.Sprintf("agent-to-%s", svc)
		if err := h.createProxyIfNeeded(state, proxyName, svc, 5432, basePort+i); err != nil {
			// It's okay if a service doesn't exist - skip it
			continue
		}
		state.proxyNames[fmt.Sprintf("pg:%s", svc)] = proxyName
	}

	setToxicState(h, state)
	return nil
}

// createProxyIfNeeded creates a proxy if it doesn't already exist.
func (h *Harness) createProxyIfNeeded(state *ToxicState, proxyName, upstream string, upstreamPort, listenPort int) error {
	// First check if proxy already exists
	proxy, err := state.client.Proxy(proxyName)
	if err == nil && proxy != nil {
		// Proxy exists, nothing to do
		return nil
	}

	// Create the proxy
	// The listen address is 0.0.0.0 inside the docker network
	// Upstream uses docker service name resolution
	listenAddr := fmt.Sprintf("0.0.0.0:%d", listenPort)
	upstreamAddr := fmt.Sprintf("%s:%d", upstream, upstreamPort)

	_, err = state.client.CreateProxy(proxyName, listenAddr, upstreamAddr)
	return err
}

// waitToxiproxyReady polls the toxiproxy health endpoint.
func (h *Harness) waitToxiproxyReady(toxiproxyURL string) error {
	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := http.Get(toxiproxyURL + "/version")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("toxiproxy did not become ready within 30s")
		}
		<-ticker.C
	}
}

// getServicePort retrieves the published port for a service from docker-compose.
func (h *Harness) getServicePort(service string, containerPort int) (int, error) {
	ports, err := h.getServicePorts(service, containerPort)
	if err != nil {
		return 0, err
	}
	if len(ports) == 0 {
		return 0, fmt.Errorf("no published port for %s:%d", service, containerPort)
	}
	return ports[0], nil
}

// getServicePorts is the plural form used by scaled services. Docker Compose
// prints one host binding per replica; keeping all of them lets L3 tests query
// every server replica while the ordinary harness continues to use the first.
func (h *Harness) getServicePorts(service string, containerPort int) ([]int, error) {
	cmd := exec.Command("docker", "compose",
		"-p", h.projectName,
		"port", service, fmt.Sprintf("%d", containerPort))
	cmd.Dir = h.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker compose port failed: %w, output: %s", err, output)
	}

	// Output is "127.0.0.1:PORT"
	var ports []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("unexpected port output format: %s", output)
		}
		var port int
		if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
			return nil, fmt.Errorf("failed to parse port: %w", err)
		}
		ports = append(ports, port)
	}
	return ports, nil
}

// Toxic adds a fault injection to a link and returns a function to remove it.
// The RemoveFunc must be called to clean up the toxic.
func (h *Harness) Toxic(link Link, t Toxic) (RemoveFunc, error) {
	state := getToxicState(h)
	if state == nil {
		return nil, fmt.Errorf("toxiproxy not initialized - call h.InitToxiproxy() first")
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	// Get the proxy name for this link
	var proxyName string
	if link.kind == "server" {
		var ok bool
		proxyName, ok = state.proxyNames[link.kind]
		if !ok {
			return nil, fmt.Errorf("server proxy not found")
		}
	} else if link.kind == "pg" {
		var ok bool
		proxyName, ok = state.proxyNames[fmt.Sprintf("pg:%s", link.svc)]
		if !ok {
			return nil, fmt.Errorf("postgres proxy for service %q not found", link.svc)
		}
	} else {
		return nil, fmt.Errorf("unknown link kind: %s", link.kind)
	}

	// Confirm the proxy exists before posting a toxic against it.
	if _, err := state.client.Proxy(proxyName); err != nil {
		return nil, fmt.Errorf("failed to get proxy %q: %w", proxyName, err)
	}

	// Build the toxic payload
	attrs := t.toxicAttrs()
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal toxic attributes: %w", err)
	}

	// A full fault (e.g. a black hole) needs both directions toxified:
	// "upstream" (client->destination, e.g. the agent's outgoing query) and
	// "downstream" (destination->client, e.g. PostgreSQL's response) are
	// the only valid toxiproxy `stream` values — an earlier version of this
	// method sent the literal string "both", which the toxiproxy API
	// rejects outright with 400 "stream was invalid, can be either upstream
	// or downstream". Undetected because no scenario had ever called this
	// live before SYS-NET-003 (test/scenario/net.go).
	state.toxicCounter++
	var toxicNames []string
	for _, stream := range []string{"upstream", "downstream"} {
		toxicName := fmt.Sprintf("toxic-%d-%s-%s", state.toxicCounter, t.toxicType(), stream)
		reqBody := fmt.Sprintf(`{"name":"%s","type":"%s","stream":"%s","toxicity":1.0,"attributes":%s}`,
			toxicName, t.toxicType(), stream, string(attrsJSON))

		url := fmt.Sprintf("%s/proxies/%s/toxics", state.toxiproxyURL, proxyName)
		resp, err := http.Post(url, "application/json", strings.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create %s toxic: %w", stream, err)
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				err = fmt.Errorf("toxiproxy API returned %d creating %s toxic: %s", resp.StatusCode, stream, body)
			}
		}()
		if err != nil {
			return nil, err
		}
		toxicNames = append(toxicNames, toxicName)
	}

	// Return a function to remove both toxics.
	removeFunc := func() error {
		state.mu.Lock()
		defer state.mu.Unlock()

		var errs []string
		for _, toxicName := range toxicNames {
			url := fmt.Sprintf("%s/proxies/%s/toxics/%s", state.toxiproxyURL, proxyName, toxicName)
			req, err := http.NewRequest("DELETE", url, nil)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			func() {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					errs = append(errs, fmt.Sprintf("toxiproxy API returned %d: %s", resp.StatusCode, body))
				}
			}()
		}
		if len(errs) > 0 {
			return fmt.Errorf("failed to delete toxic(s): %s", strings.Join(errs, "; "))
		}
		return nil
	}

	return removeFunc, nil
}

// Compile checks to ensure Toxic interface is implemented
var (
	_ Toxic = Latency{}
	_ Toxic = Timeout{}
	_ Toxic = Bandwidth{}
	_ Toxic = LimitData{}
	_ Toxic = ResetPeer{}
)
