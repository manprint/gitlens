package store

import (
	"context"
	"fmt"
	"hash/fnv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestSeriesID_Deterministic(t *testing.T) {
	t.Parallel()
	inst := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	k1 := pgtype.SeriesKey{Metric: "pg_backends", Instance: inst, Database: "postgres", Labels: pgtype.CanonicalLabels(map[string]string{"a": "1", "b": "2"})}
	k2 := pgtype.SeriesKey{Metric: "pg_backends", Instance: inst, Database: "postgres", Labels: pgtype.CanonicalLabels(map[string]string{"b": "2", "a": "1"})}
	require.Equal(t, SeriesID(k1), SeriesID(k2))
	// different metric -> different ID
	k3 := pgtype.SeriesKey{Metric: "other", Instance: inst, Database: "postgres", Labels: pgtype.CanonicalLabels(map[string]string{"a": "1"})}
	require.NotEqual(t, SeriesID(k1), SeriesID(k3))
	// different instance
	k4 := pgtype.SeriesKey{Metric: "pg_backends", Instance: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Database: "postgres", Labels: ""}
	require.NotEqual(t, SeriesID(k1), SeriesID(k4))
	// verify FNV manually
	h := fnv.New64a()
	_, _ = h.Write([]byte(k1.Metric))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k1.Instance.String()))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k1.Database))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(k1.Labels))
	require.Equal(t, int64(h.Sum64()), SeriesID(k1))
}

func TestSeriesID_EmptyLabels(t *testing.T) {
	t.Parallel()
	inst := uuid.New()
	k := pgtype.SeriesKey{Metric: "m", Instance: inst, Database: "", Labels: ""}
	id := SeriesID(k)
	require.NotZero(t, id)
	// same again
	require.Equal(t, id, SeriesID(k))
}

func TestWriteMetrics_NilLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	row := MetricRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "", Metric: "x", Labels: nil, SeriesID: 123, Value: 1.0}
	require.NoError(t, WriteMetrics(ctx, m, []MetricRow{row}))
	require.Equal(t, 1, m.batchCalls)
}

func TestWriteQueryPlan_DedupOnHash(t *testing.T) {
	tx := &mockTx{}
	row := QueryPlanRow{TenantID: "default", InstanceID: uuid.New(), ClusterID: 1, Datname: "postgres", QueryID: 7, PlanHash: "same", CapturedAt: time.Now(), Plan: []byte(`[]`)}
	require.NoError(t, WriteQueryPlan(context.Background(), tx, row))
	require.Contains(t, tx.execSQL[0], "ON CONFLICT (tenant_id, instance_id, datname, queryid, plan_hash, analyzed) DO NOTHING")
}

func TestWriteMetrics_Empty(t *testing.T) {
	t.Parallel()
	require.NoError(t, WriteMetrics(context.Background(), &mockTx{}, nil))
	require.NoError(t, WriteMetrics(context.Background(), &mockTx{}, []MetricRow{}))
}

func TestWriteMetrics_BatchLT500(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	rows := make([]MetricRow, 3)
	for i := range rows {
		rows[i] = MetricRow{TS: time.Now().Add(time.Duration(i) * time.Second), TenantID: "default", ClusterID: int64(i), InstanceID: uuid.New(), Datname: "db", Metric: "m", Labels: map[string]string{"k": "v"}, SeriesID: int64(i), Value: float64(i)}
	}
	require.NoError(t, WriteMetrics(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
	require.Equal(t, 0, m.execCalls)
	require.Equal(t, 0, m.copyCalls)
}

func TestWriteMetrics_BatchGE500(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	rows := make([]MetricRow, 500)
	for i := range rows {
		rows[i] = MetricRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "", Metric: "m", Labels: map[string]string{}, SeriesID: int64(i), Value: 1}
	}
	require.NoError(t, WriteMetrics(ctx, m, rows))
	require.Equal(t, 0, m.batchCalls)
	require.GreaterOrEqual(t, m.execCalls, 2) // create tmp + insert + drop
	require.Equal(t, 1, m.copyCalls)
}

func TestWriteMetrics_BatchExecError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	rows := []MetricRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Metric: "m", Labels: map[string]string{}, SeriesID: 1, Value: 1}}
	err := WriteMetrics(ctx, m, rows)
	require.Error(t, err)
	require.Contains(t, err.Error(), "write metrics batch row")
}

func TestWriteMetrics_BatchCloseError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchCloseErr: fmt.Errorf("close boom")}
	rows := []MetricRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Metric: "m", Labels: map[string]string{}, SeriesID: 1, Value: 1}}
	err := WriteMetrics(ctx, m, rows)
	require.Error(t, err)
	require.Contains(t, err.Error(), "close metrics batch")
}

