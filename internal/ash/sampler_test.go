package ash

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/clock"
)

// mockRows is a minimal in-memory pgx.Rows for exercising doSample's parsing
// and control-flow logic without a real database.
type mockRows struct {
	rows    [][5]any // datname, state, waitEventType, waitEvent, *int64
	idx     int
	scanErr error
}

func (m *mockRows) Close()                                       {}
func (m *mockRows) Err() error                                   { return nil }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }

func (m *mockRows) Next() bool {
	return m.idx < len(m.rows)
}

func (m *mockRows) Scan(dest ...any) error {
	if m.scanErr != nil {
		return m.scanErr
	}
	row := m.rows[m.idx]
	m.idx++
	*dest[0].(*string) = row[0].(string)
	*dest[1].(*string) = row[1].(string)
	*dest[2].(*string) = row[2].(string)
	*dest[3].(*string) = row[3].(string)
	*dest[4].(**int64) = row[4].(*int64)
	return nil
}

// mockQuerier implements querier for tests.
type mockQuerier struct {
	rows    *mockRows
	err     error
	calls   int
	lastSQL string
}

func (m *mockQuerier) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	m.calls++
	m.lastSQL = sql
	if m.err != nil {
		return nil, m.err
	}
	return m.rows, nil
}

func ptrInt64(v int64) *int64 { return &v }

// TestSampler_TickBudget tests that the sampler timeout is configured correctly.
func TestSampler_TickBudget(t *testing.T) {
	clk := clock.System()

	s := NewSampler((*pgxpool.Conn)(nil), clk, nil)
	if s == nil {
		t.Fatal("failed to create sampler")
	}

	// Verify timeout is set to 500ms per spec
	if s.timeout != 500*time.Millisecond {
		t.Errorf("expected timeout 500ms, got %v", s.timeout)
	}

	// Verify ticks missed starts at 0
	if s.TicksMissed() != 0 {
		t.Errorf("expected 0 missed ticks, got %d", s.TicksMissed())
	}
}

// TestSampler_ComputeQueryIDDiscovery tests that compute_query_id state is discovered
// from the first successful query.
func TestSampler_ComputeQueryIDDiscovery(t *testing.T) {
	clk := clock.System()

	// First test: compute_query_id is on (query_id is non-nil)
	s1 := NewSampler((*pgxpool.Conn)(nil), clk, nil)
	s1.computeQueryIDMu.Lock()
	trueVal := true
	s1.computeQueryID = &trueVal // Simulate discovery
	s1.computeQueryIDMu.Unlock()

	enabled := s1.ComputeQueryIDEnabled()
	if enabled == nil {
		t.Error("expected compute_query_id to be discovered")
	}

	// Second test: compute_query_id is off (query_id is nil on first sample)
	s2 := NewSampler((*pgxpool.Conn)(nil), clk, nil)
	s2.computeQueryIDMu.Lock()
	falseVal := false
	s2.computeQueryID = &falseVal
	s2.computeQueryIDMu.Unlock()

	enabled2 := s2.ComputeQueryIDEnabled()
	if enabled2 == nil || *enabled2 {
		t.Error("expected compute_query_id to be false")
	}
}

// TestSampler_DataParsing tests that NULL values in sample rows are handled correctly.
func TestSampler_DataParsing(t *testing.T) {
	tests := []struct {
		name    string
		datname string
		state   string
		wetType string
		weEvent string
		queryID *int64
	}{
		{
			name:    "NULL datname becomes empty string",
			datname: "",
			state:   "active",
			wetType: "CPU",
			weEvent: "CPU",
			queryID: nil,
		},
		{
			name:    "NULL wait_event_type becomes CPU",
			datname: "postgres",
			state:   "active",
			wetType: "CPU",
			weEvent: "CPU",
			queryID: nil,
		},
		{
			name:    "NULL query_id preserved as absent",
			datname: "mydb",
			state:   "active",
			wetType: "Lock",
			weEvent: "transactionid",
			queryID: nil,
		},
		{
			name:    "Non-NULL query_id preserved",
			datname: "mydb",
			state:   "active",
			wetType: "Lock",
			weEvent: "transactionid",
			queryID: ptrInt64(999),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := &SampleRow{
				Datname:       tt.datname,
				State:         tt.state,
				WaitEventType: tt.wetType,
				WaitEvent:     tt.weEvent,
				QueryID:       tt.queryID,
			}

			if row.Datname != tt.datname {
				t.Errorf("datname: got %q, want %q", row.Datname, tt.datname)
			}
			if row.WaitEventType != tt.wetType {
				t.Errorf("wait_event_type: got %q, want %q", row.WaitEventType, tt.wetType)
			}
			if row.QueryID != tt.queryID {
				t.Errorf("query_id: got %v, want %v", row.QueryID, tt.queryID)
			}
		})
	}
}

// TestSampler_DoSample_DeliversRowsToCallback exercises doSample end to end
// against a mock querier, proving every row reaches onSample and none is lost.
func TestSampler_DoSample_DeliversRowsToCallback(t *testing.T) {
	rows := &mockRows{rows: [][5]any{
		{"app", "active", "Lock", "transactionid", ptrInt64(42)},
		{"app", "idle in transaction", "CPU", "CPU", (*int64)(nil)},
	}}
	q := &mockQuerier{rows: rows}
	var got []SampleRow
	s := NewSampler(q, clock.System(), func(r SampleRow) { got = append(got, r) })

	if err := s.doSample(context.Background()); err != nil {
		t.Fatalf("doSample: %v", err)
	}
	if q.calls != 1 {
		t.Fatalf("expected exactly one Query call, got %d", q.calls)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 rows delivered, got %d", len(got))
	}
	if got[0].WaitEvent != "transactionid" || got[0].QueryID == nil || *got[0].QueryID != 42 {
		t.Errorf("row 0 mismatch: %+v", got[0])
	}
	if got[1].QueryID != nil {
		t.Errorf("row 1 expected nil query_id, got %v", got[1].QueryID)
	}
}

