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

func TestMigrations_0009_ContainsFindings(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0009_findings.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS findings")
	for _, index := range []string{
		"findings_lookup_idx",
		"findings_instance_idx",
		"findings_rule_idx",
	} {
		require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS "+index)
	}
	for _, state := range []string{"open", "degraded", "muted", "resolved"} {
		require.Contains(t, sql, "'"+state+"'")
	}
}
