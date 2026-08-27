package check

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// scanInto mimics pgx scanning: positional values to dest pointers, honoring sql.Scanner.
func scanInto(dest []any, vals []any) error {
	if len(dest) != len(vals) {
		return fmt.Errorf("mock scan: want %d dest, got %d values", len(dest), len(vals))
	}
	for i, d := range dest {
		v := vals[i]
		if scanner, ok := d.(sql.Scanner); ok {
			if err := scanner.Scan(v); err != nil {
				return fmt.Errorf("mock scan dest[%d]: %w", i, err)
			}
			continue
		}
		rv := reflect.ValueOf(d)
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			return fmt.Errorf("mock scan: dest[%d] (%T) is not a non-nil pointer", i, d)
		}
		elem := rv.Elem()
		if v == nil {
			elem.Set(reflect.Zero(elem.Type()))
			continue
		}
		vv := reflect.ValueOf(v)
		if elem.Kind() == reflect.Pointer {
			switch {
			case vv.Type() == elem.Type():
				elem.Set(vv)
			case vv.Type() == elem.Type().Elem():
				p := reflect.New(elem.Type().Elem())
				p.Elem().Set(vv)
				elem.Set(p)
			default:
				return fmt.Errorf("mock scan: dest[%d] type %s incompatible with value type %s", i, elem.Type(), vv.Type())
			}
			continue
		}
		if vv.Type() != elem.Type() {
			return fmt.Errorf("mock scan: dest[%d] type %s incompatible with value type %s", i, elem.Type(), vv.Type())
		}
		elem.Set(vv)
	}
	return nil
}

// mockRow implements pgx.Row.
type mockRow struct {
	vals []any
	err  error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(dest, r.vals)
}

// mockRows implements pgx.Rows.
type mockRows struct {
	rows []([]any)
	idx  int
	err  error
}

func (r *mockRows) Close()                                       {}
func (r *mockRows) Err() error                                   { return r.err }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

func (r *mockRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *mockRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return fmt.Errorf("mock scan: Scan called without a successful Next")
	}
	return scanInto(dest, r.rows[r.idx-1])
}

// mockConn implements Conn interface for unit testing.
type mockConn struct {
	queryFunc    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFunc func(ctx context.Context, sql string, args ...any) pgx.Row
	execFunc     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	releaseCalls int
}

func (m *mockConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *mockConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFunc != nil {
		return m.queryRowFunc(ctx, sql, args...)
	}
	return &mockRow{}
}

func (m *mockConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (m *mockConn) Release() {
	m.releaseCalls++
}

// TestApplySessionLimits_Happy applies session limits successfully.
func TestApplySessionLimits_Happy(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	err := ApplySessionLimits(context.Background(), conn, 30000000000) // 30s
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// TestApplySessionLimits_ExecError handles exec failure.
func TestApplySessionLimits_ExecError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("exec failed")
		},
	}

	err := ApplySessionLimits(context.Background(), conn, 30000000000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestActivityCheck_Scrape_Happy exercises activity check with sample data.
func TestActivityCheck_Scrape_Happy(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []([]any){
					{"active", "CPU", 5, 10.5, 0.0},
					{"idle in transaction", "Lock", 2, 30.2, 25.5},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &activityCheck{}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}
}

// TestActivityCheck_Scrape_QueryError handles query failures.
func TestActivityCheck_Scrape_QueryError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("query error")
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &activityCheck{}
	_, err := check.Scrape(context.Background(), target)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestActivityCheck_Scrape_ScanError handles scan failures.
func TestActivityCheck_Scrape_ScanError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []([]any){
					{"active", "CPU", "not-an-int", 10.5, 0.0}, // bad type
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &activityCheck{}
	_, err := check.Scrape(context.Background(), target)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestDatabaseStatsCheck_Scrape_Happy exercises database_stats check.
func TestDatabaseStatsCheck_Scrape_Happy(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				rows: []([]any){
					{"postgres", int64(100), int64(5), int64(1000), int64(50000), int64(0), int64(1024), int64(0), int64(3), time.Now()},
					{"app_db", int64(500), int64(10), int64(2000), int64(100000), int64(1), int64(2048), int64(5), int64(1), time.Now()},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &databaseStatsCheck{}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}
}

// TestDatabaseStatsCheck_Scrape_QueryError handles query failures.
func TestDatabaseStatsCheck_Scrape_QueryError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("query error")
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &databaseStatsCheck{}
	_, err := check.Scrape(context.Background(), target)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestInstanceInfoCheck_Scrape_Happy exercises instance_info check.
func TestInstanceInfoCheck_Scrape_Happy(t *testing.T) {
	sysID := "test-sysid-123"
	addr := "127.0.0.1"
	port := 5432
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{
				vals: []any{&sysID, false, 150000, time.Now(), &addr, &port},
			}
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &instanceInfoCheck{}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}
}

// TestInstanceInfoCheck_Scrape_QueryError handles query failures.
func TestInstanceInfoCheck_Scrape_QueryError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			return &mockRow{err: fmt.Errorf("perm denied")}
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &instanceInfoCheck{}
	_, err := check.Scrape(context.Background(), target)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestStatStatementsCheck_Scrape_Happy exercises stat_statements check.
func TestStatStatementsCheck_Scrape_Happy(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			q1 := "SELECT 1"
			q2 := "SELECT 2"
			return &mockRows{
				rows: []([]any){
					{int64(1), int64(100), float64(10.5), int64(1000), int64(5000), int64(500), int64(0), &q1},
					{int64(2), int64(200), float64(20.5), int64(2000), int64(15000), int64(1000), int64(512), &q2},
				},
			}, nil
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			var t *time.Time
			return &mockRow{vals: []any{t}}
		},
	}

	target := &SimpleTarget{
		ConnFunc:       func(ctx context.Context) (Conn, error) { return conn, nil },
		ConnForFunc:    func(ctx context.Context, datname string) (Conn, error) { return conn, nil },
		ClockValue:     clock.System(),
		PGVersionValue: 150000,
		PermTierValue:  1,
		Extensions:     map[string]bool{"pg_stat_statements": true},
		DatabaseValue:  "app",
	}

	check := &statStatementsCheck{selector: cardinality.NewSelector(cardinality.Options{TopN: 50}), caches: make(map[string]*lru.Cache[int64, string])}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}
}

