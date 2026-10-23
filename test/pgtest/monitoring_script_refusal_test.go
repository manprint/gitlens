//go:build integration

package pgtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// INT-PERM-012: a run of the script that cannot start, because it has no
// password, an empty one, or names no database, exits non-zero and creates
// nothing. A run without a password used to exit 0, so an automation that
// calls the script reported success for a role that was never configured.
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

		// An empty password is no password: the role would be created with
		// LOGIN and a NULL password.
		code, out = pg.Psql(t, monitoringScript(), "app", "pglens_password=", "dbname=app", "role="+scriptRole)
		require.Equal(t, 3, code, out)
		require.Contains(t, out, "pglens_password is required")
		require.NotContains(t, out, "clearing password", "an empty password must not reach ALTER ROLE")
		require.False(t, roleExists(), "a run with an empty password must not create the role")

		code, out = pg.Psql(t, monitoringScript(), "app", "pglens_password=script", "dbname=no_such_db", "role="+scriptRole)
		require.NotEqual(t, 0, code, out)
		require.False(t, roleExists(), "a run that fails halfway must leave no role behind")
	})
}

// INT-PERM-014: a database the role owns is a shared object, which no database
// holds, so the refusal names it as that object. It used to name the location
// "(the cluster)", which does not say what owns what, or where to run the
// REASSIGN OWNED that removes it.
func TestPerm_INT_PERM_014_ScriptNamesTheSharedObjectItRefuses(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS perm_script_owned")
		require.NoError(t, err)
		dropScriptRoles(t, admin)
		// A database outlives DROP OWNED, so it is dropped before its owner.
		t.Cleanup(func() { dropScriptRoles(t, admin) })
		t.Cleanup(func() {
			_, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS perm_script_owned")
			require.NoError(t, err)
		})
		_, err = admin.Exec(ctx, "CREATE ROLE "+scriptRole)
		require.NoError(t, err)
		// CREATE DATABASE cannot run in a transaction block, so it is not
		// sent together with the statement that creates its owner.
		_, err = admin.Exec(ctx, "CREATE DATABASE perm_script_owned OWNER "+scriptRole)
		require.NoError(t, err)

		code, out := runMonitoringScript(t, pg, "tier1=1", "tier2=1")
		require.Equal(t, 3, code, out)
		require.Contains(t, out, "owns database perm_script_owned, a shared object: REASSIGN OWNED BY "+scriptRole+" TO <owner>;")
		require.NotContains(t, out, "(the cluster)")
	})
}
