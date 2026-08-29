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
	require.Equal(t, "copy [redacted]", redactArchiveCommand("copy /var/lib --password=secret"))
	require.Equal(t, "", redactArchiveCommand(""))
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
