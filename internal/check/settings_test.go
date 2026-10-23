package check

import (
	"context"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestSettings_AllowlistIsSorted(t *testing.T) {
	require.True(t, sort.StringsAreSorted(settingsAllowlist))
}

func TestSettings_EmitsNonDefaultOutsideAllowlist(t *testing.T) {
	r := scrapeSettingsForTest(t, []any{"custom_setting", "42", "", "configuration file", "1", "1", false, "integer", "user", "custom"})
	require.Len(t, r.Facts, 1)
	require.Equal(t, "custom_setting", r.Facts[0].Key)
}

// Values the agent's session, its client or its role set describe pglens's
// connection, not the instance, even for an allowlisted setting.
func TestSettings_SkipsConnectionSpecificValues(t *testing.T) {
	r := scrapeSettingsForTest(t,
		[]any{"statement_timeout", "5000", "ms", "session", "0", "15000", false, "integer", "user", "timeout"},
		[]any{"lock_timeout", "1000", "ms", "user", "0", "1000", false, "integer", "user", "timeout"},
		[]any{"application_name", "pglens", "", "client", "", "pglens", false, "string", "user", "name"},
		[]any{"work_mem", "8192", "kB", "database", "4096", "8192", true, "integer", "user", "memory"},
	)
	require.Len(t, r.Facts, 1)
	require.Equal(t, "work_mem", r.Facts[0].Key)
	require.Equal(t, 1.0, metricValue(r, "pg_settings_pending_restart", nil))
	require.Equal(t, -1.0, metricValue(r, "pg_setting_seconds", map[string]string{"name": "statement_timeout"}))
}

func TestSettings_SkipsDefaultOutsideAllowlist(t *testing.T) {
	r := scrapeSettingsForTest(t, []any{"custom_setting", "1", "", "default", "1", "1", false, "integer", "user", "custom"})
	require.Empty(t, r.Facts)
}

func TestSettings_ByteUnitNormalisation(t *testing.T) {
	for _, tc := range []struct {
		unit string
		want float64
	}{
		{"8kB", 8 * 8 * 1024}, {"kB", 8 * 1024}, {"MB", 8 * 1024 * 1024}, {"GB", 8 * 1024 * 1024 * 1024},
	} {
		got, ok := normalizeSetting("8", tc.unit)
		require.True(t, ok, tc.unit)
		require.Equal(t, tc.want, got, tc.unit)
	}
}

func TestSettings_TimeUnitNormalisation(t *testing.T) {
	for _, tc := range []struct {
		unit string
		want float64
	}{
		{"ms", 0.5}, {"s", 500}, {"min", 30000},
	} {
		got, ok := normalizeSetting("500", tc.unit)
		require.True(t, ok, tc.unit)
		require.Equal(t, tc.want, got, tc.unit)
	}
}

func TestSettings_UnknownUnitIsSkipped(t *testing.T) {
	got, ok := normalizeSetting("8", "blocks")
	require.False(t, ok)
	require.Zero(t, got)
}

func TestSettings_ArchiveCommandIsRedacted(t *testing.T) {
	require.Equal(t, "copy [redacted]", redactCommand("copy /var/lib --password=secret"))
	require.Equal(t, "", redactCommand(""))
	require.Equal(t, "/bin/true", redactCommand("/bin/true"))
	require.Equal(t, "wal-g [redacted]", redactCommand("AWS_SECRET_ACCESS_KEY=abc wal-g wal-push %p"))
	require.Equal(t, "[redacted]", redactCommand("PGPASSWORD=abc"))
}

// TestSettings_CredentialsNeverLeaveTheHost covers every non-default setting
// that routinely embeds a secret, not only archive_command.
func TestSettings_CredentialsNeverLeaveTheHost(t *testing.T) {
	for name, value := range map[string]string{
		"restore_command":         "AWS_SECRET_ACCESS_KEY=s3cr3t wal-g wal-fetch %f %p",
		"archive_cleanup_command": "pg_archivecleanup -p s3cr3t /archive %r",
		"recovery_end_command":    "curl -u admin:s3cr3t https://hooks.example/end",
		"ssl_passphrase_command":  "echo s3cr3t",
		"primary_conninfo":        "host=primary user=replicator password='s3cr3t' application_name=standby1",
	} {
		got := redactSetting(name, value)
		require.NotContains(t, got, "s3cr3t", name)
	}
	require.Equal(t, "host=p password=[redacted] sslpassword = [redacted] port=5432",
		redactConninfo("host=p password=abc sslpassword = 'x y' port=5432"))
	require.Equal(t, "postgresql://rep:[redacted]@primary:5432/postgres",
		redactConninfo("postgresql://rep:abc@primary:5432/postgres"))
	require.Equal(t, "on", redactSetting("hot_standby", "on"))
}

func TestSettings_ByteAndMicrosecondUnits(t *testing.T) {
	got, ok := normalizeSetting("1024", "B")
	require.True(t, ok)
	require.Equal(t, 1024.0, got)
	got, ok = normalizeSetting("2500", "us")
	require.True(t, ok)
	require.InDelta(t, 0.0025, got, 1e-12)
	require.Equal(t, "bytes", settingUnitKind("B"))
	require.Equal(t, "seconds", settingUnitKind("us"))
}

func TestSettings_PendingRestartCount(t *testing.T) {
	r := scrapeSettingsForTest(t,
		[]any{"max_connections", "100", "", "configuration file", "100", "100", true, "integer", "postmaster", "connections"},
		[]any{"work_mem", "64", "MB", "configuration file", "4", "4", false, "integer", "user", "memory"},
	)
	var found bool
	for _, metric := range r.Metrics {
		if metric.Name == "pg_settings_pending_restart" {
			found = true
			require.Equal(t, float64(1), metric.Value)
		}
	}
	require.True(t, found)
}

func scrapeSettingsForTest(t *testing.T, rows ...[]any) Result {
	t.Helper()
	conn := &mockConn{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: rows}, nil
		},
	}
	r, err := (&settingsCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}
