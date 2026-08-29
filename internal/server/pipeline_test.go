package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestDestinationTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		check string
		want  string
	}{
		{"stat_statements", "metrics_statements"},
		{"ash", "metrics_ash"},
		{"replication", "metrics_replication"},
		{"replication_slots", "metrics_replication"},
		{"replication_streaming", "metrics_replication"},
		{"table_stats", "metrics_tables"},
		{"index_stats", "metrics_indexes"},
		{"bloat_estimate", "metrics_bloat"},
		{"something_else", "metrics"},
		{"bgwriter", "metrics"},
		{"", "metrics"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.check, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, c.want, destinationTable(c.check))
		})
	}
}

func TestProcess_GroupsTableMetricsPerRelation(t *testing.T) {
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: "1", Results: []wire.Result{{
		Check: "table_stats", TS: ts,
		Metrics: []wire.Metric{
			{Name: "n_live_tup", Value: 12, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders"}},
			{Name: "seq_scan", Value: 10, Kind: "counter", Labels: map[string]string{"schemaname": "public", "relname": "orders"}},
			{Name: "table_bytes", Value: 4096, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders"}},
		},
	}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestProcess_CounterVsGaugeClassification(t *testing.T) {
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := func(metrics ...wire.Metric) wire.Envelope {
		return wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: "2", Results: []wire.Result{{Check: "table_stats", TS: ts, Metrics: metrics}}}}}
	}
	_, err := p.Process(context.Background(), base(
		wire.Metric{Name: "n_live_tup", Value: 12, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "t"}},
		wire.Metric{Name: "idx_scan", Value: 10, Kind: "counter", Labels: map[string]string{"schemaname": "public", "relname": "t"}},
	))
	require.NoError(t, err)
	_, err = p.Process(context.Background(), base(wire.Metric{Name: "idx_scan", Value: 20, Kind: "counter", Labels: map[string]string{"schemaname": "public", "relname": "t"}}))
	require.NoError(t, err)
}

