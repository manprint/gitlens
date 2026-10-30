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

// The rename endpoints rely on these indexes to refuse a duplicate name, so
// each must be unique, case-insensitive, scoped to the tenant, and leave a
// cleared name unconstrained. Both migrations must be safe to run again.
func TestMigrations_0022_0023_NameIndexes(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0022_cluster_name_unique.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS clusters_name_uniq ON clusters (tenant_id, lower(name)) WHERE name IS NOT NULL;")

	data, err = migrationFS.ReadFile("migrations/0023_instance_display_name.sql")
	require.NoError(t, err)
	sql = string(data)
	require.Contains(t, sql, "ALTER TABLE instances ADD COLUMN IF NOT EXISTS display_name text;")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS instances_display_name_uniq ON instances (tenant_id, lower(display_name)) WHERE display_name IS NOT NULL;")
}

// ADVO-001 (schema): the rule overrides table of 11.4 (D18) is keyed by tenant
// and rule, its tenant defaults to 'default', and the migration is safe to run
// again. The round trip itself runs on a real database (MIG-ADV-001,
// ADVO-INT-001).
func TestADVO001_MigrationDefinesRuleOverrides(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0024_advisor_rule_overrides.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS advisor_rule_overrides (")
	require.Contains(t, sql, "tenant_id text NOT NULL DEFAULT 'default',")
	require.Contains(t, sql, "rule_id text NOT NULL,")
	require.Contains(t, sql, "enabled boolean NOT NULL,")
	require.Contains(t, sql, "updated_at timestamptz NOT NULL DEFAULT now(),")
	require.Contains(t, sql, "PRIMARY KEY (tenant_id, rule_id)")
}

// ADVW-001 (schema): server_settings holds one jsonb value per tenant and key
// (D22c); a key without a row has its default.
func TestADVW001_MigrationDefinesServerSettings(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0025_server_settings.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS server_settings (")
	require.Contains(t, sql, "tenant_id text NOT NULL DEFAULT 'default',")
	require.Contains(t, sql, "key text NOT NULL,")
	require.Contains(t, sql, "value jsonb NOT NULL,")
	require.Contains(t, sql, "updated_at timestamptz NOT NULL DEFAULT now(),")
	require.Contains(t, sql, "PRIMARY KEY (tenant_id, key)")
}

// DL-MIG-001: 0026 makes db.deadlock_rate fire on any deadlock, and touches the
// rule only while it still carries every value 0006 seeded, so a threshold,
// comparator or duration an operator chose survives.
func TestDLMIG001_DeadlockRuleFiresOnAnyDeadlock(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0026_deadlock_alert_any.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "SET threshold = 0,")
	require.Contains(t, sql, "for_seconds = 0,")
	require.Contains(t, sql, "WHERE rule_id = 'db.deadlock_rate'")
	require.Contains(t, sql, "AND comparator = 'gt'")
	require.Contains(t, sql, "AND threshold = 0.1")
	require.Contains(t, sql, "AND for_seconds = 300;")

	seed, err := migrationFS.ReadFile("migrations/0006_alerts.sql")
	require.NoError(t, err)
	require.Contains(t, string(seed), "('db.deadlock_rate',             'warning',  'instance', 'pg_deadlocks_total',           'gt', 0.1,  300,",
		"0026 matches the seeded values; a changed seed needs a matching repair")
}

// NOTI-MIG-001: the console's webhook addresses (11.5b) are one row per tenant
// and kind, the kind limited to the three channels, with who set it and when.
func TestNOTIMIG001_MigrationDefinesNotificationChannels(t *testing.T) {
	data, err := migrationFS.ReadFile("migrations/0028_notification_channels.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS notification_channels (")
	require.Contains(t, sql, "tenant_id text NOT NULL DEFAULT 'default',")
	require.Contains(t, sql, "kind text NOT NULL CHECK (kind IN ('slack', 'webhook', 'google_chat')),")
	require.Contains(t, sql, "url text NOT NULL,")
	require.Contains(t, sql, "updated_at timestamptz NOT NULL DEFAULT now(),")
	require.Contains(t, sql, "updated_by text NOT NULL,")
	require.Contains(t, sql, "PRIMARY KEY (tenant_id, kind)")
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

// A migration whose name the runner cannot read used to be skipped without a
// word: 0016-events-index.sql never reached the database and the server ran on.
func TestParseMigrations_RefusesNamesItCannotApply(t *testing.T) {
	for _, name := range []string{"0016-events-index.sql", "0016 events.sql", "16_events.sql", "0016_.sql", "0016_events.txt"} {
		_, err := parseMigrations([]string{"0015_meta.sql", name})
		require.ErrorContains(t, err, name)
	}
}

func TestParseMigrations_RefusesDuplicateVersion(t *testing.T) {
	_, err := parseMigrations([]string{"0016_a.sql", "0016_b.sql"})
	require.ErrorContains(t, err, "both have version 16")
}

func TestParseMigrations_OrdersByVersion(t *testing.T) {
	got, err := parseMigrations([]string{"0010_c.sql", "0002_b.sql", "0001_a.sql"})
	require.NoError(t, err)
	require.Equal(t, []migration{{1, "0001_a.sql"}, {2, "0002_b.sql"}, {10, "0010_c.sql"}}, got)
}

// The embedded set is what Migrate applies, so it must pass the same rules.
func TestParseMigrations_EmbeddedSetIsValid(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	got, err := parseMigrations(names)
	require.NoError(t, err)
	require.Len(t, got, len(names))
	require.Equal(t, 1, got[0].version)
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