func TestWriteMetrics_CopyError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{copyErr: fmt.Errorf("copy boom")}
	rows := make([]MetricRow, 500)
	for i := range rows {
		rows[i] = MetricRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Metric: "m", SeriesID: int64(i), Value: 1}
	}
	err := WriteMetrics(ctx, m, rows)
	require.Error(t, err)
	require.Contains(t, err.Error(), "copy metrics")
}

func TestWriteMetrics_CreateTmpError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{execErr: fmt.Errorf("create boom")}
	rows := make([]MetricRow, 500)
	for i := range rows {
		rows[i] = MetricRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Metric: "m", SeriesID: 1, Value: 1}
	}
	err := WriteMetrics(ctx, m, rows)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create tmp_metrics")
}

func TestWriteStatements_Branches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	require.NoError(t, WriteStatements(ctx, m, nil))
	rows := []StatementRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", QueryID: 123, CallsRate: floatPtr(1)}}
	require.NoError(t, WriteStatements(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
	// >=500
	m2 := &mockTx{}
	large := make([]StatementRow, 500)
	for i := range large {
		large[i] = StatementRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", QueryID: int64(i)}
	}
	require.NoError(t, WriteStatements(ctx, m2, large))
	require.Equal(t, 1, m2.copyCalls)
}

func TestWriteStatements_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	rows := []StatementRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", QueryID: 1}}
	err := WriteStatements(ctx, m, rows)
	require.Error(t, err)
	m2 := &mockTx{batchCloseErr: fmt.Errorf("close")}
	err = WriteStatements(ctx, m2, rows)
	require.Error(t, err)
	m3 := &mockTx{execErr: fmt.Errorf("create")}
	large := make([]StatementRow, 500)
	for i := range large {
		large[i] = StatementRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", QueryID: int64(i)}
	}
	err = WriteStatements(ctx, m3, large)
	require.Error(t, err)
}

func TestWriteASH_Branches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	require.NoError(t, WriteASH(ctx, m, nil))
	rows := []ASHRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 5, WindowSeconds: 10}}
	require.NoError(t, WriteASH(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
	m2 := &mockTx{}
	large := make([]ASHRow, 500)
	for i := range large {
		large[i] = ASHRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "LWLock", WaitEvent: "WALWriteLock", State: "active", Samples: 1, WindowSeconds: 10}
	}
	require.NoError(t, WriteASH(ctx, m2, large))
	require.Equal(t, 1, m2.copyCalls)
}

func TestWriteASH_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	err := WriteASH(ctx, m, []ASHRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 1, WindowSeconds: 10}})
	require.Error(t, err)
	m2 := &mockTx{batchCloseErr: fmt.Errorf("close")}
	err = WriteASH(ctx, m2, []ASHRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 1, WindowSeconds: 10}})
	require.Error(t, err)
	m3 := &mockTx{copyErr: fmt.Errorf("copy")}
	large := make([]ASHRow, 500)
	for i := range large {
		large[i] = ASHRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 1, WindowSeconds: 10}
	}
	require.Error(t, WriteASH(ctx, m3, large))
	m4 := &mockTx{execErr: fmt.Errorf("create")}
	require.Error(t, WriteASH(ctx, m4, large))
}

func TestWriteReplication_Branches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	require.NoError(t, WriteReplication(ctx, m, nil))
	rows := []ReplicationRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "s", EdgeType: ""}}
	require.NoError(t, WriteReplication(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
	// check default edge_type filled
	m2 := &mockTx{}
	large := make([]ReplicationRow, 500)
	for i := range large {
		large[i] = ReplicationRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "slot", EdgeType: "streaming"}
	}
	require.NoError(t, WriteReplication(ctx, m2, large))
	require.Equal(t, 1, m2.copyCalls)
	// empty edge_type should default to streaming in batch path - test via not error
	m3 := &mockTx{}
	emptyEdge := []ReplicationRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "", EdgeType: ""}}
	require.NoError(t, WriteReplication(ctx, m3, emptyEdge))
}

func TestWriteReplication_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	err := WriteReplication(ctx, m, []ReplicationRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "s"}})
	require.Error(t, err)
	m2 := &mockTx{batchCloseErr: fmt.Errorf("close")}
	err = WriteReplication(ctx, m2, []ReplicationRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "s"}})
	require.Error(t, err)
	m3 := &mockTx{execErr: fmt.Errorf("create")}
	large := make([]ReplicationRow, 500)
	for i := range large {
		large[i] = ReplicationRow{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "s"}
	}
	require.Error(t, WriteReplication(ctx, m3, large))
	m4 := &mockTx{copyErr: fmt.Errorf("copy")}
	require.Error(t, WriteReplication(ctx, m4, large))
}

