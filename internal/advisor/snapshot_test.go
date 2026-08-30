package advisor

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
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
	instanceID := uuid.New()
	siblingID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT cluster_id, role FROM instances")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"cluster_id", "role"}).AddRow(int64(42), "primary"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_version, perm_tier FROM instances")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"pg_version", "perm_tier"}).AddRow(170004, "T2"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (metric, labels)")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"metric", "labels", "value"}).
			AddRow("host_mem_total_bytes", []byte(`{}`), 1000.0).
			AddRow("host_mem_available_bytes", []byte(`{}`), 400.0).
			AddRow("host_metrics_source", []byte(`{}`), 2.0).
			AddRow("pg_setting_bytes", []byte(`{"name":"work_mem"}`), 4096.0).
			AddRow("pg_setting_seconds", []byte(`{"name":"statement_timeout"}`), 30.0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT kind, key, labels")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"kind", "key", "labels", "value_text", "last_seen"}).
			AddRow("setting", "max_connections", []byte(`{}`), "100", now).
			AddRow("check_skip", "stat_statements", []byte(`{}`), "permission denied", now).
			AddRow("check_skip", "table_stats", []byte(`{}`), "not collected", now).
			AddRow("check_skip", "index_stats", []byte(`{}`), "not collected", now).
			AddRow("check_skip", "bloat_estimate", []byte(`{}`), "not collected", now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT schemaname, relname")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"schemaname", "relname", "n_live_tup", "n_dead_tup", "n_mod_since_analyze", "last_autovacuum", "last_vacuum", "relfrozenxid_age", "seq_scan", "idx_scan"}).
			AddRow("public", "orders", 100.0, 4.0, 2.0, nil, nil, 10.0, 3.0, 1.0))
	mock.ExpectQuery(regexp.QuoteMeta("WITH latest AS")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "schemaname", "relname", "indexrelname", "idx_scan", "index_bytes", "is_primary", "is_unique", "is_valid", "def_hash", "history_days"}).
			AddRow("app", "public", "orders", "orders_customer_idx", 0.0, 2048.0, false, true, true, "hash-old", 2.5))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT key, COALESCE(value_text")).WithArgs(instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"key", "value_text", "def_hash"}).
			AddRow("public.orders_customer_idx", "CREATE INDEX orders_customer_idx ON public.orders(customer_id)", "hash-new"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (datname, schemaname, relname, indexrelname, object_kind)")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "schemaname", "relname", "indexrelname", "object_kind", "real_bytes", "bloat_ratio", "method"}).
			AddRow("app", "public", "orders", "", "table", 4096.0, 0.25, "estimate").
			AddRow("app", "public", "orders", "orders_customer_idx", "index", 2048.0, 0.1, "pgstattuple"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (datname, queryid)")).WithArgs(instanceID, now.Add(-15*time.Minute)).
		WillReturnRows(pgxmock.NewRows([]string{"datname", "queryid", "calls_rate", "exec_time_rate_ms", "shared_blks_read_rate", "shared_blks_hit_rate"}).
			AddRow("app", int64(7), 2.0, 10.0, 3.0, 5.0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT queryid,")).WithArgs(instanceID, now.Add(-8*24*time.Hour), now.Add(-time.Hour)).
		WillReturnRows(pgxmock.NewRows([]string{"queryid", "mean"}).AddRow(int64(7), 4.5))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT i.instance_id, i.role, f.kind, f.key")).WithArgs(int64(42), instanceID).
		WillReturnRows(pgxmock.NewRows([]string{"instance_id", "role", "kind", "key", "value_text", "def_hash"}).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("setting"), snapshotStringPtr("work_mem"), snapshotStringPtr("8192"), snapshotStringPtr("")).
			AddRow(siblingID, snapshotStringPtr("standby"), snapshotStringPtr("index_def"), snapshotStringPtr("public.orders_customer_idx"), snapshotStringPtr(""), snapshotStringPtr("hash-sibling")).
			AddRow(uuid.New(), snapshotStringPtr("standby"), nil, nil, nil, nil))

	s, err := LoadSnapshot(ctx, mock, instanceID, now)
	require.NoError(t, err)
	require.Equal(t, int64(42), s.ClusterID)
	require.Equal(t, instanceID, s.InstanceID)
	require.Equal(t, "primary", string(s.Role))
	require.Equal(t, 170004, int(s.PGVersion))
	require.Equal(t, "T2", s.PermTier.String())
	require.Equal(t, "100", s.Settings["max_connections"])
	require.Equal(t, "4096", s.Settings["work_mem_bytes"])
	require.Equal(t, "30", s.Settings["statement_timeout_seconds"])
	require.Equal(t, "permission denied", s.SkippedChecks["Statements"])
	require.Equal(t, "not collected", s.SkippedChecks["Tables"])
	require.Equal(t, "not collected", s.SkippedChecks["Indexes"])
	require.Equal(t, "not collected", s.SkippedChecks["Bloat"])
	require.Equal(t, HostInfo{Available: true, Source: "cgroup_v2", TotalBytes: 1000, CgroupLimitBytes: 1000}, s.Host)
	require.Len(t, s.Tables, 1)
	require.Equal(t, "public.orders", s.Tables[0].Name)
	require.Len(t, s.Indexes, 1)
	require.Equal(t, "CREATE INDEX orders_customer_idx ON public.orders(customer_id)", s.Indexes[0].IndexDef)
	require.Equal(t, "hash-new", s.Indexes[0].DefHash)
	require.Len(t, s.Bloat, 2)
	require.Equal(t, "public.orders_customer_idx", s.Bloat[1].Name)
	require.Equal(t, 5.0, s.Statements[0].MeanExecTimeMs)
	require.Equal(t, 4.5, s.Baseline.MeanExecTimeByQueryID[7])
	require.Len(t, s.Siblings, 2)
	var sibling SiblingInfo
	for _, candidate := range s.Siblings {
		if candidate.InstanceID == siblingID {
			sibling = candidate
			break
		}
	}
	require.Equal(t, "8192", sibling.Settings["work_mem"])
	require.Equal(t, "hash-sibling", sibling.IndexHashes["public.orders_customer_idx"])
	require.NoError(t, mock.ExpectationsWereMet())
}

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
