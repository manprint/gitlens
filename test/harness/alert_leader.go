//go:build e2e

package harness

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertLockName is the advisory lock the alert engine takes for leadership
// (internal/alert/engine.go). Only the replica holding it evaluates rules.
const AlertLockName = "pglens:alert-engine"

// advisoryLockHolderQuery finds the backend holding the advisory lock named $1,
// taken the way the engine takes it: pg_try_advisory_lock(hashtext(name)).
// A single bigint key shows in pg_locks as its high 32 bits in classid, its low
// 32 bits in objid, and objsubid 1. hashtext is an int4, so a negative hash
// sign-extends to a bigint whose high half is all ones; the masks take the
// unsigned halves that pg_locks reports, for negative and positive hashes alike.
const advisoryLockHolderQuery = `
WITH k AS (SELECT hashtext($1)::bigint AS key)
SELECT a.pid, COALESCE(a.client_addr::text, '')
  FROM pg_locks l
  CROSS JOIN k
  JOIN pg_stat_activity a ON a.pid = l.pid
 WHERE l.locktype = 'advisory' AND l.granted AND l.objsubid = 1
   AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
   AND l.classid::bigint = ((k.key >> 32) & 4294967295)
   AND l.objid::bigint = (k.key & 4294967295)`

// AdvisoryLockHolder returns the backend pid and client address holding the
// advisory lock name in pool's database, or 0 and "" when nobody holds it. A
// TCP client's address is its container's address on the compose network.
func AdvisoryLockHolder(ctx context.Context, pool *pgxpool.Pool, name string) (int, string, error) {
	var pid int
	var addr string
	err := pool.QueryRow(ctx, advisoryLockHolderQuery, name).Scan(&pid, &addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("find holder of advisory lock %q: %w", name, err)
	}
	return pid, addr, nil
}
