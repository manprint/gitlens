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
		t.Cleanup(func() { regrantLargeObjects(t, admin) })
		t.Cleanup(func() { regrantWALEmitters(t, admin) })
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
		// A WAL message is not a data change, but the read-only default does
		// not stop it, so the role must not hold EXECUTE on the function.
		_, err = conn.Exec(ctx, "SELECT pg_catalog.pg_logical_emit_message(true, 'pglens', 'probe')")
		require.True(t, isSQLState(err, "42501"), "%v", err)
		// Large objects under the default. Only 42501, the privilege refusal,
		// shows the revoke: without it PostgreSQL 15 writes, and 16 and later
		// refuse with 25006 from the read-only default. Each call is rolled
		// back, so the failing case leaves no large object behind.
		for _, statement := range []string{
			"SELECT pg_catalog.lo_create(0)",
			"SELECT pg_catalog.lo_from_bytea(0, '\\x00')",
		} {
			tx, err := conn.Begin(ctx)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, statement)
			require.NoError(t, tx.Rollback(ctx))
			require.True(t, isSQLState(err, "42501"), "%s: %v", statement, err)
		}

		_, err = conn.Exec(ctx, "SET default_transaction_read_only = off")
		require.NoError(t, err)
		for _, statement := range []string{
			"INSERT INTO perm_script_t VALUES (2)",
			"SELECT pg_catalog.pg_logical_emit_message(true, 'pglens', 'probe')",
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
			"SELECT pg_catalog.lo_create(0)",
			"SELECT pg_catalog.lo_from_bytea(0, '\\x00')",
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
GRANT SELECT, INSERT ON perm_script_t TO pglens_script`, "REVOKE INSERT ON public.perm_script_t FROM pglens_script;"},
		{"column grant", `CREATE ROLE pglens_script; CREATE TABLE perm_script_t (a int);
GRANT UPDATE (a) ON perm_script_t TO pglens_script`, "REVOKE UPDATE (a) ON public.perm_script_t FROM pglens_script;"},
		{"sequence grant", `CREATE ROLE pglens_script; CREATE SEQUENCE perm_script_s;
GRANT USAGE ON SEQUENCE perm_script_s TO pglens_script`, "REVOKE USAGE ON public.perm_script_s FROM pglens_script;"},
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

// INT-PERM-009: the shipped script reports the grants PUBLIC holds that let
// any role write, and with harden_public revokes them, so the role it
// configures cannot write even through PUBLIC.
func TestPerm_INT_PERM_009_ScriptHardensPublic(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		// The database ACL lives in a shared catalog the snapshot does not
		// restore, so every grant this test changes goes back.
		t.Cleanup(func() {
			_, err := admin.Exec(context.Background(), `REVOKE CREATE ON DATABASE app FROM PUBLIC;
GRANT TEMPORARY ON DATABASE app TO PUBLIC;
GRANT EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer),
  pg_catalog.lo_from_bytea(oid, bytea) TO PUBLIC;
DROP TABLE IF EXISTS perm_script_public;
DROP SEQUENCE IF EXISTS perm_script_public_s;
DROP FUNCTION IF EXISTS perm_script_definer()`)
			require.NoError(t, err)
			regrantWALEmitters(t, admin)
			dropScriptRoles(t, admin)
		})
		_, err := admin.Exec(ctx, `CREATE TABLE perm_script_public (a int);