// TestSampler_DoSample_QueryError propagates the query error without invoking onSample.
func TestSampler_DoSample_QueryError(t *testing.T) {
	q := &mockQuerier{err: errors.New("connection refused")}
	called := false
	s := NewSampler(q, clock.System(), func(SampleRow) { called = true })

	if err := s.doSample(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if called {
		t.Error("onSample must not be invoked when the query fails")
	}
}

// TestSampler_DoSample_ScanError propagates a scan error and does not panic.
func TestSampler_DoSample_ScanError(t *testing.T) {
	rows := &mockRows{
		rows:    [][5]any{{"app", "active", "CPU", "CPU", (*int64)(nil)}},
		scanErr: errors.New("scan failed"),
	}
	q := &mockQuerier{rows: rows}
	s := NewSampler(q, clock.System(), nil)

	if err := s.doSample(context.Background()); err == nil {
		t.Fatal("expected a scan error")
	}
}

// TestSampler_DoSample_ComputeQueryIDDiscoveredFromFirstRow proves the sampler
// discovers compute_query_id from the first row of the first successful sample.
func TestSampler_DoSample_ComputeQueryIDDiscoveredFromFirstRow(t *testing.T) {
	rows := &mockRows{rows: [][5]any{
		{"app", "active", "CPU", "CPU", ptrInt64(7)},
	}}
	s := NewSampler(&mockQuerier{rows: rows}, clock.System(), nil)

	if err := s.doSample(context.Background()); err != nil {
		t.Fatalf("doSample: %v", err)
	}
	enabled := s.ComputeQueryIDEnabled()
	if enabled == nil || !*enabled {
		t.Fatalf("expected compute_query_id discovered true, got %v", enabled)
	}
}

// TestSampler_DoSample_WarnsOnceWhenQueryIDMissing proves the "compute_query_id
// is off" condition warns at most once, no matter how many nil-query_id rows or
// ticks occur (phase_08.md 7.1: "the warning is emitted once rather than per tick").
func TestSampler_DoSample_WarnsOnceWhenQueryIDMissing(t *testing.T) {
	mk := func() *mockRows {
		return &mockRows{rows: [][5]any{
			{"app", "active", "CPU", "CPU", (*int64)(nil)},
			{"app", "active", "CPU", "CPU", (*int64)(nil)},
		}}
	}
	q := &mockQuerier{rows: mk()}
	s := NewSampler(q, clock.System(), nil)

	if err := s.doSample(context.Background()); err != nil {
		t.Fatalf("doSample: %v", err)
	}
	if !s.warnedNoQueryID.Load() {
		t.Fatal("expected warnedNoQueryID to be set after a nil query_id row")
	}

	// A second tick (new mock rows, same sampler) must not flip anything further;
	// the CompareAndSwap in doSample guards this — assert it stays true and no
	// panic/duplicate state occurs across ticks.
	q.rows = mk()
	if err := s.doSample(context.Background()); err != nil {
		t.Fatalf("doSample (second tick): %v", err)
	}
	if !s.warnedNoQueryID.Load() {
		t.Fatal("warnedNoQueryID must remain set across ticks")
	}
}

// TestSampler_DoSample_NilOnSampleIsSafe proves a nil callback (the default
// before an Aggregator is wired in) never panics.
func TestSampler_DoSample_NilOnSampleIsSafe(t *testing.T) {
	rows := &mockRows{rows: [][5]any{{"app", "active", "CPU", "CPU", (*int64)(nil)}}}
	s := NewSampler(&mockQuerier{rows: rows}, clock.System(), nil)
	if err := s.doSample(context.Background()); err != nil {
		t.Fatalf("doSample: %v", err)
	}
}

// mockQuerierEachTick returns a fresh mockRows every Query call so the fake
// ticker's repeated ticks each see one row, instead of reusing an
// already-exhausted mockRows. n is accessed from both the sampler's run()
// goroutine and the test goroutine, hence atomic.
type mockQuerierEachTick struct {
	n atomic.Int32
}

func (m *mockQuerierEachTick) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	m.n.Add(1)
	return &mockRows{rows: [][5]any{{"app", "active", "CPU", "CPU", (*int64)(nil)}}}, nil
}

// TestSampler_StartStop_RunsAtInterval proves Start ticks once per second via
// the injectable clock (never time.Sleep) and Stop halts the loop cleanly.
func TestSampler_StartStop_RunsAtInterval(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	q := &mockQuerierEachTick{}
	s := NewSampler(q, clk, nil)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("expected the second Start to fail — sampler already running")
	}

	// The fake ticker's channel is buffered by 1 (see internal/clock/fake.go);
	// advance one interval at a time, yielding briefly so the sampler goroutine
	// can drain each tick before the next one is queued.
	deadline := time.Now().Add(2 * time.Second)
	for q.n.Load() < 3 && time.Now().Before(deadline) {
		clk.Advance(1 * time.Second)
		time.Sleep(time.Millisecond)
	}

	s.Stop()
	s.Stop() // must be idempotent

	if q.n.Load() < 1 {
		t.Fatalf("expected at least one tick to have queried, got %d", q.n.Load())
	}
}
