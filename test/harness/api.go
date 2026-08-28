//go:build e2e

package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient wraps the HTTP client for the pglens server API.
type APIClient struct {
	client     interface{} // kept for compat; use httpClient
	baseURL    string
	httpClient *http.Client
}

// Clusters fetches the current cluster state from /api/v1/clusters. The
// endpoint's top-level JSON shape is an array (see API.handleClusters), not
// an object — decoding into a map (as an earlier version of this method did)
// always failed with a JSON type error the moment anything actually called
// it; no scenario had, until SYS-PERM-001 (test/scenario/perm.go).
func (c *APIClient) Clusters() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/clusters", c.baseURL)
	resp, err := c.httpClient.Get(url)
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
	url := fmt.Sprintf("%s/api/v1/instances/%s", c.baseURL, id)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/v1/instances/%s: %d, body: %s", id, resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// Events fetches events from /api/v1/events.
func (c *APIClient) Events() ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/events", c.baseURL)
	resp, err := c.httpClient.Get(url)
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
	url := fmt.Sprintf("%s/readyz", c.baseURL)
	resp, err := c.httpClient.Get(url)
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
	url := fmt.Sprintf("%s/healthz", c.baseURL)
	resp, err := c.httpClient.Get(url)
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
	url := c.baseURL + path
	resp, err := c.httpClient.Get(url)
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

// RawGet fetches an arbitrary endpoint and returns its raw text body — for
// non-JSON responses like /metrics (Prometheus text exposition), where Get
// would fail decoding.
func (c *APIClient) RawGet(path string) (string, error) {
	url := c.baseURL + path
	resp, err := c.httpClient.Get(url)
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