// TestStatStatementsCheck_Scrape_NoExtension handles missing extension gracefully.
func TestStatStatementsCheck_Scrape_NoExtension(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("does not exist: pg_stat_statements")
		},
	}

	target := &SimpleTarget{
		ConnFunc:       func(ctx context.Context) (Conn, error) { return conn, nil },
		ConnForFunc:    func(ctx context.Context, datname string) (Conn, error) { return conn, nil },
		ClockValue:     clock.System(),
		PGVersionValue: 150000,
		PermTierValue:  1,
		Extensions:     map[string]bool{"pg_stat_statements": false},
		DatabaseValue:  "app",
	}

	check := &statStatementsCheck{selector: cardinality.NewSelector(cardinality.Options{TopN: 50}), caches: make(map[string]*lru.Cache[int64, string])}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) != 0 {
		t.Fatal("expected no metrics when extension missing")
	}
}

// TestAshCheck_Scrape_Happy exercises ash check.
func TestAshCheck_Scrape_Happy(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			var qid int64 = 12345
			return &mockRows{
				rows: []([]any){
					{"postgres", "active", "CPU", "CPU", &qid},
					{"app_db", "waiting", "Lock", "relation", nil},
				},
			}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &ashCheck{}
	result, err := check.Scrape(context.Background(), target)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(result.Metrics) == 0 {
		t.Fatal("expected metrics, got none")
	}
	// Every metric needs a real Kind: an unset Kind ("") makes
	// internal/server/pipeline.go's metric-kind switch reject the ENTIRE
	// instance's push for that cycle, not just this metric — ash_samples
	// shipped with Kind unset for the whole session until this was caught
	// live (STATE.md §8 records it), and this was the exact gap: no test
	// checked Kind at all, only that some metrics came back.
	for _, m := range result.Metrics {
		if m.Kind != pgtype.KindGauge && m.Kind != pgtype.KindCounter {
			t.Errorf("metric %q has invalid Kind %q", m.Name, m.Kind)
		}
	}
}

// TestAshCheck_Scrape_QueryError handles query failures.
func TestAshCheck_Scrape_QueryError(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("query error")
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &ashCheck{}
	_, err := check.Scrape(context.Background(), target)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestConnRelease verifies Release is called.
func TestConnRelease(t *testing.T) {
	conn := &mockConn{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, nil
		},
	}

	target := &SimpleTarget{
		ConnFunc:   func(ctx context.Context) (Conn, error) { return conn, nil },
		ClockValue: clock.System(),
	}

	check := &activityCheck{}
	_, _ = check.Scrape(context.Background(), target)
	if conn.releaseCalls != 1 {
		t.Fatalf("expected Release called once, got %d times", conn.releaseCalls)
	}
}