GRANT INSERT ON perm_script_public TO PUBLIC;
CREATE SEQUENCE perm_script_public_s;
GRANT USAGE ON SEQUENCE perm_script_public_s TO PUBLIC;
GRANT CREATE ON DATABASE app TO PUBLIC;
CREATE FUNCTION perm_script_definer() RETURNS int LANGUAGE sql SECURITY DEFINER AS 'SELECT 1'`)
		require.NoError(t, err)

		code, out := runMonitoringScript(t, pg)
		require.Equal(t, 0, code, out)
		require.Contains(t, out, scriptRole+", like every role, can write outside a read-only transaction")
		require.Contains(t, out, "revoked EXECUTE on pg_catalog.lo_create(oid) from PUBLIC")
		for _, fix := range []string{
			"REVOKE TEMPORARY ON DATABASE app FROM PUBLIC;",
			"REVOKE CREATE ON DATABASE app FROM PUBLIC;",
			"REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON public.perm_script_public FROM PUBLIC;",
			"REVOKE USAGE, UPDATE ON SEQUENCE public.perm_script_public_s FROM PUBLIC;",
			"REVOKE EXECUTE ON FUNCTION public.perm_script_definer() FROM PUBLIC;",
		} {
			require.Contains(t, out, fix)
		}

		code, out = runMonitoringScript(t, pg, "harden_public=1")
		require.Equal(t, 0, code, out)
		// A function that runs as its owner is only reported: revoking
		// EXECUTE is the application's call.
		require.Contains(t, out, "after harden_public, "+scriptRole+" can still")
		require.Contains(t, out, "public.perm_script_definer()")
		require.NotContains(t, out, "REVOKE TEMPORARY ON DATABASE app FROM PUBLIC;")

		conn := connectAs(t, pg, scriptRole, "script")
		_, err = conn.Exec(ctx, "SET default_transaction_read_only = off")
		require.NoError(t, err)
		for _, statement := range []string{
			"SELECT pg_catalog.lo_create(0)",
			"SELECT pg_catalog.lo_creat(-1)",
			"SELECT pg_catalog.lo_from_bytea(0, '\\x00')",
			"CREATE TEMP TABLE perm_script_temp (a int)",
			"CREATE SCHEMA perm_script_schema",
			"INSERT INTO perm_script_public VALUES (1)",
			"SELECT nextval('perm_script_public_s')",
			"CREATE TABLE public.perm_script_new (a int)",
		} {
			_, err := conn.Exec(ctx, statement)
			require.True(t, isSQLState(err, "42501"), "%s: %v", statement, err)
		}
	})
}

// INT-PERM-015: a run warns about every other database the role can connect
// to, because PUBLIC may let it create large objects or emit WAL messages
// there, and the warning stops once the connection is revoked. The revokes are
// per database, so this warning is what names where the script runs next.
func TestPerm_INT_PERM_015_ScriptWarnsAboutOtherDatabases(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		t.Cleanup(func() { dropScriptRoles(t, admin) })
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS perm_script_other")
		require.NoError(t, err)
		// CREATE DATABASE cannot run in a transaction block, so it is sent alone.
		// A new database grants CONNECT to PUBLIC, so the role can connect to it.
		_, err = admin.Exec(ctx, "CREATE DATABASE perm_script_other")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS perm_script_other")
			require.NoError(t, err)
		})
		warning := scriptRole + " can connect to perm_script_other, where PUBLIC may still let it create large objects or emit WAL messages"

		code, out := runMonitoringScript(t, pg)
		require.Equal(t, 0, code, out)
		require.Contains(t, out, warning)

		_, err = admin.Exec(ctx, "REVOKE CONNECT ON DATABASE perm_script_other FROM PUBLIC")
		require.NoError(t, err)
		code, out = runMonitoringScript(t, pg)
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, "can connect to perm_script_other")
	})
}

// INT-PERM-010: the script runs as a superuser in a database another role
// may own. A schema of that role listed first in the database's search_path
// cannot put its own functions in place of the built-ins the script calls,
// nor its own = and <> operators in place of the comparisons of the password
// and the tier flags, which run before the script pins its search_path.
func TestPerm_INT_PERM_010_ScriptIgnoresDatabaseSearchPath(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		t.Cleanup(func() { regrantWALEmitters(t, admin) })
		t.Cleanup(func() {
			_, err := admin.Exec(context.Background(), `ALTER DATABASE app RESET search_path;
DROP SCHEMA IF EXISTS perm_hijack CASCADE`)
			require.NoError(t, err)
			dropScriptRoles(t, admin)
		})
		// The operators record what the script compares, and answer as the
		// built-ins would, only in the psql session the script runs in. Every
		// other session of the database, this test's included, sees the
		// built-in answers.
		_, err := admin.Exec(ctx, `CREATE SCHEMA perm_hijack;
CREATE FUNCTION perm_hijack.current_setting(text) RETURNS text LANGUAGE plpgsql
  AS $$BEGIN RAISE EXCEPTION 'hijacked'; END$$;
CREATE FUNCTION perm_hijack.quote_ident(text) RETURNS text LANGUAGE plpgsql
  AS $$BEGIN RAISE EXCEPTION 'hijacked'; END$$;
CREATE TABLE perm_hijack.seen (call text);
CREATE FUNCTION perm_hijack.hijacked_eq(text, text) RETURNS boolean LANGUAGE plpgsql
  AS $$BEGIN
    IF pg_catalog.current_setting('application_name') OPERATOR(pg_catalog.=) 'psql' THEN
      INSERT INTO perm_hijack.seen VALUES (pg_catalog.concat_ws(' ', '=', $1, $2));
      RETURN true;
    END IF;
    RETURN $1 OPERATOR(pg_catalog.=) $2;
  END$$;
