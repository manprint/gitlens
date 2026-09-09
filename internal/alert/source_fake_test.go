package alert

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeQueryer scripts one result set per Query call and records the SQL and
// arguments it was asked for, so the source's own SQL shape is testable
// without a database.
type fakeQueryer struct {
	results []*fakeRows
	errs    []error
	calls   []fakeCall
}

type fakeCall struct {
	sql  string
	args []any
}

func (f *fakeQueryer) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	i := len(f.calls)
	f.calls = append(f.calls, fakeCall{sql: sql, args: args})
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.results) {
		return f.results[i], nil
	}
	return &fakeRows{}, nil
}

type fakeRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return fmt.Errorf("fake scan: Scan called without a successful Next")
	}
	src := r.rows[r.idx-1]
	if len(src) != len(dest) {
		return fmt.Errorf("fake scan: %d destinations for %d columns", len(dest), len(src))
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *int64:
			*d = src[i].(int64)
		case *float64:
			*d = src[i].(float64)
		case *string:
			*d = src[i].(string)
		case *time.Time:
			*d = src[i].(time.Time)
		case *uuid.UUID:
			*d = src[i].(uuid.UUID)
		default:
			return fmt.Errorf("fake scan: unsupported destination %T", dest[i])
		}
	}
	return nil
}
