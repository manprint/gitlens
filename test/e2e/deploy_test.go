//go:build e2e

package e2e

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
	"testing"
	"time"
)

const deployBootstrapToken = "dev-token"

// TestFull_DeployExampleStack is SYS-DEPLOY-001: the checked-in deployment
// example must start unedited, become healthy, and expose one recently
// reporting instance through the documented collection endpoint.
func TestFull_DeployExampleStack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping deployment E2E test in short mode")
	}

	repoRoot := repositoryRoot(t)
	project := fmt.Sprintf("pglens-deploy-%d", os.Getpid())
	httpPort := freePort(t)
	postgresPort := freePort(t)
	agentPort := freePort(t)

	env := append(os.Environ(),
		fmt.Sprintf("PGLENS_HTTP_PORT=%d", httpPort),
		fmt.Sprintf("PGLENS_POSTGRES_PORT=%d", postgresPort),
		fmt.Sprintf("PGLENS_AGENT_HEALTHZ_PORT=%d", agentPort),
		fmt.Sprintf("PGLENS_BOOTSTRAP_TOKEN=%s", deployBootstrapToken),
	)
	compose := func(ctx context.Context, args ...string) ([]byte, error) {
		full := append([]string{"compose", "-p", project, "-f", "deploy/docker-compose.yml"}, args...)
		cmd := exec.CommandContext(ctx, "docker", full...)
		cmd.Dir = repoRoot
		cmd.Env = env
		return cmd.CombinedOutput()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cleanupCancel()
		if out, err := compose(cleanupCtx, "down", "-v", "--remove-orphans"); err != nil {
			t.Logf("deployment cleanup failed: %v\n%s", err, out)
		}
	}()

	if out, err := compose(ctx, "up", "-d", "--wait", "--wait-timeout", "180"); err != nil {
		logs, _ := compose(context.Background(), "logs", "--no-color")
		t.Fatalf("SYS-DEPLOY-001: compose up failed: %v\n%s\nlogs:\n%s", err, out, logs)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if instances, err := deploymentInstances(client, baseURL, deployBootstrapToken); err == nil && len(instances) == 1 {
			if age := time.Since(instances[0].LastSeen); age >= 0 && age < 2*time.Minute && instances[0].Up {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("SYS-DEPLOY-001: waiting for recent instance: %v", ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}

	logs, _ := compose(context.Background(), "logs", "--no-color")
	t.Fatalf("SYS-DEPLOY-001: no recent instance from GET /api/v1/instances\nlogs:\n%s", logs)
}

type deploymentInstance struct {
	LastSeen time.Time `json:"last_seen"`
	Up       bool      `json:"up"`
}

func deploymentInstances(client *http.Client, baseURL, token string) ([]deploymentInstance, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/instances", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/v1/instances: %s: %s", resp.Status, body)
	}
	var instances []deploymentInstance
	if err := json.NewDecoder(resp.Body).Decode(&instances); err != nil {
		return nil, err
	}
	return instances, nil
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
