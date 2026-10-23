//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// victimsRunning counts the sessions the view-based tests end, while they run.
func victimsRunning(t *testing.T, admin *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, admin.QueryRow(context.Background(),
		"SELECT count(*) FROM pg_stat_activity WHERE application_name = 'pglens-guard-victim' AND state = 'active'").Scan(&n))
	return n
}

// startVictim opens a session of the monitoring role that sleeps under the
// name the view-based tests look for. The session is ended when the test
// finishes, so the goroutine that runs its sleep does not outlive the test.
func startVictim(t *testing.T, pg *pgtest.PG) {
	t.Helper()
	cfg, err := pgx.ParseConfig(pg.DSN("postgres", pgtest.RoleT1))
	require.NoError(t, err)
	cfg.RuntimeParams["application_name"] = "pglens-guard-victim"
	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = conn.Exec(ctx, "SELECT pg_sleep(30)")
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = conn.Close(context.Background())
	})
}

// listenFor listens on channel as the superuser and returns a function that
// reports how many notifications have arrived. Each call runs a statement,
// which is what lets the driver deliver the ones waiting.
func listenFor(t *testing.T, pg *pgtest.PG, channel string) func() int {
	t.Helper()
	ctx := context.Background()
	received := make(chan struct{}, 64)
	cfg, err := pgx.ParseConfig(pg.DSN("postgres", pgtest.RoleSuperuser))
	require.NoError(t, err)
	cfg.OnNotification = func(*pgconn.PgConn, *pgconn.Notification) { received <- struct{}{} }
	conn, err := pgx.ConnectConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	_, err = conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize())
	require.NoError(t, err)
	return func() int {
		_, err := conn.Exec(ctx, "SELECT 1")
		require.NoError(t, err)
		return len(received)
	}
}

