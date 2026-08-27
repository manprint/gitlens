package server

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dbPool is the subset of *pgxpool.Pool used by Inventory, Pipeline, and API.
// It exists so unit tests can substitute a mock and exercise the real
// (non-nil-pool) code paths without a database; *pgxpool.Pool satisfies it
// unchanged. Staleness keeps a concrete *pgxpool.Pool because its leader
// election needs Acquire's concrete *pgxpool.Conn, which cannot be mocked.
type dbPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// asDBPool converts a possibly-nil *pgxpool.Pool into a dbPool, returning a
// true nil interface (not a non-nil interface wrapping a nil pointer) when
// pool is nil. This keeps every existing "pool == nil" nil-pool fast path
// working once the field type changes from *pgxpool.Pool to dbPool.
func asDBPool(pool *pgxpool.Pool) dbPool {
	if pool == nil {
		return nil
	}
	return pool
}
