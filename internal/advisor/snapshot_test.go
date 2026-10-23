package advisor

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_MetricHelperUsesCanonicalLabels(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{
		"host_load1": {"a=1\x1fb=2": 3.5},
	}}
	v, ok := s.Metric("host_load1", map[string]string{"b": "2", "a": "1"})
	require.True(t, ok)
	require.Equal(t, 3.5, v)
}

func TestSnapshot_MetricMissingReturnsFalse(t *testing.T) {
	s := &Snapshot{Metrics: map[string]map[string]float64{}}
	_, ok := s.Metric("missing", nil)
	require.False(t, ok)
}

func snapshotStringPtr(value string) *string { return &value }

func TestLoadSnapshot_PopulatesBoundedSections(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	ctx := context.Background()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	firstSeen := now.Add(-40 * 24 * time.Hour)
	instanceID := uuid.New()
	siblingID := uuid.New()
	backupAt := now.Add(-3 * 24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT cluster_id, role FROM instances")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"cluster_id", "role"}).AddRow(int64(42), "primary"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_version, perm_tier, first_seen FROM instances")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"pg_version", "perm_tier", "first_seen"}).AddRow(170004, "T2", firstSeen))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (metric, labels)")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"metric", "labels", "value", "ts"}).
			AddRow("host_mem_total_bytes", []byte(`{}`), 1000.0, now).
			AddRow("host_mem_available_bytes", []byte(`{}`), 400.0, now).
			AddRow("host_metrics_source", []byte(`{}`), 2.0, now).
			AddRow("pg_backends", []byte(`{"state":"active","wait_event_type":"CPU"}`), 2.0, now.Add(-10*time.Second)).
			AddRow("pg_backends", []byte(`{"state":"idle","wait_event_type":"Client"}`), 6.0, now.Add(-10*time.Second)).
			AddRow("pg_backends", []byte(`{"state":"idle in transaction","wait_event_type":"Client"}`), 5.0, now.Add(-5*time.Minute)).
			AddRow("pg_setting_bytes", []byte(`{"name":"work_mem"}`), 4096.0, now).
			AddRow("pg_setting_seconds", []byte(`{"name":"statement_timeout"}`), 30.0, now))
	mock.ExpectQuery(regexp.QuoteMeta("COALESCE(SUM(value) FILTER (WHERE metric='pg_checkpoints_requested_total'), 0)")).WithArgs(instanceID, now.Add(-24*time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"requested", "timed", "count"}).AddRow(1.0, 3.0, int64(10)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT kind, key, labels")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"kind", "key", "labels", "value_text", "last_seen", "datname", "value_json"}).
			AddRow("setting", "max_connections", []byte(`{"context":"postmaster"}`), "100", now, "", nil).
			AddRow("setting", "work_mem", []byte(`{"unit":"kB","context":"user"}`), "8192", now, "", nil).
			AddRow("setting", "archive_timeout", []byte(`{"unit":"s","context":"sighup"}`), "60", now, "", nil).
			AddRow("setting", "reset_to_default", []byte(`{}`), "stale", now.Add(-2*time.Hour), "", nil).
			AddRow("backup", "basebackup_running", []byte(`{}`), "1", backupAt, "", nil).
			AddRow("index_def", "public.orders_customer_idx", []byte(`{"def_hash":"hash-other"}`), "CREATE INDEX orders_customer_idx ON public.orders(other_id)", now, "other", nil).
			AddRow("index_def", "public.orders_customer_idx", []byte(`{"def_hash":"hash-new"}`), "CREATE INDEX orders_customer_idx ON public.orders(customer_id)", now, "app", nil).
			AddRow("check_skip", "stat_statements", []byte(`{}`), "permission denied", now, "", nil).
			AddRow("trigger", "public.orders.audit", []byte(`{"state":"disabled"}`), "", now, "app", []byte(`{"function":"public.audit"}`)).
			AddRow("trigger", "public.orders.audit", []byte(`{"state":"enabled"}`), "", now, "other", nil).
			AddRow("check_skip", "table_stats", []byte(`{}`), "not collected", now, "", nil).
			AddRow("check_skip", "index_stats", []byte(`{}`), "not collected", now, "", nil).
			AddRow("check_skip", "bloat_estimate", []byte(`{}`), "not collected", now, "", nil))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT datname, schemaname, relname, n_live_tup")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "schemaname", "relname", "n_live_tup", "n_dead_tup", "n_mod_since_analyze", "last_autovacuum", "last_vacuum", "relfrozenxid_age", "seq_scan", "idx_scan"}).
			AddRow("app", "public", "orders", 100.0, 4.0, 2.0, nil, nil, 10.0, 3.0, 1.0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (datname, schemaname, indexrelname)")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "schemaname", "relname", "indexrelname", "peak_scan_rate", "index_bytes", "is_primary", "is_unique", "is_valid", "def_hash", "history_days"}).
			AddRow("app", "public", "orders", "orders_customer_idx", 0.25, 2048.0, false, true, true, "hash-old", 2.5))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT datname, schemaname, relname, indexrelname, object_kind, real_bytes")).WithArgs(instanceID, now.Add(-7*24*time.Hour), now.Add(-13*time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "schemaname", "relname", "indexrelname", "object_kind", "real_bytes", "bloat_ratio", "method"}).
			AddRow("app", "public", "orders", "", "table", 4096.0, 0.25, "estimate").
			AddRow("app", "public", "orders", "orders_customer_idx", "index", 2048.0, 0.1, "pgstattuple"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT datname, queryid,")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "queryid", "calls", "exec_ms", "temp_blks", "blks_read", "blks_hit"}).
			AddRow("app", int64(7), 120.0, 600.0, 10.0, 3.0, 5.0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT queryid,")).WithArgs(instanceID, now.Add(-8*24*time.Hour), now.Add(-time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"queryid", "mean"}).AddRow(int64(7), 4.5))
	siblingSeen := now.Add(-time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT i.instance_id, i.role, f.kind, f.key")).WithArgs(int64(42), instanceID, now.Add(-time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"instance_id", "role", "kind", "key", "value_text", "def_hash", "context", "last_idx_scan", "datname", "last_seen"}).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("setting"), snapshotStringPtr("work_mem"), snapshotStringPtr("8192"), snapshotStringPtr(""), snapshotStringPtr("user"), snapshotStringPtr(""), snapshotStringPtr(""), &siblingSeen).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("setting"), snapshotStringPtr("stale_setting"), snapshotStringPtr("x"), snapshotStringPtr(""), snapshotStringPtr("user"), snapshotStringPtr(""), snapshotStringPtr(""), snapshotTimePtr(now.Add(-3*time.Hour))).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("index_def"), snapshotStringPtr("public.orders_customer_idx"), snapshotStringPtr(""), snapshotStringPtr("hash-sibling"), snapshotStringPtr(""), snapshotStringPtr(""), snapshotStringPtr("app"), &siblingSeen).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("index_def"), snapshotStringPtr("public.orders_label_idx"), snapshotStringPtr(""), snapshotStringPtr("hash-label"), snapshotStringPtr(""), snapshotStringPtr("2026-08-29T09:00:00Z"), snapshotStringPtr("app"), &siblingSeen).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("backup"), snapshotStringPtr("basebackup_running"), snapshotStringPtr("1"), snapshotStringPtr(""), snapshotStringPtr(""), snapshotStringPtr(""), snapshotStringPtr(""), &siblingSeen).
			AddRow(uuid.New(), snapshotStringPtr("standby"), nil, nil, snapshotStringPtr(""), snapshotStringPtr(""), snapshotStringPtr(""), snapshotStringPtr(""), nil, nil))
	mock.ExpectQuery(regexp.QuoteMeta("FROM metrics_indexes m")).WithArgs(int64(42), instanceID, now.Add(-time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"instance_id", "datname", "schemaname", "indexrelname"}).
			AddRow(siblingID, "app", "public", "orders_metrics_idx"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (slot_name, src)")).WithArgs(instanceID, now.Add(-5*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"standbys", "max_lag", "slots"}).AddRow(2, 1024.0, 1))

	s, err := LoadSnapshot(ctx, mock, instanceID, now)
	require.NoError(t, err)
	require.Equal(t, int64(42), s.ClusterID)
	require.Equal(t, instanceID, s.InstanceID)
	require.Equal(t, "primary", string(s.Role))
	require.Equal(t, 170004, int(s.PGVersion))
	require.Equal(t, "T2", s.PermTier.String())
	require.Equal(t, firstSeen, s.FirstSeen)
	require.Equal(t, "100", s.Settings["max_connections"])
	require.Equal(t, "postmaster", s.SettingContexts["max_connections"])
	require.Equal(t, "4096", s.Settings["work_mem_bytes"], "the metric is the fact's normalisation and wins")
	require.Equal(t, "60", s.Settings["archive_timeout_seconds"], "normalised from the fact while the hourly metric is out of the window")
	require.Equal(t, "30", s.Settings["statement_timeout_seconds"])
	require.NotContains(t, s.Settings, "reset_to_default", "a setting the latest scrape did not ship is stale")
	require.Equal(t, 0.75, s.Metrics["pg_connections_idle_share"][""], "only the latest activity scrape counts")
	require.Equal(t, 0.25, s.Metrics["pg_checkpoints_requested_ratio"][""])
	require.Equal(t, backupAt, s.Facts["backup"]["basebackup_running"].ObservedAt)
	require.Equal(t, "hash-new", s.Facts["index_def"][indexHashKey("app", "public.orders_customer_idx")].Labels["def_hash"])
	// The same object name in two databases stays two facts.
	require.Len(t, s.Facts["trigger"], 2)
	appTrigger := s.Facts["trigger"][databaseFactKey("app", "public.orders.audit")]
	require.Equal(t, "app", appTrigger.Datname)
	require.Equal(t, "disabled", appTrigger.Labels["state"])
	require.Equal(t, "public.audit", appTrigger.Value["function"])
	require.Equal(t, "enabled", s.Facts["trigger"][databaseFactKey("other", "public.orders.audit")].Labels["state"])
	require.Equal(t, "permission denied", s.SkippedChecks["Statements"])
	require.Equal(t, "not collected", s.SkippedChecks["Tables"])
	require.Equal(t, "not collected", s.SkippedChecks["Indexes"])
	require.Equal(t, "not collected", s.SkippedChecks["Bloat"])
	require.Equal(t, HostInfo{Available: true, Source: "cgroup_v2", TotalBytes: 1000, CgroupLimitBytes: 1000}, s.Host)
	require.Len(t, s.Tables, 1)
	require.Equal(t, "public.orders", s.Tables[0].Name)
	require.Equal(t, "app", s.Tables[0].Datname)
	require.Len(t, s.Indexes, 1)
	require.Equal(t, "CREATE INDEX orders_customer_idx ON public.orders(customer_id)", s.Indexes[0].IndexDef, "the definition of the index's own database")
	require.Equal(t, "hash-new", s.Indexes[0].DefHash)
	require.Equal(t, 0.25, s.Indexes[0].IdxScan)
	require.Len(t, s.Bloat, 2)
	require.Equal(t, "public.orders_customer_idx", s.Bloat[1].Name)
	require.Equal(t, "index", s.Bloat[1].Kind)
	require.Equal(t, "pgstattuple", s.Bloat[1].Method)
	require.Equal(t, StatementStat{Datname: "app", QueryID: 7, Calls: 120, TotalExecTimeMs: 600, MeanExecTimeMs: 5, TempBytes: 10 * 8192, SharedBlksRead: 3, SharedBlksHit: 5}, s.Statements[0])
	require.Equal(t, 4.5, s.Baseline.MeanExecTimeByQueryID[7])
	require.Len(t, s.Siblings, 2)
	require.True(t, s.Siblings[0].InstanceID.String() < s.Siblings[1].InstanceID.String(), "siblings are ordered")
	var sibling SiblingInfo
	for _, candidate := range s.Siblings {
		if candidate.InstanceID == siblingID {
			sibling = candidate
			break
		}
	}
	require.Equal(t, "8192", sibling.Settings["work_mem"])
	require.NotContains(t, sibling.Settings, "stale_setting")
	require.Equal(t, "user", sibling.SettingContexts["work_mem"])
	require.Equal(t, "hash-sibling", sibling.IndexHashes[indexHashKey("app", "public.orders_customer_idx")])
	require.Equal(t, &siblingSeen, sibling.BasebackupSeenAt)
	// A scan in the sibling's metrics history and a last scan label both count;
	// an index reported with an empty label and no scan does not.
	require.True(t, sibling.IndexScanned[indexHashKey("app", "public.orders_metrics_idx")])
	require.True(t, sibling.IndexScanned[indexHashKey("app", "public.orders_label_idx")])
	require.NotContains(t, sibling.IndexScanned, indexHashKey("app", "public.orders_customer_idx"))
	require.Equal(t, &ReplicationInfo{Standbys: 2, MaxLagBytes: 1024, ActivePhysicalSlots: 1}, s.Replication)
	require.NoError(t, mock.ExpectationsWereMet())
}

func snapshotTimePtr(value time.Time) *time.Time { return &value }

func TestLoadSnapshot_NilPoolReturnsInitializedSnapshot(t *testing.T) {
	id := uuid.New()
	now := time.Unix(100, 0).UTC()
	s, err := LoadSnapshot(context.Background(), nil, id, now)
	require.NoError(t, err)
	require.Equal(t, id, s.InstanceID)
	require.Equal(t, now, s.Now)
	require.Equal(t, "T0", s.PermTier.String())
	require.Empty(t, s.Metrics)
}

func TestSnapshotHelpers(t *testing.T) {
	require.Equal(t, "cgroup_v1", hostSourceName(1))
	require.Equal(t, "cgroup_v2", hostSourceName(2))
	require.Equal(t, "host", hostSourceName(99))
	require.True(t, isOptionalSnapshotError(&pgconn.PgError{Code: "42P01"}))
	require.True(t, isOptionalSnapshotError(&pgconn.PgError{Code: "42703"}))
	require.False(t, isOptionalSnapshotError(&pgconn.PgError{Code: "42501"}))
	require.False(t, isOptionalSnapshotError(errors.New("ordinary database error")))
}