CREATE FUNCTION perm_hijack.hijacked_ne(text, text) RETURNS boolean LANGUAGE plpgsql
  AS $$BEGIN
    IF pg_catalog.current_setting('application_name') OPERATOR(pg_catalog.=) 'psql' THEN
      INSERT INTO perm_hijack.seen VALUES (pg_catalog.concat_ws(' ', '<>', $1, $2));
      RETURN true;
    END IF;
    RETURN $1 OPERATOR(pg_catalog.<>) $2;
  END$$;
CREATE OPERATOR perm_hijack.= (LEFTARG = text, RIGHTARG = text, PROCEDURE = perm_hijack.hijacked_eq);
CREATE OPERATOR perm_hijack.<> (LEFTARG = text, RIGHTARG = text, PROCEDURE = perm_hijack.hijacked_ne);
ALTER DATABASE app SET search_path = perm_hijack, pg_catalog, public`)
		require.NoError(t, err)

		code, out := runMonitoringScript(t, pg, "tier1=1", "harden_public=1")
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, "hijacked")
		var calls int
		require.NoError(t, admin.QueryRow(ctx, "SELECT count(*) FROM perm_hijack.seen").Scan(&calls))
		require.Zero(t, calls, "the script compared values through the database owner's operators")
	})
}

// scriptTierGrants reports whether the script role holds the tier 1 read
// privilege and the tier 2 signal privilege.
func scriptTierGrants(t *testing.T, admin *pgxpool.Pool) (readAll, signal bool) {
	t.Helper()
	require.NoError(t, admin.QueryRow(context.Background(), `SELECT pg_has_role($1::name, 'pg_read_all_data', 'MEMBER'),
       pg_has_role($1::name, 'pg_signal_backend', 'MEMBER')`, scriptRole).Scan(&readAll, &signal))
	return readAll, signal
}

// INT-PERM-011: the script grants a tier only for the value 1. tier1=0 and
// tier1=false grant nothing, and tier2=1 without tier1=1 is refused before
// the script changes anything.
func TestPerm_INT_PERM_011_ScriptTierFlagsNeedValueOne(t *testing.T) {
	ForEach(t, func(t *testing.T, pg *PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", RoleSuperuser)
		dropScriptRoles(t, admin)
		t.Cleanup(func() { dropScriptRoles(t, admin) })
		t.Cleanup(func() { regrantLargeObjects(t, admin) })
		t.Cleanup(func() { regrantWALEmitters(t, admin) })

		for _, flags := range [][]string{{"tier1=0"}, {"tier1=false", "tier2=0"}} {
			dropScriptRoles(t, admin)
			code, out := runMonitoringScript(t, pg, flags...)
			require.Equal(t, 0, code, "%v: %s", flags, out)
			readAll, signal := scriptTierGrants(t, admin)
			require.False(t, readAll, "%v", flags)
			require.False(t, signal, "%v", flags)
		}

		dropScriptRoles(t, admin)
		code, out := runMonitoringScript(t, pg, "tier1=1")
		require.Equal(t, 0, code, out)
		readAll, signal := scriptTierGrants(t, admin)
		require.True(t, readAll)
		require.False(t, signal)

		dropScriptRoles(t, admin)
		code, out = runMonitoringScript(t, pg, "tier2=1")
		require.Equal(t, 3, code, out)
		require.Contains(t, out, "tier2=1 needs tier1=1")
		var exists bool
		require.NoError(t, admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", scriptRole).Scan(&exists))
		require.False(t, exists, "a refused run must not create the role")
	})
}

// regrantLargeObjects gives PUBLIC back the large-object creators that a run
// of the script without harden_public revokes.
func regrantLargeObjects(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	_, err := admin.Exec(context.Background(), `GRANT EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer),
  pg_catalog.lo_from_bytea(oid, bytea) TO PUBLIC`)
	require.NoError(t, err)
}

// regrantWALEmitters gives PUBLIC back EXECUTE on every pg_logical_emit_message
// overload that a run of the script revokes. The overloads differ by
// PostgreSQL major (19 adds an argument), so the list comes from the catalog.
func regrantWALEmitters(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	_, err := admin.Exec(context.Background(), `DO $$
DECLARE emitter text;
BEGIN
  FOR emitter IN SELECT oid::regprocedure::text FROM pg_catalog.pg_proc
                 WHERE proname = 'pg_logical_emit_message'
                   AND pronamespace = 'pg_catalog'::regnamespace LOOP
    EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO PUBLIC', emitter);
  END LOOP;
END $$`)
	require.NoError(t, err)
}