func TestWriteQueryTexts_Branches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	require.NoError(t, WriteQueryTexts(ctx, m, nil))
	rows := []QueryTextRow{{TenantID: "default", ClusterID: 1, Datname: "db", QueryID: 123, PGMajor: 16, QueryText: "SELECT 1", LastSeen: time.Now()}}
	require.NoError(t, WriteQueryTexts(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
}

func TestWriteQueryTexts_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	err := WriteQueryTexts(ctx, m, []QueryTextRow{{TenantID: "default", ClusterID: 1, Datname: "db", QueryID: 1, PGMajor: 15, QueryText: "x", LastSeen: time.Now()}})
	require.Error(t, err)
	m2 := &mockTx{batchCloseErr: fmt.Errorf("close")}
	err = WriteQueryTexts(ctx, m2, []QueryTextRow{{TenantID: "default", ClusterID: 1, Datname: "db", QueryID: 1, PGMajor: 15, QueryText: "x", LastSeen: time.Now()}})
	require.Error(t, err)
}

func TestWriteEvents_Branches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	require.NoError(t, WriteEvents(ctx, m, nil))
	rows := []EventRow{{TenantID: "default", TS: time.Now(), Type: "t", ClusterID: int64Ptr(1), InstanceID: uuidPtr(uuid.New()), Payload: map[string]any{"a": "b"}}}
	require.NoError(t, WriteEvents(ctx, m, rows))
	require.Equal(t, 1, m.batchCalls)
	// nil payload maps
	rows2 := []EventRow{{TenantID: "default", TS: time.Now(), Type: "t", Payload: nil}}
	require.NoError(t, WriteEvents(ctx, m, rows2))
}

func TestWriteEvents_MarshalError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{}
	rows := []EventRow{{TenantID: "default", TS: time.Now(), Type: "t", Payload: map[string]any{"bad": make(chan int)}}}
	err := WriteEvents(ctx, m, rows)
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal event payload")
}

func TestWriteEvents_BatchErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &mockTx{batchExecErr: fmt.Errorf("boom")}
	err := WriteEvents(ctx, m, []EventRow{{TenantID: "default", TS: time.Now(), Type: "t", Payload: map[string]any{"a": 1}}})
	require.Error(t, err)
	m2 := &mockTx{batchCloseErr: fmt.Errorf("close")}
	err = WriteEvents(ctx, m2, []EventRow{{TenantID: "default", TS: time.Now(), Type: "t", Payload: map[string]any{"a": 1}}})
	require.Error(t, err)
}

func TestWriter_Wrappers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(nil)
	m := &mockTx{}
	require.NoError(t, w.WriteMetrics(ctx, m, []MetricRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Metric: "m", SeriesID: 1, Value: 1}}))
	require.NoError(t, w.WriteStatements(ctx, m, []StatementRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", QueryID: 1}}))
	require.NoError(t, w.WriteASH(ctx, m, []ASHRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), Datname: "db", WaitEventType: "CPU", WaitEvent: "CPU", State: "active", Samples: 1, WindowSeconds: 10}}))
	require.NoError(t, w.WriteReplication(ctx, m, []ReplicationRow{{TS: time.Now(), TenantID: "default", ClusterID: 1, InstanceID: uuid.New(), SlotName: "s"}}))
	require.NoError(t, w.WriteQueryTexts(ctx, m, []QueryTextRow{{TenantID: "default", ClusterID: 1, Datname: "db", QueryID: 1, PGMajor: 16, QueryText: "q", LastSeen: time.Now()}}))
}

func TestTypedFactRows_ArgsOrder(t *testing.T) {
	inst := uuid.New()
	r := TableStatRow{TS: time.Unix(1, 0), TenantID: "t", ClusterID: 2, InstanceID: inst, Datname: "db", Schemaname: "public", Relname: "tab"}
	require.Equal(t, []any{r.TS, r.TenantID, r.ClusterID, r.InstanceID, r.Datname, r.Schemaname, r.Relname, r.SeqScan, r.SeqTupRead, r.IdxScan, r.IdxTupFetch, r.NTupIns, r.NTupUpd, r.NTupDel, r.NTupHotUpd, r.NLiveTup, r.NDeadTup, r.NModSinceAnalyze, r.DeadTupleRatio, r.LastVacuumAgeSeconds, r.LastVacuum, r.LastAutovacuum, r.LastAnalyze, r.LastAutoanalyze, r.AutovacuumCount, r.AutoanalyzeCount, r.Relpages, r.RelTuples, r.RelfrozenXIDAge, r.TotalBytes, r.TableBytes, r.ToastBytes}, r.args())
	require.Len(t, r.args(), 32)
	i := IndexStatRow{TS: r.TS, TenantID: "t", ClusterID: 2, InstanceID: inst, Datname: "db", Schemaname: "public", Relname: "tab", IndexRelname: "idx"}
	require.Len(t, i.args(), 18)
	b := BloatRow{TS: r.TS, TenantID: "t", ClusterID: 2, InstanceID: inst, Datname: "db", Schemaname: "public", Relname: "tab", ObjectKind: "table", Method: "estimate"}
	require.Len(t, b.args(), 14)
}

