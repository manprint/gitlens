package agent

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConfig_UnknownKeyIsAnError verifies that unknown YAML keys cause parse errors.
func TestConfig_UnknownKeyIsAnError(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
push_interval: 15s
buffer:
  path: /var/lib/pglens/buffer
  max_size: 512MiB
  max_age: 6h
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
    cluster_name: test-cluster
    databases:
      max: 10
checks:
  activity:
    interval: 10s
unknown_key: should_fail
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	_, err := LoadConfig(tmpfile)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown")
}

// TestConfig_Defaults verifies that omitted fields take documented defaults.
func TestConfig_Defaults(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	cfg, err := LoadConfig(tmpfile)
	require.NoError(t, err)
	// If not specified in YAML, GetParsedPushInterval returns default of 15s
	require.Equal(t, 15*time.Second, cfg.GetParsedPushInterval())
}

func TestConfig_LocksDefaultInterval(t *testing.T) {
	yaml := `server: {url: http://localhost:8080, token: test}
identity_path: /tmp/id
buffer: {path: /tmp/buf}
targets: [{name: pg, dsn: postgres://localhost/app}]
checks: {locks: {}}`
	path := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(path) }()
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), cfg.GetCheckInterval("locks"))
}

func TestConfig_ByApplicationDefaultsFalse(t *testing.T) {
	yaml := `server: {url: http://localhost:8080, token: test}
identity_path: /tmp/id
buffer: {path: /tmp/buf}
targets: [{name: pg, dsn: postgres://localhost/app}]
checks: {activity: {}}`
	path := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(path) }()
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.False(t, cfg.Checks["activity"].ByApplication)
}

func TestConfig_ExampleFileParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "deploy", "agent.example.yaml"))
	require.NoError(t, err)
	text := strings.Replace(string(data), "token_file: /run/secrets/pglens_token", "token: test", 1)
	path := writeConfigFile(t, text)
	defer func() { _ = os.Remove(path) }()
	_, err = LoadConfig(path)
	require.NoError(t, err)
}