// TestINTCMD024_ExplainAnalyzeRefusesEffectsBehindViews: effects a read-only
// transaction does not stop, reached through a view, so that the statement
// names no function. ANALYZE refuses each of them for tier 1 and tier 2, and
// none of them happens. Registering a statement runs it as the superuser, so
// each case also shows that the effect exists in this build.
func TestINTCMD024_ExplainAnalyzeRefusesEffectsBehindViews(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, guardFixture)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS pglens_guard CASCADE")
			_, _ = admin.Exec(context.Background(), "DROP EXTENSION IF EXISTS dblink")
		})

		// Tier 1 may end its own sessions and tier 2 may end this role's
		// sessions as foreign ones. The victims start after the statement is
		// registered, which would otherwise end them.
		_, err = admin.Exec(ctx, `CREATE VIEW pglens_guard.v_kill AS
	SELECT pg_terminate_backend(pid) AS killed FROM pg_stat_activity WHERE application_name = 'pglens-guard-victim'`)
		require.NoError(t, err)
		const kill = "SELECT * FROM pglens_guard.v_kill"
		killID := registerStatements(t, pg, kill)[kill]
		startVictim(t, pg)
		startVictim(t, pg)
		require.Eventually(t, func() bool { return victimsRunning(t, admin) == 2 }, 5*time.Second, 100*time.Millisecond)
		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, killID, true)
			requireRefused(t, err, "pg_terminate_backend()")
			require.Equal(t, 2, victimsRunning(t, admin), role.String())
		}
		// Plan-only EXPLAIN plans the view without calling its volatile function.
		_, err = explainAs(t, pg, pgtest.RoleT1, killID, false)
		require.NoError(t, err)
		require.Equal(t, 2, victimsRunning(t, admin))

		// pg_notify returns void, so the view counts its calls. It reaches the
		// listener only when it is called.
		notifications := listenFor(t, pg, "pglens_guard")
		_, err = admin.Exec(ctx, "CREATE VIEW pglens_guard.v_notify AS SELECT count(*) AS notified FROM (SELECT pg_notify('pglens_guard', 'explain')) AS s")
		require.NoError(t, err)
		const notify = "SELECT * FROM pglens_guard.v_notify"
		notifyID := registerStatements(t, pg, notify)[notify]
		require.Equal(t, 1, notifications())
		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, notifyID, true)
			requireRefused(t, err, "pg_notify()")
			require.Equal(t, 1, notifications(), role.String())
		}

		// Statistics reset. pg_stat_reset is superuser-only, so the monitoring
		// roles cannot even plan it: the refusal names it either way, and the
		// reset time shows that no reset happened.
		statsReset := func() string {
			var at string
			require.NoError(t, admin.QueryRow(ctx, "SELECT COALESCE(stats_reset::text, '') FROM pg_stat_database WHERE datname = current_database()").Scan(&at))
			return at
		}
		_, err = admin.Exec(ctx, "CREATE VIEW pglens_guard.v_reset AS SELECT count(*) AS reset FROM (SELECT pg_stat_reset()) AS s")
		require.NoError(t, err)
		const reset = "SELECT * FROM pglens_guard.v_reset"
		before := statsReset()
		resetID := registerStatements(t, pg, reset)[reset]
		require.NotEqual(t, before, statsReset())
		before = statsReset()
		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, resetID, true)
			requireRefused(t, err, "pg_stat_reset")
			require.Equal(t, before, statsReset(), role.String())
		}

		// The effect is a WAL message that a read-only transaction cannot roll
		// back. The cluster writes WAL of its own (running-transaction records,
		// checkpoints) while the test runs, so an insert-LSN delta cannot tell
		// the refused statement from that. pg_walinspect reads the records
		// between two LSNs, and the test counts only the messages with the
		// guard's prefix. pg_walinspect reads only flushed WAL, and under
		// synchronous_commit=off the flush of a non-transactional message can
		// wait for the WAL writer. A mark therefore commits a transactional
		// message with synchronous_commit on: that commit flushes all the WAL
		// before it, and the insert LSN read first lies before the commit.
		_, err = admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_walinspect")
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP EXTENSION IF EXISTS pg_walinspect") })
		walMark := func() string {
			var lsn string
			require.NoError(t, admin.QueryRow(ctx, "SELECT pg_catalog.pg_current_wal_insert_lsn()::text").Scan(&lsn))
			tx, err := admin.Begin(ctx)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "SET LOCAL synchronous_commit = on")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "SELECT pg_catalog.pg_logical_emit_message(true, 'pglens-mark', '')")
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			return lsn
		}
		// guardMessages counts the messages with the guard's prefix written
		// after from, and returns the mark that ends the count.
		guardMessages := func(from string) (int, string) {
			to := walMark()
			var n int
			require.NoError(t, admin.QueryRow(ctx, `SELECT pg_catalog.count(*) FROM pg_get_wal_records_info($1::pg_catalog.pg_lsn, $2::pg_catalog.pg_lsn)
				WHERE resource_manager = 'LogicalMessage' AND description LIKE '%prefix "pglens-guard";%'`, from, to).Scan(&n))
			return n, to
		}
		_, err = admin.Exec(ctx, "CREATE VIEW pglens_guard.v_message AS SELECT pg_logical_emit_message(false, 'pglens-guard', 'x') AS emitted")
		require.NoError(t, err)
		const message = "SELECT * FROM pglens_guard.v_message"
		mark := walMark()
		messageID := registerStatements(t, pg, message)[message]
		// The positive control: the registration wrote the message.
		var written int
		written, mark = guardMessages(mark)
		require.Equal(t, 1, written)
		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, messageID, true)
			requireRefused(t, err, "pg_logical_emit_message()")
			var n int
			n, mark = guardMessages(mark)
			require.Equal(t, 0, n, role.String())
		}

		// dblink opens its own session, so the write it makes is not undone by
		// the read-only transaction. The registration inserts a row through it.
		_, err = admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS dblink")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, `CREATE VIEW pglens_guard.v_remote AS
	SELECT dblink_exec('host=127.0.0.1 port=5432 dbname=postgres user=postgres password=test',
		'INSERT INTO pglens_guard.t VALUES (3, ''remote'')') AS remote_result`)
		require.NoError(t, err)
		const remote = "SELECT * FROM pglens_guard.v_remote"
		remoteID := registerStatements(t, pg, remote)[remote]
		rows := func() int {
			var n int
			require.NoError(t, admin.QueryRow(ctx, "SELECT count(*) FROM pglens_guard.t").Scan(&n))
			return n
		}
		require.Equal(t, 3, rows())
		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, remoteID, true)
			requireRefused(t, err, "dblink_exec()")
			require.Equal(t, 3, rows(), role.String())
		}
		_, err = explainAs(t, pg, pgtest.RoleT1, remoteID, false)
		require.NoError(t, err)
		require.Equal(t, 3, rows())
	})
}

// TestINTCMD025_ExplainAnalyzeRefusesForeignScans proves that EXPLAIN ANALYZE
// refuses a foreign scan. postgres_fdw opens its own session on the remote
// side, which a read-only transaction does not cover, so the view reads a
// remote view that calls a VOLATILE function which writes.
func TestINTCMD025_ExplainAnalyzeRefusesForeignScans(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, guardFixture)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS pglens_guard CASCADE")
			_, _ = admin.Exec(context.Background(), "DROP SERVER IF EXISTS pglens_loopback CASCADE")
			_, _ = admin.Exec(context.Background(), "DROP EXTENSION IF EXISTS postgres_fdw")
		})

		// The loopback server is this database, reached over TCP as the
		// superuser, the same way the dblink case reaches it.
		_, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS postgres_fdw;
CREATE SERVER pglens_loopback FOREIGN DATA WRAPPER postgres_fdw OPTIONS (host '127.0.0.1', port '5432', dbname 'postgres');
CREATE USER MAPPING FOR PUBLIC SERVER pglens_loopback OPTIONS (user 'postgres', password 'test');
GRANT USAGE ON FOREIGN SERVER pglens_loopback TO PUBLIC;
CREATE FUNCTION pglens_guard.write_remote() RETURNS int LANGUAGE plpgsql VOLATILE
	AS $$ BEGIN INSERT INTO pglens_guard.t VALUES (4, 'remote'); RETURN 1; END $$;
CREATE VIEW pglens_guard.remote_writer AS SELECT count(*) AS n FROM (SELECT pglens_guard.write_remote()) AS s;
CREATE FOREIGN TABLE pglens_guard.f_writer (n bigint) SERVER pglens_loopback OPTIONS (schema_name 'pglens_guard', table_name 'remote_writer');
CREATE VIEW pglens_guard.v_fdw AS SELECT n FROM pglens_guard.f_writer`)
		require.NoError(t, err)
		rows := func() int {
			var n int
			require.NoError(t, admin.QueryRow(ctx, "SELECT count(*) FROM pglens_guard.t").Scan(&n))
			return n
		}

		// Registration runs the view once as the superuser, so the remote
		// write happens. That shows the path is live: a refusal below means
		// that nothing ran.
		const fdw = "SELECT * FROM pglens_guard.v_fdw"
		before := rows()
		fdwID := registerStatements(t, pg, fdw)[fdw]
		require.Equal(t, before+1, rows())
		afterRegistration := rows()

		for _, role := range []pgtest.Role{pgtest.RoleT1, pgtest.RoleT2} {
			_, err = explainAs(t, pg, role, fdwID, true)
			requireRefused(t, err, "Foreign Scan")
			require.Equal(t, afterRegistration, rows(), role.String())
		}
		// Plan-only EXPLAIN plans the foreign scan without running it.
		_, err = explainAs(t, pg, pgtest.RoleT1, fdwID, false)
		require.NoError(t, err)
		require.Equal(t, afterRegistration, rows())
	})
}
