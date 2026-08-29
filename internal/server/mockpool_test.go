package server

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// scanInto is a minimal, reflection-based stand-in for what pgx does when
// scanning a real row: it assigns positional values into the caller's dest
// pointers, honoring database/sql.Scanner for types like sql.NullString.
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

// mockRow implements pgx.Row for QueryRow. Set err to simulate pgx.ErrNoRows
// or any other lookup failure; otherwise vals are scanned positionally.
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

// mockRows implements pgx.Rows for Query. rows is a slice of positional
// value-sets, one per result row.
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

// mockPool implements dbPool. Each of queryRows/queryRowVals/execErrs/begins
// is consumed in call order by the corresponding method — tests script the
// exact sequence the code under test is known to issue.
type mockPool struct {
	queryResults  []*mockRows
	queryErrs     []error
	queryRowVals  []*mockRow
	execErrs      []error
	execRows      []int64
	execCalls     int
	queryCalls    int
	queryRowCalls int
	beginTx       pgx.Tx
	beginErr      error
}

func (m *mockPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	i := m.execCalls
	m.execCalls++
	if i < len(m.execErrs) && m.execErrs[i] != nil {
		return pgconn.CommandTag{}, m.execErrs[i]
	}
	if i < len(m.execRows) {
		return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", m.execRows[i])), nil
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (m *mockPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	i := m.queryCalls
	m.queryCalls++
	if i < len(m.queryErrs) && m.queryErrs[i] != nil {
		return nil, m.queryErrs[i]
	}
	if i < len(m.queryResults) && m.queryResults[i] != nil {
		return m.queryResults[i], nil
	}
	return &mockRows{}, nil
}

func (m *mockPool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	i := m.queryRowCalls
	m.queryRowCalls++
	if i < len(m.queryRowVals) && m.queryRowVals[i] != nil {
		return m.queryRowVals[i]
	}
	return &mockRow{err: pgx.ErrNoRows}
}

func (m *mockPool) Begin(_ context.Context) (pgx.Tx, error) {
	if m.beginErr != nil {
		return nil, m.beginErr
	}
	return m.beginTx, nil
}

// mockTx implements pgx.Tx for exercising Pipeline.Process's real-pool branch
// without a database. store.Write* take pgx.Tx directly so this is the only
// mock Process needs beyond mockPool.Begin.
type mockTx struct {
	commitErr   error
	rollbackErr error
	execErr     error
	committed   bool
	rolledBack  bool
}

func (t *mockTx) Begin(_ context.Context) (pgx.Tx, error) { return t, nil }
func (t *mockTx) Commit(_ context.Context) error {
	t.committed = true
	return t.commitErr
}
func (t *mockTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return t.rollbackErr
}
func (t *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *mockTx) SendBatch(_ context.Context, b *pgx.Batch) pgx.BatchResults {
	return &mockTxBatchResults{n: b.Len(), execErr: t.execErr}
}
func (t *mockTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (t *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *mockTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if t.execErr != nil {
		return pgconn.CommandTag{}, t.execErr
	}
	return pgconn.NewCommandTag("OK"), nil
}
func (t *mockTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &mockRows{}, nil
}
func (t *mockTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return &mockRow{} }
func (t *mockTx) Conn() *pgx.Conn                                        { return nil }

type mockTxBatchResults struct {
	n       int
	execErr error
}

func (b *mockTxBatchResults) Exec() (pgconn.CommandTag, error) {
	if b.execErr != nil {
		return pgconn.CommandTag{}, b.execErr
	}
	return pgconn.NewCommandTag("OK"), nil
}
func (b *mockTxBatchResults) Query() (pgx.Rows, error) { return &mockRows{}, nil }
func (b *mockTxBatchResults) QueryRow() pgx.Row        { return &mockRow{} }
func (b *mockTxBatchResults) Close() error             { return nil }