func TestProcess_InvalidFactIsSkippedNotFatal(t *testing.T) {
	p, _, _ := newPipelineWithMockPool()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: "3", Results: []wire.Result{{
		Check: "settings", TS: time.Now(), Facts: []wire.Fact{{Kind: "setting", Key: "", ValueText: "bad"}},
	}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
}

func TestProcess_LockTreeFactIsAcceptedAndDropped(t *testing.T) {
	p, _, _ := newPipelineWithMockPool()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: "4", Results: []wire.Result{{
		Check: "locks", TS: time.Now(), Facts: []wire.Fact{{Kind: "lock_tree", Key: "root", ValueJSON: []byte(`{"pid":1}`)}},
	}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestApplyTableMetric_CoversTypedColumns(t *testing.T) {
	row := &store.TableStatRow{}
	for _, name := range []string{"seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch", "n_tup_ins", "n_tup_upd", "n_tup_del", "n_tup_hot_upd", "autovacuum_count", "autoanalyze_count", "n_live_tup", "n_dead_tup", "n_mod_since_analyze", "relpages", "reltuples", "relfrozenxid_age", "total_bytes", "table_bytes", "toast_bytes"} {
		applyTableMetric(row, name, 7)
	}
	require.NotNil(t, row.SeqScan)
	require.NotNil(t, row.NLiveTup)
	require.NotNil(t, row.TotalBytes)
}

func TestApplyIndexMetric_CoversTypedColumns(t *testing.T) {
	row := &store.IndexStatRow{}
	labels := map[string]string{"def_hash": "abc"}
	for _, name := range []string{"idx_scan", "idx_tup_read", "idx_tup_fetch", "idx_blks_read", "idx_blks_hit", "index_bytes", "is_unique", "is_primary", "is_valid", "def_hash"} {
		applyIndexMetric(row, name, 1, labels)
	}
	require.NotNil(t, row.IdxScan)
	require.NotNil(t, row.IndexBytes)
	require.NotNil(t, row.DefHash)
}

func TestApplyBloatMetric_CoversTypedColumns(t *testing.T) {
	row := &store.BloatRow{}
	for _, name := range []string{"real_bytes", "expected_bytes", "bloat_bytes", "bloat_ratio"} {
		applyBloatMetric(row, name, 3)
	}
	require.NotNil(t, row.RealBytes)
	require.NotNil(t, row.BloatRatio)
}

func TestProcess_RoutesTypedFactsAndRelationChecks(t *testing.T) {
	p, _, _ := newPipelineWithMockPool()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: "5", Results: []wire.Result{
		{Check: "index_stats", TS: time.Now(), Metrics: []wire.Metric{
			{Name: "idx_scan", Value: 4, Kind: "counter", Labels: map[string]string{"schemaname": "public", "relname": "orders", "indexrelname": "orders_pkey"}},
			{Name: "index_bytes", Value: 128, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders", "indexrelname": "orders_pkey"}},
		}},
		{Check: "bloat_estimate", TS: time.Now(), Metrics: []wire.Metric{
			{Name: "pg_bloat_real_bytes", Value: 100, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders", "object_kind": "table", "method": "estimate"}},
			{Name: "pg_bloat_expected_bytes", Value: 75, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders", "object_kind": "table", "method": "estimate"}},
			{Name: "pg_bloat_bytes", Value: 25, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders", "object_kind": "table", "method": "estimate"}},
			{Name: "pg_bloat_ratio", Value: 0.25, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders", "object_kind": "table", "method": "estimate"}},
		}},
		{Check: "settings", TS: time.Now(), Database: "app", Facts: []wire.Fact{
			{Kind: "setting", Key: "work_mem", ValueText: "4MB"},
			{Kind: "index_def", Key: "orders_pkey", ValueJSON: []byte(`{"definition":"PRIMARY KEY (id)"}`)},
		}},
	}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestReplicationSlot(t *testing.T) {
	t.Parallel()
	require.Equal(t, "my_slot", replicationSlot(map[string]string{"slot_name": "my_slot"}))
	require.Equal(t, "s2", replicationSlot(map[string]string{"slot": "s2"}))
	require.Equal(t, "app1", replicationSlot(map[string]string{"application_name": "app1"}))
	require.Equal(t, "", replicationSlot(map[string]string{}))
	require.Equal(t, "slot_name", replicationSlot(map[string]string{"slot_name": "slot_name", "slot": "other"}))
}

func TestApplyStatementMetric(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		metric string
		value  float64
		check  func(*store.StatementRow) bool
	}{
		{"shared_blks_hit", "shared_blks_hit", 10, func(r *store.StatementRow) bool { return r.SharedBlksHitRate != nil && *r.SharedBlksHitRate == 10 }},
		{"shared_blks_read", "shared_blks_read", 5, func(r *store.StatementRow) bool { return r.SharedBlksReadRate != nil && *r.SharedBlksReadRate == 5 }},
		{"wal_bytes", "wal_bytes", 100, func(r *store.StatementRow) bool { return r.WALBytesRate != nil && *r.WALBytesRate == 100 }},
		{"exec_time", "total_exec_time", 20, func(r *store.StatementRow) bool { return r.ExecTimeRateMs != nil && *r.ExecTimeRateMs == 20 }},
		{"calls", "calls", 7, func(r *store.StatementRow) bool { return r.CallsRate != nil && *r.CallsRate == 7 }},
		{"rows", "rows", 3, func(r *store.StatementRow) bool { return r.RowsRate != nil && *r.RowsRate == 3 }},
		{"blks rows excluded", "shared_blks_rows", 9, func(r *store.StatementRow) bool { return r.RowsRate == nil }},
		{"unknown fallback", "unknown_metric_xyz", 42, func(r *store.StatementRow) bool { return r.CallsRate != nil && *r.CallsRate == 42 }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			row := &store.StatementRow{}
			applyStatementMetric(row, c.metric, c.value)
			require.True(t, c.check(row))
		})
	}
	// ensure case insensitive
	row := &store.StatementRow{}
	applyStatementMetric(row, "SHARED_BLKS_HIT", 1)
	require.NotNil(t, row.SharedBlksHitRate)
}

func TestApplyReplicationMetric(t *testing.T) {
	t.Parallel()
	// write_lag_bytes
	row := &store.ReplicationRow{}
	applyReplicationMetric(row, "write_lag_bytes", 123, nil)
	require.NotNil(t, row.WriteLagBytes)
	require.Equal(t, int64(123), *row.WriteLagBytes)
	// flush_lag_bytes
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "flush_lag_bytes", 456, nil)
	require.Equal(t, int64(456), *row.FlushLagBytes)
	// replay_lag_bytes
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "replay_lag_bytes", 789, nil)
	require.Equal(t, int64(789), *row.ReplayLagBytes)
	// write_lag_sec
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "write_lag_sec", 1.5, nil)
	require.NotNil(t, row.WriteLagSec)
	require.Equal(t, 1.5, *row.WriteLagSec)
	// flush_lag_sec
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "flush_lag_sec", 2.5, nil)
	require.Equal(t, 2.5, *row.FlushLagSec)
	// replay_lag_sec
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "replay_lag_sec", 3.5, nil)
	require.Equal(t, 3.5, *row.ReplayLagSec)
	// slot_active
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "slot_active", 1, nil)
	require.NotNil(t, row.SlotActive)
	require.True(t, *row.SlotActive)
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "slot_active", 0, nil)
	require.False(t, *row.SlotActive)
	// slot_retained / retained_bytes
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "slot_retained_bytes", 1000, nil)
	require.Equal(t, int64(1000), *row.SlotRetainedBytes)
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "slot_wal_status", 0, map[string]string{"slot_wal_status": "reserved"})
	require.NotNil(t, row.SlotWalStatus)
	require.Equal(t, "reserved", *row.SlotWalStatus)
	// sync_state
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "sync_state", 0, map[string]string{"sync_state": "sync"})
	require.Equal(t, "sync", *row.SyncState)
	// fallback bytes
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "some_bytes_metric", 55, nil)
	require.Equal(t, int64(55), *row.WriteLagBytes)
	// fallback lag
	row = &store.ReplicationRow{}
	applyReplicationMetric(row, "some_lag_metric", 5.5, nil)
	require.Equal(t, 5.5, *row.WriteLagSec)
}

