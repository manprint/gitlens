package check

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const extensionSchemaQuery = `SELECT n.nspname
  FROM pg_catalog.pg_extension e
  JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
 WHERE e.extname = $1`

// ExtensionSchema returns the schema the extension was installed in, in the
// database conn is connected to. Agent sessions search pg_catalog only
// (agent.ReadOnlySession), so whatever an extension created elsewhere is
// named with this schema: pgx.Identifier{schema, object}.Sanitize(). When
// the extension is not installed in that database the error carries
// SQLSTATE 42P01, as a reference to one of its objects would.
func ExtensionSchema(ctx context.Context, conn Conn, extension string) (string, error) {
	var schema string
	err := conn.QueryRow(ctx, extensionSchemaQuery, extension).Scan(&schema)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", &pgconn.PgError{
			Code:    pgUndefinedTable,
			Message: fmt.Sprintf("extension %q is not installed in this database", extension),
		}
	}
	if err != nil {
		return "", fmt.Errorf("locate extension %s: %w", extension, err)
	}
	return schema, nil
}
