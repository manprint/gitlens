package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// PGL-016: checks.logs defaults; an unknown source, a logs key under another
// check and a budget outside its range are configuration errors.
func TestPGL016_LogsConfig(t *testing.T) {
	base := `server: {url: http://localhost:8080, token: test}
identity_path: /var/lib/pglens/id
buffer: {path: /tmp/buf}
targets: [{name: pg, dsn: postgres://localhost/app}]
checks: `
	cfg, err := LoadConfig(writeConfigFile(t, base+`{}`))
	require.NoError(t, err)
	logs := cfg.Checks[LogsCheck]
	require.False(t, logs.LogsEnabled())
	require.Equal(t, LogSourceFile, logs.LogSource())
	require.False(t, logs.LogStatementText())
	require.False(t, logs.LogPlanParameters())
	require.Equal(t, DefaultLogBytesPerInterval, logs.LogBudget())
	require.Equal(t, 15*time.Second, cfg.LogInterval())
	require.Equal(t, filepath.Join("/var/lib/pglens", "pglog-state.json"), cfg.LogStatePath())

	cfg, err = LoadConfig(writeConfigFile(t, base+`{logs: {enabled: true, source: sql, statement_text: true, max_bytes_per_interval: 65536, interval: 30s, state_path: /tmp/s.json}}`))
	require.NoError(t, err)
	logs = cfg.Checks[LogsCheck]
	require.True(t, logs.LogsEnabled())
	require.Equal(t, LogSourceSQL, logs.LogSource())
	require.True(t, logs.LogStatementText())
	require.EqualValues(t, 65536, logs.LogBudget())
	require.Equal(t, 30*time.Second, cfg.LogInterval())
	require.Equal(t, "/tmp/s.json", cfg.LogStatePath())

	for body, msg := range map[string]string{
		`{logs: {source: syslog}}`:                   `checks.logs.source must be "file" or "sql", not "syslog"`,
		`{activity: {source: file}}`:                 "checks.activity: source, path",
		`{locks: {statement_text: true}}`:            "belong to checks.logs",
		`{logs: {max_bytes_per_interval: 1024}}`:     "checks.logs.max_bytes_per_interval must be between 65536 and 16777216",
		`{logs: {max_bytes_per_interval: 16777217}}`: "checks.logs.max_bytes_per_interval must be between",
	} {
		_, err := LoadConfig(writeConfigFile(t, base+body))
		require.ErrorContains(t, err, msg, body)
	}
}
