package check

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATE codes the checks tell apart. Matching on error text instead used
// to turn unrelated failures (a permission error, an extension that is
// installed but not preloaded) into silently empty results.
const (
	pgInsufficientPrivilege        = "42501"
	pgUndefinedTable               = "42P01"
	pgUndefinedColumn              = "42703"
	pgObjectNotInPrerequisiteState = "55000"
	pgLockNotAvailable             = "55P03"
)

// pgErrorCode returns err's SQLSTATE, or "" when err is nil or did not come
// from the server.
func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// connectionFailed reports whether err ends the connection rather than one
// statement on it: the connection itself failed, or the server closed it (class
// 08, and the 57P operator-intervention codes such as admin_shutdown). A
// statement error on a live connection, such as a timeout (57014), is not one.
func connectionFailed(err error) bool {
	code := pgErrorCode(err)
	return code == "" || strings.HasPrefix(code, "08") || strings.HasPrefix(code, "57P")
}