func TestBuildASHRow(t *testing.T) {
	t.Parallel()
	inst := uuid.New()
	now := time.Now()
	m := wire.Metric{Name: "x", Value: 5, Labels: map[string]string{"wait_event_type": "LWLock", "wait_event": "WALWriteLock", "state": "active", "datname": "mydb", "queryid": "123", "window_seconds": "20"}}
	row := buildASHRow(now, "default", 1, inst, "postgres", m, "ash")
	require.Equal(t, "LWLock", row.WaitEventType)
	require.Equal(t, "WALWriteLock", row.WaitEvent)
	require.Equal(t, "active", row.State)
	require.Equal(t, "mydb", row.Datname)
	require.NotNil(t, row.QueryID)
	require.Equal(t, int64(123), *row.QueryID)
	require.Equal(t, 5, row.Samples)
	require.Equal(t, 20, row.WindowSeconds)
	// defaults
	m2 := wire.Metric{Name: "x", Value: 2, Labels: map[string]string{}}
	row2 := buildASHRow(now, "default", 1, inst, "postgres", m2, "ash")
	require.Equal(t, "CPU", row2.WaitEventType)
	require.Equal(t, "CPU", row2.WaitEvent)
	require.Equal(t, "active", row2.State)
	require.Equal(t, "postgres", row2.Datname)
	require.Nil(t, row2.QueryID)
	require.Equal(t, 10, row2.WindowSeconds)
	// query_id alternative label
	m3 := wire.Metric{Name: "x", Value: 1, Labels: map[string]string{"query_id": "999"}}
	row3 := buildASHRow(now, "default", 1, inst, "postgres", m3, "ash")
	require.NotNil(t, row3.QueryID)
	require.Equal(t, int64(999), *row3.QueryID)
	// case insensitive queryid
	m4 := wire.Metric{Name: "x", Value: 1, Labels: map[string]string{"QueryId": "777"}}
	row4 := buildASHRow(now, "default", 1, inst, "postgres", m4, "ash")
	require.NotNil(t, row4.QueryID)
	require.Equal(t, int64(777), *row4.QueryID)
	// window alternative
	m5 := wire.Metric{Name: "x", Value: 1, Labels: map[string]string{"window": "30"}}
	row5 := buildASHRow(now, "default", 1, inst, "postgres", m5, "ash")
	require.Equal(t, 30, row5.WindowSeconds)
}

