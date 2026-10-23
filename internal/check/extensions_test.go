package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestExtensions_Requirements(t *testing.T) {
	c := &extensionsCheck{}
	require.Equal(t, "extensions", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Empty(t, c.Requires().Extensions)
	require.Equal(t, time.Hour, c.DefaultInterval())
}

func TestExtensions_ReportsVersionsAndUpgrades(t *testing.T) {
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		require.Equal(t, extensionsQuery, sql)
		require.Equal(t, []any{extensionFactLimit + 1}, args)
		return &mockRows{rows: [][]any{
			{"plpgsql", "pg_catalog", "1.0", "1.0"},
			{"postgis", "public", "3.5.2", "3.6.4"},
			{"retired", "public", "2.0", ""},
		}}, nil
	}}
	r, err := (&extensionsCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	require.False(t, r.Truncated)
	require.Equal(t, []Fact{
		{Kind: extensionFactKind, Key: "plpgsql", ValueText: "1.0", Labels: map[string]string{"schema": "pg_catalog", "default_version": "1.0", "upgrade": "none"}},
		{Kind: extensionFactKind, Key: "postgis", ValueText: "3.5.2", Labels: map[string]string{"schema": "public", "default_version": "3.6.4", "upgrade": "available"}},
		{Kind: extensionFactKind, Key: "retired", ValueText: "2.0", Labels: map[string]string{"schema": "public", "default_version": "", "upgrade": "unknown"}},
	}, r.Facts)
	require.ElementsMatch(t, []pgtype.Metric{
		{Name: "pg_extensions_installed", Value: 3, Kind: pgtype.KindGauge},
		{Name: "pg_extensions_upgrade_available", Value: 1, Kind: pgtype.KindGauge},
	}, r.Metrics)
}

func TestExtensionUpgrade(t *testing.T) {
	for _, tc := range []struct{ installed, available, want string }{
		{"1.0", "1.0", "none"},
		{"1.9", "1.10", "available"},
		{"3.6", "3.6.1", "available"},
		{"3.6.0", "3.6", "none"},
		{"2.1", "2.0", "none"},
		{"1.5beta", "1.5", "unknown"},
		{"1.0", "", "unknown"},
	} {
		require.Equal(t, tc.want, extensionUpgrade(tc.installed, tc.available), "%s -> %s", tc.installed, tc.available)
	}
}
