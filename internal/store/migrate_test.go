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

func TestMigrations_0010_ContainsCommands(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0010_commands.sql")
	require.NoError(t, err)
	sql := string(data)
	for _, table := range []string{"commands", "command_audit", "query_plans"} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
	for _, index := range []string{
		"commands_queue_idx",
		"commands_instance_idx",
		"command_audit_instance_idx",
		"query_plans_dedup_idx",
		"query_plans_lookup_idx",
	} {
		require.Contains(t, sql, "CREATE ")
		require.Contains(t, sql, index)
	}
	for _, kind := range []string{"explain", "cancel", "terminate", "pgstattuple"} {
		require.Contains(t, sql, "'"+kind+"'")
	}
	for _, state := range []string{"pending", "claimed", "done", "failed", "expired"} {
		require.Contains(t, sql, "'"+state+"'")
	}
}

func TestMigrations_0012_ContainsTargetNameIdentity(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0012_target_name.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "ALTER TABLE instances ADD COLUMN target_name text")
	require.Contains(t, sql, "instances_target_name_idx")
}