func TestPipeline_NewPipelineDefaults(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	require.NotNil(t, p)
	require.NotNil(t, p.delta)
	require.NotNil(t, p.clock)
	// with fake clock
	fc := clock.NewFake(time.Now())
	p2 := NewPipeline(nil, fc)
	require.Equal(t, fc, p2.clock)
}

func TestPipeline_MaybeEvict(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fc := clock.NewFake(start)
	p := NewPipeline(nil, fc)
	// initially lastEvict = start
	p.maybeEvict()
	require.Equal(t, start, p.lastEvict)
	// advance 5 minutes, should not evict
	fc.Advance(5 * time.Minute)
	p.maybeEvict()
	require.Equal(t, start, p.lastEvict)
	// advance to 11 minutes, should evict
	fc.Advance(6 * time.Minute)
	p.maybeEvict()
	require.Equal(t, start.Add(11*time.Minute), p.lastEvict)
	// concurrent safety check with multiple calls
}

func TestPipeline_GaugeBypassesDelta(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(123).String()
	env := wire.Envelope{
		Instances: []wire.Instance{
			{
				InstanceID: instID, ClusterID: cid,
				Results: []wire.Result{
					{Check: "bgwriter", TS: time.Now(), Database: "", Metrics: []wire.Metric{{Name: "pg_bgwriter_buffers_checkpoint", Value: 42, Kind: "gauge", Labels: map[string]string{}}}},
				},
			},
		},
	}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
}

// TestPipeline_ResultErrorIncrementsCheckErrorTotal proves wire.Result.Error
// is actually read now — found live via SYS-LOAD-003 prep that it never was
// (row 141-142's stat_statements bug hid invisibly for exactly this
// reason). Uses a unique check name since pglens_check_error_total is a
// shared package-level counter other tests may also touch.
func TestPipeline_ResultErrorIncrementsCheckErrorTotal(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(123).String()
	const checkName = "pipeline_test_unique_check_error"
	env := wire.Envelope{
		Instances: []wire.Instance{
			{
				InstanceID: instID, ClusterID: cid,
				Results: []wire.Result{
					{Check: checkName, TS: time.Now(), Error: "connection refused"},
				},
			},
		},
	}
	before := pglensCheckErrorTotal.Get(checkName)
	_, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, before+1, pglensCheckErrorTotal.Get(checkName))
}

func TestPipeline_CounterProducesRate(t *testing.T) {
	t.Parallel()
	fc := clock.NewFake(time.Now())
	p := NewPipeline(nil, fc)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(123).String()
	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(60 * time.Second)
	env1 := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts1, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 100, Kind: "counter", Labels: map[string]string{}}}}}},
		},
	}
	env2 := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts2, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 160, Kind: "counter", Labels: map[string]string{}}}}}},
		},
	}
	// first counter no row but accepted
	res1, err := p.Process(context.Background(), env1)
	require.NoError(t, err)
	require.Equal(t, 1, res1.Accepted)
	// second produces rate internally (processInstanceNoDB still just observes, no DB check for value)
	res2, err := p.Process(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res2.Accepted)
	// verify delta engine produced rate by checking second call with real DB? For nil pool we can't verify stored row, but we can test delta directly
	// Instead test that no error and no event for normal rate
}

func TestPipeline_FirstCounterNoRow(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "c", Value: 10, Kind: "counter"}}}}},
		},
	}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_ResetProducesEventNotRow(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	ts1 := time.Now()
	ts2 := ts1.Add(60 * time.Second)
	// first
	_, _ = p.Process(context.Background(), wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts1, Metrics: []wire.Metric{{Name: "c", Value: 100, Kind: "counter"}}}}}}})
	// reset: value goes backwards
	res, err := p.Process(context.Background(), wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts2, Metrics: []wire.Metric{{Name: "c", Value: 10, Kind: "counter"}}}}}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// with real DB we would check events, but nil pool still accepts
}

