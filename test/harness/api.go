//go:build e2e

package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Request performs an arbitrary JSON API request and returns the decoded JSON
// value. It is intentionally small so scenarios can exercise array-shaped
// endpoints and operator actions without duplicating HTTP plumbing.
func (c *APIClient) Request(method, path string, payload []byte) (interface{}, error) {
	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}
	req, err := c.newRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %d, body: %s", method, path, resp.StatusCode, raw)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var result interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RequestWithHeaders is the authenticated variant used by command-channel
// acceptance tests to submit a result with a deliberately stale claim token.
func (c *APIClient) RequestWithHeaders(method, path string, payload []byte, headers map[string]string) (interface{}, int, error) {
	var body io.Reader
	if len(payload) > 0 {
		body = bytes.NewReader(payload)
	}
	req, err := c.newRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("%s %s: %d, body: %s", method, path, resp.StatusCode, raw)
	}
	if len(raw) == 0 {
		return nil, resp.StatusCode, nil
	}
	var result interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, resp.StatusCode, err
	}
	return result, resp.StatusCode, nil
}

// APIClient wraps the HTTP client for the pglens server API.
type APIClient struct {
	client     interface{} // kept for compat; use httpClient
	baseURL    string
	token      string
	httpClient *http.Client
}

func (c *APIClient) newRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *APIClient) do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := c.newRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

// Clusters fetches the current cluster state from /api/v1/clusters. The
// endpoint's top-level JSON shape is an array (see API.handleClusters), not
// an object — decoding into a map (as an earlier version of this method did)
// always failed with a JSON type error the moment anything actually called
// it; no scenario had, until SYS-PERM-001 (test/scenario/perm.go).
func (c *APIClient) Clusters() ([]map[string]interface{}, error) {
	resp, err := c.do(http.MethodGet, "/api/v1/clusters", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/v1/clusters: %d, body: %s", resp.StatusCode, body)
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// Instance fetches one instance's detail (including its per-database
// monitored/skip_reason breakdown) from /api/v1/instances/{id}.
func (c *APIClient) Instance(id string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/v1/instances/%s", id)
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %d, body: %s", path, resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// Events fetches events from /api/v1/events.
func (c *APIClient) Events() ([]map[string]interface{}, error) {
	resp, err := c.do(http.MethodGet, "/api/v1/events", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/v1/events: %d, body: %s", resp.StatusCode, body)
	}

	var result []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// Readyz checks the server health endpoint.
func (c *APIClient) Readyz() (bool, error) {
	resp, err := c.do(http.MethodGet, "/readyz", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// Healthz fetches the pglens-server's own /healthz — a static liveness
// check (`internal/server/http.go` returns plain text "ok", not JSON; a
// prior version of this method tried to JSON-decode that body and would
// have failed every single call, though nothing had ever actually called
// it to notice). For the agent's real, JSON-bodied health
// (state/buffer_stats/last_error), see Harness.AgentHealthz.
func (c *APIClient) Healthz() (map[string]interface{}, error) {
	resp, err := c.do(http.MethodGet, "/healthz", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return map[string]interface{}{"status_code": resp.StatusCode, "body": string(body)}, nil
}

// Get fetches an arbitrary JSON-object endpoint. path must start with "/"
// and may include a query string (e.g. "/api/v1/ash?instance_id=...").
func (c *APIClient) Get(path string) (map[string]interface{}, error) {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %d, body: %s", path, resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetExact is the lossless counterpart of Get for endpoints carrying int64
// identifiers such as PostgreSQL queryids. It is kept separate so existing
// scenarios that consume float64 JSON numbers remain source-compatible.
func (c *APIClient) GetExact(path string) (interface{}, error) {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: %d, body: %s", path, resp.StatusCode, body)
	}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	var result interface{}
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// RawGet fetches an arbitrary endpoint and returns its raw text body — for
// non-JSON responses like /metrics (Prometheus text exposition), where Get
// would fail decoding.
func (c *APIClient) RawGet(path string) (string, error) {
	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %d, body: %s", path, resp.StatusCode, body)
	}
	return string(body), nil
}

// AnonymousGet issues a GET with no Authorization header and no cookies at
// all, and returns only the status code.
//
// It exists so a scenario can assert what an unauthenticated caller gets from
// the API. The server used to install its credential gate only when the web
// UI was enabled, so a deployment that turned the interface off to expose
// "just the API" served the entire read API — clusters, statements, ASH,
// findings, settings, audit — and the command surface to anyone who could
// reach the port, and nothing in the suite would have noticed.
func (c *APIClient) AnonymousGet(path string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// WaitReadyz polls /readyz until it returns 200 or timeout.
func (c *APIClient) WaitReadyz(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		ok, err := c.Readyz()
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("readyz timeout: %w", err)
			}
			return fmt.Errorf("readyz timeout")
		}
		<-ticker.C
	}
}
