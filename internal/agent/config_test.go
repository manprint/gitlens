package agent

import (
	"fmt"
	"os"
	"path/filepath"
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

// TestMountWarning verifies that isMountWarning detects root filesystem paths correctly.
func TestMountWarning(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		mountinfo   string
		wantWarning bool
	}{
		{
			name: "root_filesystem_path",
			path: "/var/lib/pglens",
			mountinfo: `1 0 0:1 / / rw,relatime - rootfs rootfs rw
2 1 0:2 / /dev rw,relatime - devtmpfs devtmpfs rw
3 1 0:3 / /proc rw,relatime - proc proc rw
`,
			wantWarning: true,
		},
		{
			name: "separate_mount",
			path: "/var/lib/pglens",
			mountinfo: `1 0 0:1 / / rw,relatime - rootfs rootfs rw
2 1 0:2 / /dev rw,relatime - devtmpfs devtmpfs rw
3 1 0:3 / /proc rw,relatime - proc proc rw
4 1 8:1 / /var/lib/pglens rw,relatime - ext4 /dev/sda1 rw
`,
			wantWarning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile := filepath.Join(t.TempDir(), "mountinfo")
			err := os.WriteFile(tmpfile, []byte(tt.mountinfo), 0600)
			require.NoError(t, err)

			warning, err := isMountWarning(tt.path, tmpfile)
			require.NoError(t, err)
			require.Equal(t, tt.wantWarning, warning)
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