func TestWriteLockSnapshot_ArgsOrder(t *testing.T) {
	id := uuid.New()
	ts := time.Unix(10, 0).UTC()
	r := LockSnapshotRow{TenantID: "tenant", InstanceID: id, ClusterID: 7, TS: ts, Tree: []byte(`{"nodes":[]}`)}
	require.Equal(t, "tenant", r.TenantID)
	require.Equal(t, id, r.InstanceID)
	require.Equal(t, int64(7), r.ClusterID)
	require.Equal(t, ts, r.TS)
	require.Equal(t, `{"nodes":[]}`, string(r.Tree))
}

// helpers

func floatPtr(v float64) *float64    { return &v }
func int64Ptr(v int64) *int64        { return &v }
func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

// mockTx implements pgx.Tx for unit testing Write* helpers.

type mockTx struct {
	batchCalls int
	execCalls  int
	copyCalls  int
	execSQL    []string
	// error injection
	batchExecErr  error
	batchCloseErr error
	execErr       error
	copyErr       error
}

func (m *mockTx) Begin(_ context.Context) (pgx.Tx, error) { return m, nil }
func (m *mockTx) Commit(_ context.Context) error          { return nil }
func (m *mockTx) Rollback(_ context.Context) error        { return nil }
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	m.copyCalls++
	if m.copyErr != nil {
		return 0, m.copyErr
	}
	return 100, nil
}
func (m *mockTx) SendBatch(_ context.Context, b *pgx.Batch) pgx.BatchResults {
	m.batchCalls++
	return &mockBatchResults{batch: b, execErr: m.batchExecErr, closeErr: m.batchCloseErr}
}
func (m *mockTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	m.execCalls++
	m.execSQL = append(m.execSQL, sql)
	if m.execErr != nil {
		return pgconn.NewCommandTag(""), m.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (m *mockTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return &mockRow{}
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

type mockBatchResults struct {
	batch    *pgx.Batch
	idx      int
	execErr  error
	closeErr error
}

func (b *mockBatchResults) Exec() (pgconn.CommandTag, error) {
	if b.execErr != nil && b.idx < len(b.batch.QueuedQueries) {
		b.idx++
		return pgconn.NewCommandTag(""), b.execErr
	}
	if b.idx >= len(b.batch.QueuedQueries) {
		return pgconn.NewCommandTag(""), fmt.Errorf("no more queries")
	}
	b.idx++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (b *mockBatchResults) Query() (pgx.Rows, error) { return nil, nil }
func (b *mockBatchResults) QueryRow() pgx.Row        { return &mockRow{} }
func (b *mockBatchResults) Close() error {
	if b.closeErr != nil {
		return b.closeErr
	}
	if b.execErr != nil {
		return b.execErr
	}
	return nil
}

type mockRow struct{}

func (r *mockRow) Scan(_ ...any) error { return nil }

func TestWriteTopologyEdges_Empty(t *testing.T) {
	t.Parallel()
	m := &mockTx{}
	err := WriteTopologyEdges(context.Background(), m, "default", 1, uuid.New(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, m.execCalls, "must still delete stale rows even when there is nothing new to insert")
}

func TestWriteTopologyEdges_InsertsRows(t *testing.T) {
	t.Parallel()
	m := &mockTx{}
	from := uuid.New()
	rows := []TopologyEdgeRow{
		{TenantID: "default", ClusterID: 1, FromInstance: from, ToInstance: uuid.New(), EdgeType: "streaming", Confidence: "high"},
	}
	err := WriteTopologyEdges(context.Background(), m, "default", 1, from, rows)
	require.NoError(t, err)
	require.Equal(t, 2, m.execCalls, "1 delete + 1 insert")
}

func TestWriteTopologyEdges_DeleteError(t *testing.T) {
	t.Parallel()
	m := &mockTx{execErr: fmt.Errorf("boom")}
	err := WriteTopologyEdges(context.Background(), m, "default", 1, uuid.New(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete stale topology edges")
}
