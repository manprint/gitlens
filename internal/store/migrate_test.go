package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrations_0006_Parses(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0006_alerts.sql")
	require.NoError(t, err)
	require.NotEmpty(t, data)

	sql := string(data)
	for _, table := range []string{"alert_rules", "alerts", "silences", "notifications"} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
}

func TestMigrations_0007_ContainsHypertables(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0007_facts.sql")
	require.NoError(t, err)
	sql := string(data)
	for _, table := range []string{"metrics_tables", "metrics_indexes", "metrics_bloat", "object_facts"} {
		require.Contains(t, sql, "CREATE TABLE "+table)
	}
	require.Contains(t, sql, "create_hypertable('metrics_tables'")
	require.Contains(t, sql, "create_hypertable('metrics_indexes'")
	require.Contains(t, sql, "create_hypertable('metrics_bloat'")
}
