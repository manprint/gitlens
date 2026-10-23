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
	conn, err := target.ConnFor(ctx, args.Datname)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	// READ ONLY: pgstattuple only reads, and pglens never opens a
	// transaction that could write to a monitored database.
	if _, err := conn.Exec(ctx, "BEGIN READ ONLY"); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(ctx, "ROLLBACK") }()
	// The statement timeout stays inside the dispatcher's own execution limit
	// (commandExecutionLimit). It used to be 300s: the agent gave up after
	// 60s and closed the socket, but pgx sends no cancel request on context
	// expiry, so the server went on scanning the relation for minutes after
	// nobody was waiting for the answer.
	if _, err := conn.Exec(ctx, pgstattupleStatementTimeout); err != nil {
		return nil, err
	}
	// Extensions are per database, so the one that counts is installed where
	// the relation lives. target.HasExtension reads the maintenance database
	// only: it refused a measurement in a database that has the extension,
	// and let one through to fail in a database that lacks it.
	schema, err := check.ExtensionSchema(ctx, conn, "pgstattuple")
	if isUndefinedTable(err) {
		where := "the target's database"
		if args.Datname != "" {
			where = fmt.Sprintf("database %q", args.Datname)
		}
		return nil, rejectedf("pgstattuple extension is not installed in %s; pglens does not install it", where)
	}
	if err != nil {
		return nil, err
	}

	relation := (pgx.Identifier{args.Schema, args.Relation}).Sanitize()
	// current_database() names the database the scan ran in: a command
	// without datname runs in the target's own database, and the server
	// filed its measurement under an empty database name.
	query := `SELECT current_database(), table_len, tuple_count, tuple_len, dead_tuple_count, dead_tuple_len, free_space, free_percent FROM ` +
		pgx.Identifier{schema, "pgstattuple"}.Sanitize() + `($1::pg_catalog.regclass)`
	var result pgstattupleResult
	if err := conn.QueryRow(ctx, query, relation).Scan(
		&result.Datname,
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

// pgstattupleStatementTimeout is kept below commandExecutionLimit so the
// server abandons the scan before the dispatcher abandons the command.
const pgstattupleStatementTimeout = "SET LOCAL statement_timeout = '50s'"

type pgstattupleResult struct {
	Datname        string  `json:"datname"`
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
