package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/command"
)

// PgstattupleExecutor performs an exact relation scan only when the target
// already has the pgstattuple extension. It never installs extensions.
type PgstattupleExecutor struct{}

func NewPgstattupleExecutor() *PgstattupleExecutor { return &PgstattupleExecutor{} }

func (e *PgstattupleExecutor) Kind() command.Kind { return command.KindPgstattuple }

func (e *PgstattupleExecutor) Execute(ctx context.Context, target check.Target, args command.Args) (json.RawMessage, error) {
	if !target.HasExtension("pgstattuple") {
		return nil, rejectedf("pgstattuple extension is not installed; pglens does not install it")
	}
	conn, err := target.ConnFor(ctx, args.Datname)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(ctx, "ROLLBACK") }()
	if _, err := conn.Exec(ctx, "SET LOCAL statement_timeout = '300s'"); err != nil {
		return nil, err
	}

	relation := (pgx.Identifier{args.Schema, args.Relation}).Sanitize()
	const query = `SELECT table_len, tuple_count, tuple_len, dead_tuple_count, dead_tuple_len, free_space, free_percent FROM pgstattuple($1::regclass)`
	var result pgstattupleResult
	if err := conn.QueryRow(ctx, query, relation).Scan(
		&result.TableLen,
		&result.TupleCount,
		&result.TupleLen,
		&result.DeadTupleCount,
		&result.DeadTupleLen,
		&result.FreeSpace,
		&result.FreePercent,
	); err != nil {
		return nil, fmt.Errorf("pgstattuple %s: %w", relation, err)
	}
	result.Schema = args.Schema
	result.Relation = args.Relation
	return json.Marshal(result)
}

type pgstattupleResult struct {
	Schema         string  `json:"schema"`
	Relation       string  `json:"relation"`
	TableLen       int64   `json:"table_len"`
	TupleCount     int64   `json:"tuple_count"`
	TupleLen       int64   `json:"tuple_len"`
	DeadTupleCount int64   `json:"dead_tuple_count"`
	DeadTupleLen   int64   `json:"dead_tuple_len"`
	FreeSpace      int64   `json:"free_space"`
	FreePercent    float64 `json:"free_percent"`
}
