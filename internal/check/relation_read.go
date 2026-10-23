package check

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// readRelation runs query, which takes one relation's OID as its only
// parameter, and scans that relation's row into dest. The size and definition
// functions open the relation with AccessShareLock, so an ACCESS EXCLUSIVE
// holder makes them wait out lock_timeout, and a relation dropped since the
// catalog was read has no row. Both return ok false and no error: the relation
// is left out of the scrape. Any other failure is returned.
//
// Callers read one relation per call. A statement that fails takes every
// relation in it down, so the probes that can wait are never batched.
func readRelation(ctx context.Context, conn Conn, query string, oid uint32, dest ...any) (bool, error) {
	err := conn.QueryRow(ctx, query, oid).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) || pgErrorCode(err) == pgLockNotAvailable {
		return false, nil
	}
	return err == nil, err
}
