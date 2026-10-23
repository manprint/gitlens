//go:build integration

package pgtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// INT-PERM-013: PostgreSQL 15 to 19 grant pg_control_system() and
// pg_control_checkpoint() to PUBLIC on a fresh cluster, so a role that holds
// no grant of its own can read the cluster identity. The monitoring script
// grants both anyway, for hardened clusters that revoke PUBLIC. This pins the
// default the harness must reproduce, so the revoked case in INT-PERM-002 is
// the exception, not the baseline.
func TestPerm_INT_PERM_013_ControlFunctionsDefaultToPublic(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", RoleT0NoControl)
		var id, lsn string
		require.NoError(t, pool.QueryRow(context.Background(),
			"SELECT system_identifier::text FROM pg_control_system()").Scan(&id))
		require.NotEmpty(t, id)
		require.NoError(t, pool.QueryRow(context.Background(),
			"SELECT checkpoint_lsn::text FROM pg_control_checkpoint()").Scan(&lsn))
		require.NotEmpty(t, lsn)
	})
}
