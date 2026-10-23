//go:build integration

package pgtest

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// scriptRole is the role these tests let deploy/sql/monitoring_user.sql
// configure. Roles outlive the snapshot Lock restores, so the shared pglens
// role stays untouched and this one is dropped after each test.
const scriptRole = "pglens_script"

func monitoringScript() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../deploy/sql/monitoring_user.sql")
}

func runMonitoringScript(t *testing.T, pg *PG, flags ...string) (int, string) {
	t.Helper()
	vars := append([]string{"pglens_password=script", "dbname=app", "role=" + scriptRole}, flags...)
	return pg.Psql(t, monitoringScript(), "app", vars...)
}

func dropScriptRoles(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	_, err := admin.Exec(context.Background(), `DO $$
DECLARE r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['pglens_script', 'perm_script_app'] LOOP
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('DROP OWNED BY %I', r);
      EXECUTE format('DROP ROLE %I', r);
    END IF;
  END LOOP;
END $$`)
	require.NoError(t, err)
}

func connectAs(t *testing.T, pg *PG, role, password string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(pg.DSN("app", RoleSuperuser))
	require.NoError(t, err)
	cfg.User, cfg.Password = role, password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// INT-PERM-007: the role the shipped script configures, at every tier,
// cannot change data even after its session leaves read-only mode, and a
// database-specific setting cannot switch that mode off.
func TestPerm_INT_PERM_007_ScriptRoleCannotWrite(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		t.Cleanup(func() { dropScriptRoles(t, admin) })
		_, err := admin.Exec(ctx, `CREATE TABLE perm_script_t (a int);
INSERT INTO perm_script_t VALUES (1);
CREATE SEQUENCE perm_script_s`)
		require.NoError(t, err)

		for _, flags := range [][]string{nil, {"tier1=1"}, {"tier1=1", "tier2=1"}} {
			code, out := runMonitoringScript(t, pg, flags...)
			require.Equal(t, 0, code, out)
		}
		_, err = admin.Exec(ctx, "ALTER ROLE "+scriptRole+" IN DATABASE app SET default_transaction_read_only = off")
		require.NoError(t, err)
		code, out := runMonitoringScript(t, pg)
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "reset the default_transaction_read_only setting of "+scriptRole+" in database app")

		conn := connectAs(t, pg, scriptRole, "script")
		var readOnly string
		require.NoError(t, conn.QueryRow(ctx, "SHOW default_transaction_read_only").Scan(&readOnly))
		require.Equal(t, "on", readOnly)
		_, err = conn.Exec(ctx, "INSERT INTO perm_script_t VALUES (2)")
		require.True(t, isSQLState(err, "25006"), "%v", err)

		_, err = conn.Exec(ctx, "SET default_transaction_read_only = off")
		require.NoError(t, err)
		for _, statement := range []string{
			"INSERT INTO perm_script_t VALUES (2)",
			"UPDATE perm_script_t SET a = 3",
			"DELETE FROM perm_script_t",
			"TRUNCATE perm_script_t",
			"SELECT a FROM perm_script_t FOR UPDATE",
			"SELECT nextval('perm_script_s')",
			"ALTER TABLE perm_script_t ADD COLUMN b int",
			"COMMENT ON TABLE perm_script_t IS 'changed'",
			"DROP TABLE perm_script_t",
			"CREATE TABLE public.perm_script_new (a int)",
			"CREATE SCHEMA perm_script_schema",
			"CREATE DATABASE perm_script_db",
			"CREATE ROLE perm_script_role",
			"ALTER SYSTEM SET work_mem = '1MB'",
		} {
			_, err := conn.Exec(ctx, statement)
			require.True(t, isSQLState(err, "42501"), "%s: %v", statement, err)
		}
		var rows int
		require.NoError(t, admin.QueryRow(ctx, "SELECT count(*) FROM perm_script_t WHERE a = 1").Scan(&rows))
		require.Equal(t, 1, rows)
	})
}