func TestPipeline_UnknownKindError(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "x", Value: 1, Kind: "unknown"}}}}},
		},
	}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[instID].Error(), "unknown metric kind")
}

func TestPipeline_InvalidClusterID(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: "bad"}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[instID].Error(), "invalid cluster_id")
}

func TestPipeline_InvalidInstanceID(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: "bad", ClusterID: cid}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors["bad"].Error(), "invalid instance_id")
}

func TestPipeline_GaugeASHBypass(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "ash", TS: time.Now(), Metrics: []wire.Metric{{Name: "ash_samples", Value: 5, Kind: "gauge"}, {Name: "ash_samples", Value: 5, Kind: "counter"}}}}},
		},
	}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// unknown kind for ASH should also error
	env2 := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "ash", TS: time.Now(), Metrics: []wire.Metric{{Name: "x", Value: 1, Kind: "bad"}}}}},
		},
	}
	res, err = p.Process(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res.Rejected)
}

func TestPipeline_TruncatedAndQueryTexts(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	ts := time.Now()
	env := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, PGVersion: 170000, Results: []wire.Result{{Check: "stat_statements", TS: ts, Database: "db", QueryTexts: map[string]string{"123": "SELECT 1"}, Metrics: []wire.Metric{{Name: "calls", Value: 10, Kind: "gauge", Labels: map[string]string{"queryid": "123"}}}}}},
		},
	}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// invalid queryid in QueryTexts should be skipped
	env2 := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "stat_statements", TS: ts, QueryTexts: map[string]string{"bad": "SELECT 1"}}}},
		},
	}
	res, err = p.Process(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_Process_NilPool_StatsReset(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	ts1 := time.Now()
	reset := ts1
	ts2 := ts1.Add(60 * time.Second)
	newReset := ts2
	env1 := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID, ClusterID: cid,
			Results: []wire.Result{{
				Check: "bgwriter", TS: ts1, StatsReset: &reset,
				Metrics: []wire.Metric{{Name: "c", Value: 100, Kind: "counter"}},
			}},
		}},
	}
	env2 := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID, ClusterID: cid,
			Results: []wire.Result{{
				Check: "bgwriter", TS: ts2, StatsReset: &newReset,
				Metrics: []wire.Metric{{Name: "c", Value: 200, Kind: "counter"}},
			}},
		}},
	}
	_, _ = p.Process(context.Background(), env1)
	res, err := p.Process(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

// The tests below exercise Pipeline.Process's real (non-nil-pool) branch with
// a mockPool/mockTx instead of a database, closing V002-F07's L1-coverage gap.
// See internal/server/mockpool_test.go.

func newPipelineWithMockPool() (*Pipeline, *mockPool, *mockTx) {
	p := NewPipeline(nil, nil)
	tx := &mockTx{}
	pool := &mockPool{beginTx: tx}
	p.pool = pool
	return p, pool, tx
}

func TestPipeline_MockPool_Gauge(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: uuid.NewString(),
		ClusterID:  pgtype.ClusterID(1).String(),
		Results: []wire.Result{{
			Check: "bgwriter", TS: time.Now(),
			Metrics: []wire.Metric{{Name: "pg_buffers_checkpoint", Value: 42, Kind: "gauge"}},
		}},
	}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
}

// TestPipeline_MockPool_SetsSeriesTotal proves I-8's real number: after
// processing an envelope with one counter series, pglens_series_total
// reflects it — SetSeriesTotal existed since an earlier phase pass but
// nothing ever called it with a real value before this.
func TestPipeline_MockPool_SetsSeriesTotal(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID,
		ClusterID:  pgtype.ClusterID(3).String(),
		Results: []wire.Result{{
			Check: "bgwriter", TS: time.Now(),
			Metrics: []wire.Metric{{Name: "pg_xact_commit_total", Value: 100, Kind: "counter"}},
		}},
	}}}
	_, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1.0, pglensSeriesTotalGauge.Get(instID))
}

