package check

import (
	"context"
	"errors"
	"time"

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

// relationProbeReserve is the time one relation probe needs after its lock
// wait: lock_timeout caps that wait at one second (ApplySessionLimits), and the
// rest is the round trip.
const relationProbeReserve = 2 * time.Second

// relationTimeLeft reports whether ctx has room for one more relation probe. A
// probe that starts with less than that can run into the check's own deadline,
// and the context error it then returns fails the whole scrape. A check that
// stops here leaves the relations it did not reach out, and the scrape reports
// them as partial, as it does for a locked relation.
func relationTimeLeft(ctx context.Context) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > relationProbeReserve
}