// INT-PERM-008: the shipped script refuses a role that could write, names
// the statement that fixes each problem, and changes nothing.
func TestPerm_INT_PERM_008_ScriptRefusesWritableRole(t *testing.T) {
	cases := []struct {
		name  string
		setup string
		fix   string
	}{
		{"superuser", "CREATE ROLE pglens_script SUPERUSER", "ALTER ROLE pglens_script NOSUPERUSER;"},
		{"createdb", "CREATE ROLE pglens_script CREATEDB", "ALTER ROLE pglens_script NOCREATEDB;"},
		{"bypassrls", "CREATE ROLE pglens_script BYPASSRLS", "ALTER ROLE pglens_script NOBYPASSRLS;"},
		{"write role", "CREATE ROLE pglens_script; GRANT pg_write_all_data TO pglens_script",
			"REVOKE pg_write_all_data FROM pglens_script;"},
		{"indirect write role", `CREATE ROLE pglens_script; CREATE ROLE perm_script_app;
GRANT pg_write_all_data TO perm_script_app; GRANT perm_script_app TO pglens_script`,
			"pglens_script is a member of pg_write_all_data through another role"},
		{"owner", `CREATE ROLE pglens_script; CREATE TABLE perm_script_owned (a int);
ALTER TABLE perm_script_owned OWNER TO pglens_script`, "REASSIGN OWNED BY pglens_script TO <owner>;"},
		{"table grant", `CREATE ROLE pglens_script; CREATE TABLE perm_script_t (a int);
GRANT SELECT, INSERT ON perm_script_t TO pglens_script`, "REVOKE INSERT ON perm_script_t FROM pglens_script;"},
		{"column grant", `CREATE ROLE pglens_script; CREATE TABLE perm_script_t (a int);
GRANT UPDATE (a) ON perm_script_t TO pglens_script`, "REVOKE UPDATE (a) ON perm_script_t FROM pglens_script;"},
		{"sequence grant", `CREATE ROLE pglens_script; CREATE SEQUENCE perm_script_s;
GRANT USAGE ON SEQUENCE perm_script_s TO pglens_script`, "REVOKE USAGE ON perm_script_s FROM pglens_script;"},
		{"schema grant", `CREATE ROLE pglens_script; GRANT CREATE ON SCHEMA public TO pglens_script`,
			"REVOKE CREATE ON SCHEMA public FROM pglens_script;"},
		{"database grant", `CREATE ROLE pglens_script; GRANT TEMPORARY ON DATABASE app TO pglens_script`,
			"REVOKE TEMPORARY ON DATABASE app FROM pglens_script;"},
		{"parameter grant", `CREATE ROLE pglens_script; GRANT ALTER SYSTEM ON PARAMETER work_mem TO pglens_script`,
			"REVOKE ALTER SYSTEM ON PARAMETER work_mem FROM pglens_script;"},
		{"default privilege", `CREATE ROLE pglens_script;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres GRANT INSERT ON TABLES TO pglens_script`,
			"ALTER DEFAULT PRIVILEGES FOR ROLE postgres REVOKE INSERT ON TABLES FROM pglens_script;"},
	}
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		t.Cleanup(func() { dropScriptRoles(t, admin) })
		for _, tc := range cases {
			dropScriptRoles(t, admin)
			_, err := admin.Exec(ctx, "DROP TABLE IF EXISTS perm_script_t, perm_script_owned; DROP SEQUENCE IF EXISTS perm_script_s")
			require.NoError(t, err)
			_, err = admin.Exec(ctx, tc.setup)
			require.NoError(t, err, tc.name)

			code, out := runMonitoringScript(t, pg, "tier1=1", "tier2=1")
			require.Equal(t, 3, code, "%s: %s", tc.name, out)
			require.Contains(t, out, "refusing to configure pglens_script", tc.name)
			require.Contains(t, out, tc.fix, tc.name)

			// The script ran in one transaction: the role neither got a
			// password and LOGIN nor any of the tier memberships.
			var canLogin, monitor, readAll, signal bool
			require.NoError(t, admin.QueryRow(ctx, `SELECT rolcanlogin,
       pg_has_role(oid, 'pg_monitor', 'MEMBER') AND NOT rolsuper,
       pg_has_role(oid, 'pg_read_all_data', 'MEMBER') AND NOT rolsuper,
       pg_has_role(oid, 'pg_signal_backend', 'MEMBER') AND NOT rolsuper
FROM pg_roles WHERE rolname = $1`, scriptRole).Scan(&canLogin, &monitor, &readAll, &signal))
			require.False(t, canLogin, tc.name)
			require.False(t, monitor, tc.name)
			require.False(t, readAll, tc.name)
			require.False(t, signal, tc.name)
		}
	})
}