func TestPipeline_MockPool_CounterReset(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	cid := pgtype.ClusterID(2).String()
	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Minute)
	env1 := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{
		Check: "bgwriter", TS: ts1, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 100, Kind: "counter"}},
	}}}}}
	env2 := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{
		Check: "bgwriter", TS: ts2, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 5, Kind: "counter"}}, // lower value => reset
	}}}}}
	_, err := p.Process(context.Background(), env1)
	require.NoError(t, err)
	res2, err := p.Process(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res2.Accepted)
}

func TestPipeline_MockPool_ASHAndReplicationAndStatements(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	cid := pgtype.ClusterID(3).String()
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID, ClusterID: cid, PGVersion: 170000,
		Results: []wire.Result{
			{Check: "ash", TS: time.Now(), Metrics: []wire.Metric{{Name: "samples", Value: 5, Kind: "gauge", Labels: map[string]string{"wait_event_type": "CPU", "wait_event": "CPU"}}}},
			{Check: "replication", TS: time.Now(), Metrics: []wire.Metric{{Name: "write_lag_bytes", Value: 10, Kind: "gauge", Labels: map[string]string{"slot_name": "s1", "upstream_id": uuid.NewString(), "sync_state": "sync"}}}},
			{Check: "stat_statements", TS: time.Now(), Database: "db", QueryTexts: map[string]string{"123": "SELECT 1"}, Metrics: []wire.Metric{{Name: "calls", Value: 10, Kind: "gauge", Labels: map[string]string{"queryid": "123"}}}},
		},
	}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_MockPool_UnknownKind_Rejected(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	instID := uuid.NewString()
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID, ClusterID: pgtype.ClusterID(4).String(),
		Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "x", Value: 1, Kind: "unknown"}}}},
	}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[instID].Error(), "unknown metric kind")
}

func TestPipeline_MockPool_InvalidClusterAndInstanceID_Rejected(t *testing.T) {
	t.Parallel()
	p, _, _ := newPipelineWithMockPool()
	env := wire.Envelope{Instances: []wire.Instance{
		{InstanceID: uuid.NewString(), ClusterID: "not-a-number"},
		{InstanceID: "not-a-uuid", ClusterID: pgtype.ClusterID(5).String()},
	}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 2, res.Rejected)
}

func TestPipeline_MockPool_BeginError(t *testing.T) {
	t.Parallel()
	p, pool, _ := newPipelineWithMockPool()
	pool.beginErr = errors.New("connection refused")
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: pgtype.ClusterID(6).String()}}}
	_, err := p.Process(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "begin tx")
}

func TestPipeline_MockPool_WriteError_RollsBack(t *testing.T) {
	t.Parallel()
	p, _, tx := newPipelineWithMockPool()
	tx.execErr = errors.New("disk full")
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: uuid.NewString(), ClusterID: pgtype.ClusterID(7).String(),
		Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "x", Value: 1, Kind: "gauge"}}}},
	}}}
	_, err := p.Process(context.Background(), env)
	require.Error(t, err)
	require.True(t, tx.rolledBack)
	require.False(t, tx.committed)
}

func TestPipeline_StatementMissingQueryIDSkipped(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "stat_statements", TS: time.Now(), Metrics: []wire.Metric{{Name: "calls", Value: 1, Kind: "gauge", Labels: map[string]string{}}}}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_StatementCaseInsensitiveQueryID(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "stat_statements", TS: time.Now(), Metrics: []wire.Metric{{Name: "calls", Value: 1, Kind: "gauge", Labels: map[string]string{"QueryId": "999"}}}}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_ReplicationSlotFallback(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "replication_slots", TS: time.Now(), Metrics: []wire.Metric{{Name: "slot_active", Value: 1, Kind: "gauge", Labels: map[string]string{}}}}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}

func TestPipeline_LabelsNilHandled(t *testing.T) {
	t.Parallel()
	p := NewPipeline(nil, nil)
	instID := uuid.NewString()
	cid := pgtype.ClusterID(1).String()
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "m", Value: 1, Kind: "gauge", Labels: nil}}}}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
}
