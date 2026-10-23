//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/test/pgtest"
)

// guardFixture is a schema in the postgres database with the objects the
// EXPLAIN guard must see through: a plpgsql function that takes a session
// advisory lock, and a view that takes one. Its functions are VOLATILE, so
// the planner never runs them; planFixture adds the ones it does run.
const guardFixture = `
DROP SCHEMA IF EXISTS pglens_guard CASCADE;
CREATE SCHEMA pglens_guard;
CREATE TABLE pglens_guard.t (a int, b text);
INSERT INTO pglens_guard.t VALUES (1, 'x'), (2, 'y');
GRANT USAGE ON SCHEMA pglens_guard TO PUBLIC;
CREATE FUNCTION pglens_guard.evil() RETURNS int LANGUAGE plpgsql
	AS $$ BEGIN PERFORM pg_advisory_lock(4242); RETURN 1; END $$;
CREATE VIEW pglens_guard.locker AS SELECT pg_try_advisory_lock(4243) AS locked FROM pglens_guard.t;
`

// planFixture is an IMMUTABLE function with an effect. PostgreSQL folds a call
// to it while planning, before any plan can be inspected, so the database that
// defines it refuses EXPLAIN altogether.
const planFixture = `
CREATE FUNCTION pglens_guard.lock_at_plan_time() RETURNS int LANGUAGE plpgsql IMMUTABLE
	AS $$ BEGIN PERFORM pg_advisory_lock(4244); RETURN 1; END $$;
CREATE VIEW pglens_guard.plan_locker AS SELECT pglens_guard.lock_at_plan_time() AS locked;
`

// registerStatements runs each statement once as the superuser so that
// pg_stat_statements records it, on a connection that is closed afterwards:
// the advisory locks the statements take end with it.
func registerStatements(t *testing.T, pg *pgtest.PG, statements ...string) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, pg.DSN("postgres", pgtest.RoleSuperuser))
	require.NoError(t, err)
	for _, statement := range statements {
		_, err := conn.Exec(ctx, statement)
		require.NoError(t, err, statement)
	}
	queryIDs := make(map[string]int64, len(statements))
	for _, statement := range statements {
		var queryID int64
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT queryid FROM pg_stat_statements WHERE query = $1 ORDER BY calls DESC LIMIT 1", statement).Scan(&queryID), statement)
		queryIDs[statement] = queryID
	}
	require.NoError(t, conn.Close(ctx))
	return queryIDs
}

// requireNoAdvisoryLocks waits for closed sessions to exit, then requires
// that no backend holds an advisory lock.
func requireNoAdvisoryLocks(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	var held int
	require.Eventually(t, func() bool {
		err := admin.QueryRow(context.Background(), "SELECT count(*) FROM pg_locks WHERE locktype = 'advisory'").Scan(&held)
		return err == nil && held == 0
	}, 5*time.Second, 100*time.Millisecond, "advisory locks still held: %d", held)
}

func explainAs(t *testing.T, pg *pgtest.PG, role pgtest.Role, queryID int64, analyze bool) (json.RawMessage, error) {
	t.Helper()
	ctx := context.Background()
	mgr, err := NewManager(ctx, "explain-guard-"+role.String(), pg.DSN("postgres", role), DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	defer mgr.Close()
	return NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: "postgres", Analyze: analyze})
}

func requireRefused(t *testing.T, err error, reason string) {
	t.Helper()
	var rejected *RejectedError
	require.ErrorAs(t, err, &rejected)
	require.Contains(t, rejected.Error(), reason)
}

// TestINTCMD020_ExplainAnalyzeGuard proves that EXPLAIN ANALYZE never runs a
// statement whose effects could outlive its read-only transaction, with the
// real tier 1 and tier 2 roles, and that a statement the guard admits is
// analyzed.
func TestINTCMD020_ExplainAnalyzeGuard(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, guardFixture)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS pglens_guard CASCADE") })

		const (
			callsUserFunction = "SELECT pglens_guard.evil() FROM pglens_guard.t"
			viewTakesLock     = "SELECT * FROM pglens_guard.locker"
			// The statement that motivated the guard: with no constants it
			// reaches ANALYZE, and with pg_signal_backend it would
			// terminate every other client. Registering it terminates no
			// one: pid <> pid holds for no row.
			terminatesOthers = "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pid"
			readsOnly        = "SELECT count(*), max(pg_relation_size(c.oid)) FROM pg_class c"
			readsTable       = "SELECT count(*) FROM pglens_guard.t"
		)
		ids := registerStatements(t, pg, callsUserFunction, viewTakesLock, terminatesOthers, readsOnly, readsTable)
		requireNoAdvisoryLocks(t, admin)

		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, ids[callsUserFunction], true)
			requireRefused(t, err, "pglens_guard.evil()")

			_, err = explainAs(t, pg, role, ids[viewTakesLock], true)
			requireRefused(t, err, "pg_try_advisory_lock()")
			requireNoAdvisoryLocks(t, admin)

			_, err = explainAs(t, pg, role, ids[terminatesOthers], true)
			requireRefused(t, err, "pg_terminate_backend()")

			result, err := explainAs(t, pg, role, ids[readsOnly], true)
			require.NoError(t, err, role.String())
			require.Contains(t, string(result), "Actual Rows")

			result, err = explainAs(t, pg, role, ids[readsTable], true)
			require.NoError(t, err, role.String())
			require.Contains(t, string(result), "Actual Rows")
		}
	})
}

