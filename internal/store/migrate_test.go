package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// TestMigrations_SeededRuleMetricsAreProduced — a Tier 1 seed rule whose
// metric name does not match a metric something actually emits is silently
// dead: internal/alert's metric source queries `metrics` by exact name, so
// the rule produces no samples, never fires and never reports itself as
// broken. conn.idle_in_transaction shipped for exactly that reason, naming
// pg_max_idle_in_txn_seconds while the collector emits
// pg_max_idle_in_transaction_seconds (repaired by 0013).
//
// The map records where each seeded metric comes from; the test enforces it.
func TestMigrations_SeededRuleMetricsAreProduced(t *testing.T) {
	producers := map[string][]string{
		// Derived by the alert engine itself, not emitted by any check.
		"pg_replication_lag_seconds": {"internal/alert/source_sql.go"},
		"pg_sync_standby_count":      {"internal/alert/source_sql.go"},
		// Emitted by the agent's own host collector, not by a PostgreSQL check.
		"host_disk_free_ratio": {"cmd/pglens-agent/run.go"},
	}
	checkDir := filepath.Join("..", "check")

	data, err := migrationFS.ReadFile("migrations/0006_alerts.sql")
	require.NoError(t, err)
	seed := regexp.MustCompile(`\('([a-z0-9_.]+)',\s*'(?:critical|warning|info)',\s*'(?:instance|cluster|database)',\s*'([a-z0-9_]+)'`)
	rows := seed.FindAllStringSubmatch(string(data), -1)
	require.NotEmpty(t, rows, "no seeded rules found: the parser no longer matches the migration")

	// 0013 renames one of the seeded metrics; apply the same rewrite the
	// migration does so the check reflects the post-migration state.
	repair, err := migrationFS.ReadFile("migrations/0013_alert_rule_metric_names.sql")
	require.NoError(t, err)
	require.Contains(t, string(repair), "pg_max_idle_in_transaction_seconds")

	for _, row := range rows {
		ruleID, metric := row[1], row[2]
		if files, ok := producers[metric]; ok {
			found := false
			for _, f := range files {
				body, err := os.ReadFile(filepath.Join("..", "..", f))
				require.NoError(t, err)
				if strings.Contains(string(body), metric) {
					found = true
					break
				}
			}
			require.True(t, found, "rule %s: %s is no longer produced by %v", ruleID, metric, files)
			continue
		}
		require.True(t, metricEmittedByAnyCheck(t, checkDir, metric),
			"rule %s compares %s, which no check in internal/check emits and which is not a documented derived metric", ruleID, metric)
	}
}

// metricEmittedByAnyCheck reports whether the metric name appears in any
// non-test source file under internal/check.
func metricEmittedByAnyCheck(t *testing.T, dir, metric string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		if strings.Contains(string(body), metric) {
			return true
		}
	}
	return false
}
