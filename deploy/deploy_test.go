package deploy

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// composeFile is the part of a compose file these tests read.
type composeFile struct {
	Services map[string]struct {
		Environment map[string]string `yaml:"environment"`
		Ports       []string          `yaml:"ports"`
		Volumes     []string          `yaml:"volumes"`
	} `yaml:"services"`
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(".", name))
	require.NoError(t, err)
	return data
}

// The example agent must not monitor the cluster as its superuser. It
// connects as pglens_monitor, which the example stack creates on first boot
// by running the shipped monitoring script, and takes the password from
// PGPASSWORD so the example file holds no secret.
func TestExampleStack_AgentConnectsAsMonitoringRole(t *testing.T) {
	var agent struct {
		Targets []struct {
			DSN string `yaml:"dsn"`
		} `yaml:"targets"`
	}
	require.NoError(t, yaml.Unmarshal(readFile(t, "agent.example.yaml"), &agent))
	require.NotEmpty(t, agent.Targets)
	for _, target := range agent.Targets {
		dsn, err := url.Parse(target.DSN)
		require.NoError(t, err)
		require.Equal(t, "pglens_monitor", dsn.User.Username(), target.DSN)
		_, hasPassword := dsn.User.Password()
		require.False(t, hasPassword, "the password must come from PGPASSWORD")
	}

	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	db := stack.Services["timescaledb"]
	require.Contains(t, db.Volumes, "./sql/monitoring_user.sql:/opt/pglens/monitoring_user.sql:ro")
	require.Contains(t, db.Volumes, "./sql/init_monitoring_user.sh:/docker-entrypoint-initdb.d/01-monitoring.sh:ro")
	require.Contains(t, string(readFile(t, filepath.Join("sql", "init_monitoring_user.sh"))), "role=pglens_monitor")
	require.NotEmpty(t, stack.Services["pglens-agent"].Environment["PGPASSWORD"])
	require.True(t, strings.HasPrefix(stack.Services["pglens-agent"].Environment["PGPASSWORD"], "${PGLENS_MONITORING_PASSWORD"))
}

// The example stacks publish their ports on loopback only, and take the
// superuser password and the bearer token from the environment with no
// default: a published superuser port or a shipped token reachable from the
// network is an open door.
func TestExampleStack_PublishesLoopbackOnlyAndRequiresSecrets(t *testing.T) {
	for _, name := range []string{"docker-compose.yml", filepath.Join("compose", "timescaledb.yml")} {
		var stack composeFile
		require.NoError(t, yaml.Unmarshal(readFile(t, name), &stack))
		for service, s := range stack.Services {
			for _, port := range s.Ports {
				require.True(t, strings.HasPrefix(port, "127.0.0.1:"), "%s: %s publishes %q on every interface", name, service, port)
			}
		}
	}

	var stack composeFile
	require.NoError(t, yaml.Unmarshal(readFile(t, "docker-compose.yml"), &stack))
	for service, key := range map[string]string{
		"timescaledb":   "POSTGRES_PASSWORD",
		"pglens-server": "PGLENS_BOOTSTRAP_TOKEN",
		"pglens-agent":  "PGLENS_BOOTSTRAP_TOKEN",
	} {
		require.Contains(t, stack.Services[service].Environment[key], ":?", "%s %s has a default", service, key)
	}
}