// TestINTCMD021_ExplainRefusesFunctionsThePlannerRuns proves that EXPLAIN,
// plan-only or ANALYZE, is refused in a database that defines an IMMUTABLE
// function with an effect, when the statement calls it directly and when a
// view does. The lock it would take is not taken. The two defences that do not
// depend on the catalog are proved on their own: a tier 2 session plans as
// pg_read_all_data, and a session the command used is closed, not pooled.
func TestINTCMD021_ExplainRefusesFunctionsThePlannerRuns(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, guardFixture+planFixture)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS pglens_guard CASCADE") })

		const (
			direct = "SELECT pglens_guard.lock_at_plan_time()"
			viewed = "SELECT * FROM pglens_guard.plan_locker"
		)
		ids := registerStatements(t, pg, direct, viewed)
		requireNoAdvisoryLocks(t, admin)

		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			for _, statement := range []string{direct, viewed} {
				for _, analyze := range []bool{false, true} {
					_, err = explainAs(t, pg, role, ids[statement], analyze)
					requireRefused(t, err, "the database defines pglens_guard.lock_at_plan_time()")
					requireNoAdvisoryLocks(t, admin)
				}
			}
		}

		// A tier 2 session holds pg_signal_backend. Planning must not run as
		// that role, so the executor drops to pg_read_all_data first.
		for role, want := range map[pgtest.Role]string{pgtest.RoleT1: "pglens_t1", pgtest.RoleT2: "pg_read_all_data"} {
			mgr, err := NewManager(ctx, "explain-guard-session-"+role.String(), pg.DSN("postgres", role), DefaultConnOptions(), clock.System())
			require.NoError(t, err)
			conn, err := mgr.ForDatabase(ctx, "postgres")
			require.NoError(t, err)
			_, err = conn.Exec(ctx, "BEGIN READ ONLY")
			require.NoError(t, err)
			require.NoError(t, dropExplainPrivileges(ctx, conn, pg.Major, false))
			var user string
			require.NoError(t, conn.QueryRow(ctx, "SELECT current_user").Scan(&user))
			require.Equal(t, want, user, role.String())
			_, err = conn.Exec(ctx, "ROLLBACK")
			require.NoError(t, err)
			conn.Release()
			mgr.Close()
		}

		// The session a command used ends with it: an advisory lock taken on
		// it is gone once discardConn has closed the session.
		mgr, err := NewManager(ctx, "explain-guard-discard", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		conn, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		_, err = conn.Exec(ctx, "SELECT pg_advisory_lock(4246)")
		require.NoError(t, err)
		discardConn(ctx, conn)
		requireNoAdvisoryLocks(t, admin)
	})
}

// TestINTCMD022_ExplainAnalyzeRefusesHiddenCalls: an operator backed by a
// user-defined function can run without appearing in a plan (CASE x WHEN y
// compares with it), so ANALYZE refuses a statement that reaches one. The
// function is VOLATILE, so the planner does not run it and the refusal is the
// hidden-call check's.
func TestINTCMD022_ExplainAnalyzeRefusesHiddenCalls(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, guardFixture+`
CREATE FUNCTION pglens_guard.eq(int, int) RETURNS bool LANGUAGE plpgsql
	AS $$ BEGIN PERFORM pg_advisory_lock(4245); RETURN $1 = $2; END $$;
CREATE OPERATOR pglens_guard.=== (LEFTARG = int, RIGHTARG = int, FUNCTION = pglens_guard.eq);`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS pglens_guard CASCADE") })

		const readsTable = "SELECT count(*) FROM pglens_guard.t"
		ids := registerStatements(t, pg, readsTable)
		_, err = explainAs(t, pg, pgtest.RoleT1, ids[readsTable], true)
		requireRefused(t, err, "operator pglens_guard.===(integer,integer) calls pglens_guard.eq()")

		_, err = admin.Exec(ctx, "DROP OPERATOR pglens_guard.=== (int, int)")
		require.NoError(t, err)
		_, err = explainAs(t, pg, pgtest.RoleT1, ids[readsTable], true)
		require.NoError(t, err)
	})
}

// TestINTCMD023_ExplainGuardDenyListExists: a denied built-in that is
// renamed or removed in a new major version would silently stop being
// denied. Every name the guard denies by name must exist.
func TestINTCMD023_ExplainGuardDenyListExists(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		for name := range builtinDenied {
			var exists bool
			require.NoError(t, admin.QueryRow(context.Background(),
				"SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = $1 AND pronamespace = 'pg_catalog'::regnamespace)", name).Scan(&exists))
			require.True(t, exists, name)
		}
	})
}