func TestConfig_ParsesBufferAndCheckOverrides(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
  max_size: 64KiB
  max_age: 2h
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity:
    interval: 2s
    top_n: 7
`
	cfg, err := LoadConfig(writeConfigFile(t, yaml))
	require.NoError(t, err)
	require.Equal(t, int64(64*1024), cfg.GetParsedBufferMaxSize())
	require.Equal(t, 2*time.Hour, cfg.GetParsedBufferMaxAge())
	require.Equal(t, 2*time.Second, cfg.GetCheckInterval("activity"))
}

// TestConfig_EnvOverrides verifies that environment variables override file settings.
func TestConfig_EnvOverrides(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token: file-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	// Set environment overrides
	t.Setenv("PGLENS_SERVER_URL", "http://override:9999")
	t.Setenv("PGLENS_BOOTSTRAP_TOKEN", "env-token")

	cfg, err := LoadConfig(tmpfile)
	require.NoError(t, err)
	require.Equal(t, "http://override:9999", cfg.Server.URL)
	require.Equal(t, "env-token", cfg.Server.Token)
}

// TestConfig_TokenFileRead verifies that token files are read and trailing whitespace is stripped.
func TestConfig_TokenFileRead(t *testing.T) {
	tmpdir := t.TempDir()
	tokenFile := filepath.Join(tmpdir, "token")
	err := os.WriteFile(tokenFile, []byte("secret-token\n  \n"), 0600)
	require.NoError(t, err)

	yaml := fmt.Sprintf(`
server:
  url: http://localhost:8080
  token_file: %s
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`, tokenFile)
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	cfg, err := LoadConfig(tmpfile)
	require.NoError(t, err)
	require.Equal(t, "secret-token", cfg.Server.Token)
}

// TestConfig_TokenFileRead_Unreadable verifies that unreadable token files produce clear errors.
func TestConfig_TokenFileRead_Unreadable(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token_file: /nonexistent/token/file
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	_, err := LoadConfig(tmpfile)
	require.Error(t, err)
	require.Contains(t, err.Error(), "token file")
}

// TestConfig_DSNNeverLogged verifies that the DSN (with password) never appears in Config string or error messages.
func TestConfig_DSNNeverLogged(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens:secretpassword@localhost/postgres
checks:
  activity: {}
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	cfg, err := LoadConfig(tmpfile)
	require.NoError(t, err)

	// Check that Config.String() doesn't contain the password
	configStr := cfg.String()
	require.NotContains(t, configStr, "secretpassword")
	require.NotContains(t, configStr, "postgres://pglens:secretpassword")
}

// TestConfig_Validation verifies that invalid configurations are rejected with clear messages.
func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		yaml   string
		errMsg string
	}{
		{
			name: "empty_targets",
			yaml: `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets: []
checks:
  activity: {}
`,
			errMsg: "targets",
		},
		{
			name: "malformed_dsn",
			yaml: `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: not-a-dsn
checks:
  activity: {}
`,
			errMsg: "dsn",
		},
		{
			name: "negative_max_databases",
			yaml: `
server:
  url: http://localhost:8080
  token: test-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
    databases:
      max: -1
checks:
  activity: {}
`,
			errMsg: "max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile := writeConfigFile(t, tt.yaml)
			defer func() { _ = os.Remove(tmpfile) }()

			_, err := LoadConfig(tmpfile)
			require.Error(t, err, "expected validation error for %s", tt.name)
			require.Contains(t, err.Error(), tt.errMsg, "error should mention %s", tt.errMsg)
		})
	}
}

// Helper function to write config to temp file
func writeConfigFile(t *testing.T, yaml string) string {
	tmpfile, err := os.CreateTemp(t.TempDir(), "config-*.yaml")
	require.NoError(t, err)
	defer func() { _ = tmpfile.Close() }()

	_, err = tmpfile.WriteString(yaml)
	require.NoError(t, err)

	return tmpfile.Name()
}

// TestConfig_ServerURLLosesTrailingSlash: the push path is appended to
// server.url, so a trailing slash from the file or from PGLENS_SERVER_URL is
// removed before anything builds a request from it.
func TestConfig_ServerURLLosesTrailingSlash(t *testing.T) {
	yaml := `
server:
  url: http://localhost:8080/
  token: file-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`
	tmpfile := writeConfigFile(t, yaml)
	defer func() { _ = os.Remove(tmpfile) }()

	cfg, err := LoadConfig(tmpfile)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:8080", cfg.Server.URL)

	t.Setenv("PGLENS_SERVER_URL", "http://override:9999//")
	cfg, err = LoadConfig(tmpfile)
	require.NoError(t, err)
	require.Equal(t, "http://override:9999", cfg.Server.URL)
}

// TestConfig_PlainHTTPServerURLIsLogged: the bearer token goes unencrypted over
// plain http to any host but this machine, so the config load says so. Loopback
// and https stay quiet, and an environment override is checked like the file.
func TestConfig_PlainHTTPServerURLIsLogged(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		env     string
		wantLog bool
	}{
		{name: "remote_hostname_http", url: "http://pglens.example.com:8080", wantLog: true},
		{name: "compose_service_http", url: "http://pglens-server:8080", wantLog: true},
		{name: "private_address_http", url: "http://10.0.0.5:8080", wantLog: true},
		{name: "localhost_http", url: "http://localhost:8080", wantLog: false},
		{name: "loopback_ipv4_http", url: "http://127.0.0.1:8080", wantLog: false},
		{name: "loopback_ipv6_http", url: "http://[::1]:8080", wantLog: false},
		{name: "remote_https", url: "https://pglens.example.com", wantLog: false},
		{name: "env_override_remote_http", url: "http://localhost:8080", env: "http://pglens.example.com:8080", wantLog: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yaml := fmt.Sprintf(`
server:
  url: %s
  token: file-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`, tt.url)
			t.Setenv("PGLENS_SERVER_URL", tt.env)

			var logged bytes.Buffer
			prev := log.Writer()
			log.SetOutput(&logged)
			t.Cleanup(func() { log.SetOutput(prev) })

			_, err := LoadConfig(writeConfigFile(t, yaml))
			require.NoError(t, err)
			require.Equal(t, tt.wantLog, strings.Contains(logged.String(), "plain http"), "log: %q", logged.String())
		})
	}
}

// TestConfig_PlainHTTPWarningNamesOnlyTheHost: the warning names the server
// host without its port, and a load writes it once.
func TestConfig_PlainHTTPWarningNamesOnlyTheHost(t *testing.T) {
	yaml := `
server:
  url: http://pglens.example.com:8080
  token: file-token
identity_path: /var/lib/pglens/identity.json
buffer:
  path: /var/lib/pglens/buffer
targets:
  - name: pg-app
    dsn: postgres://pglens@localhost/postgres
checks:
  activity: {}
`
	t.Setenv("PGLENS_SERVER_URL", "")

	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prev) })

	_, err := LoadConfig(writeConfigFile(t, yaml))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logged.String(), "plain http"), "log: %q", logged.String())
	require.Contains(t, logged.String(), "host pglens.example.com is reached")
	require.NotContains(t, logged.String(), "8080")
}
