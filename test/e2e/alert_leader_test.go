//go:build integration && e2e

package e2e

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/test/harness"
)

// TestAlertLeaderLock_HolderIsFound proves the query that failover uses finds
// the session holding the alert engine's advisory lock. The engine's own name
// hashes negative on PostgreSQL 18, which exercises the sign-extension in the
// query's masks; "k3" is a second name, so the unmasked half is checked too.
func TestAlertLeaderLock_HolderIsFound(t *testing.T) {
	pool := timescaleFixturePool(t)
	ctx := context.Background()

	for _, name := range []string{harness.AlertLockName, "k3"} {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)

		var pid int
		require.NoError(t, conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
		var locked bool
		require.NoError(t, conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext($1))`, name).Scan(&locked))
		require.True(t, locked, "lock %q was not taken", name)

		gotPID, gotAddr, err := harness.AdvisoryLockHolder(ctx, pool, name)
		require.NoError(t, err)
		require.Equal(t, pid, gotPID, "holder of %q", name)
		// The address is matched against container IPs, so it must be the bare
		// address the server sees for that session, not inet's text form.
		var wantAddr string
		require.NoError(t, pool.QueryRow(ctx, `SELECT COALESCE(host(client_addr), '') FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&wantAddr))
		require.NotEmpty(t, wantAddr, "holder %d has no TCP client address", pid)
		require.NotContains(t, gotAddr, "/", "holder address %q is in inet text form", gotAddr)
		require.Equal(t, wantAddr, gotAddr, "holder address of %q", name)

		_, err = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1))`, name)
		require.NoError(t, err)
		conn.Release()

		gotPID, _, err = harness.AdvisoryLockHolder(ctx, pool, name)
		require.NoError(t, err)
		require.Zero(t, gotPID, "lock %q is still reported as held after release", name)
	}
}
