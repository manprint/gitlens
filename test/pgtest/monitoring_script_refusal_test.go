//go:build integration

package pgtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// INT-PERM-012: a run of the script that cannot start, because it has no
// password or names no database, exits non-zero and creates nothing. A
// password-less run exits 0 today, so an automation that calls the script
// reports success for a role that was never configured.
func TestPerm_INT_PERM_012_ScriptRefusesRunsThatCannotConfigure(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		t.Cleanup(func() { dropScriptRoles(t, admin) })

		roleExists := func() bool {
			var exists bool
			require.NoError(t, admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", scriptRole).Scan(&exists))
			return exists
		}

		code, out := pg.Psql(t, monitoringScript(), "app", "dbname=app", "role="+scriptRole)
		require.Equal(t, 3, code, out)
		require.Contains(t, out, "pglens_password is required")
		require.NotContains(t, out, ">&2", "psql does not redirect \\echo, so the text must not carry it")
		require.False(t, roleExists(), "a run without a password must not create the role")

		code, out = pg.Psql(t, monitoringScript(), "app", "pglens_password=script", "dbname=no_such_db", "role="+scriptRole)
		require.NotEqual(t, 0, code, out)
		require.False(t, roleExists(), "a run that fails halfway must leave no role behind")
	})
}
