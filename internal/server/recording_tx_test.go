package server

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// recordedExec is one statement Process ran through a transaction.
type recordedExec struct {
	sql  string
	args []any
}

// recordingTx is a mockTx that keeps every statement it executes, so a test
// can see what Process wrote, not only that it succeeded. Batched statements
// are recorded as well: the store writes metrics and events through SendBatch.
type recordingTx struct {
	mockTx
	execs []recordedExec
}

func (t *recordingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, recordedExec{sql: sql, args: args})
	return t.mockTx.Exec(ctx, sql, args...)
}

func (t *recordingTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	for _, q := range b.QueuedQueries {
		t.execs = append(t.execs, recordedExec{sql: q.SQL, args: q.Arguments})
	}
	return t.mockTx.SendBatch(ctx, b)
}
