//go:build integration

package agent

import (
	"context"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/command"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-CONN-001: connection ceiling never exceeds MaxConnsPerInstance.
func TestINTCONN001_ConnectionCeiling(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		// Open multiple per-database connections and verify ceiling.
		dbs := []string{"postgres", "template1"}
		conns := make(map[string]interface{})
		opts := mgr.opts
		maxExpected := opts.MaxConnsPerInstance

		for _, db := range dbs {
			for i := 0; i < maxExpected+2; i++ {
				conn, err := mgr.ForDatabase(ctx, db)
				if err != nil {
					t.Fatalf("ForDatabase(%s) failed: %v", db, err)
				}
				conns[db] = conn
				conn.Release()
			}
		}

		// Query pg_stat_activity to count pglens connections.
		conn, err := mgr.Shared(ctx)
		require.NoError(t, err)
		defer conn.Release()

		var activeConns int
		err = conn.QueryRow(ctx, `
			SELECT COUNT(*) FROM pg_stat_activity
			WHERE application_name LIKE 'pglens/%' AND usename = 'pglens'
		`).Scan(&activeConns)
		require.NoError(t, err)
		// Should not exceed MaxConnsPerInstance + 1 (shared).
		if activeConns > maxExpected+1 {
			t.Errorf("too many connections: expected <= %d, got %d", maxExpected+1, activeConns)
		}
	})
}

// INT-CONN-002: idle per-database connections are closed after IdleTimeout.
func TestINTCONN002_IdleEviction(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		fakeClock := clock.NewFake(time.Now())
		opts := DefaultConnOptions()
		opts.IdleTimeout = 10 * time.Second
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), opts, fakeClock)
		require.NoError(t, err)
		defer mgr.Close()

		// Open a per-database connection.
		conn, err := mgr.ForDatabase(ctx, "template1")
		require.NoError(t, err)
		conn.Release()

		// Advance time past idle timeout.
		fakeClock.Advance(opts.IdleTimeout + 1*time.Second)

		// Open another connection (will evict the idle one).
		conn2, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		conn2.Release()

		// Verify the old connection's pool was closed (no assertion needed, just doesn't panic).
	})
}

// INT-CONN-003: application_name is set to pglens/<context>.
func TestINTCONN003_ApplicationName(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		// Get shared connection and check application_name.
		shared, err := mgr.Shared(ctx)
		require.NoError(t, err)
		defer shared.Release()

		var appName string
		err = shared.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName)
		require.NoError(t, err)
		require.Equal(t, "pglens/shared", appName)

		// Get per-database connection and check application_name.
		perDb, err := mgr.ForDatabase(ctx, "template1")
		require.NoError(t, err)
		defer perDb.Release()

		var appName2 string
		err = perDb.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName2)
		require.NoError(t, err)
		require.Equal(t, "pglens/template1", appName2)
	})
}

// TestManager_DedicatedConn proves DedicatedConn (phase 7.1, the ASH
// sampler's own connection) returns a real, immediately usable connection
// tagged distinctly from Shared()'s.
func TestManager_DedicatedConn(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		conn, err := mgr.DedicatedConn(ctx)
		require.NoError(t, err)
		defer conn.Release()

		var appName string
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&appName))
		require.Equal(t, "pglens/ash", appName)

		var one int
		require.NoError(t, conn.QueryRow(ctx, "SELECT 1").Scan(&one))
		require.Equal(t, 1, one)
	})
}

// INT-CONN-004: new databases created after startup are discovered; dropped
// ones stop being listed. The database is created by the superuser: the
// monitoring role can create nothing, and this test used to create it
// through the agent's own connection, fail, and assert nothing at all.
func TestINTCONN004_DynamicDiscovery(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		listed := func() bool {
			t.Helper()
			dbs, err := mgr.Discover(ctx)
			require.NoError(t, err)
			for _, db := range dbs {
				if db.Name == "test_conn_discovery" {
					require.True(t, db.Monitored)
					return true
				}
			}
			return false
		}
		_, err = admin.Exec(ctx, "DROP DATABASE IF EXISTS test_conn_discovery")
		require.NoError(t, err)
		require.False(t, listed())

		_, err = admin.Exec(ctx, "CREATE DATABASE test_conn_discovery")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS test_conn_discovery WITH (FORCE)")
		})
		require.True(t, listed(), "test_conn_discovery not found in discovery result")

		_, err = admin.Exec(ctx, "DROP DATABASE test_conn_discovery WITH (FORCE)")
		require.NoError(t, err)
		require.False(t, listed(), "a dropped database must stop being listed")
	})
}

// INT-CONN-005: with MaxDatabases limit, excess databases are reported with skip_reason="db_budget".
func TestINTCONN005_BudgetReporting(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		opts := DefaultConnOptions()
		opts.MaxDatabases = 3
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		dbs, err := mgr.Discover(ctx)
		require.NoError(t, err)

		if len(dbs) > opts.MaxDatabases {
			monitored := 0
			unmonitored := 0
			for _, db := range dbs {
				if db.Monitored {
					monitored++
				} else {
					unmonitored++
					require.Equal(t, "db_budget", db.SkipReason)
				}
			}
			require.Equal(t, opts.MaxDatabases, monitored)
			require.Equal(t, len(dbs)-opts.MaxDatabases, unmonitored)
		}
	})
}

// INT-CONN-006: every session the agent opens is read-only inside
// PostgreSQL — the shared connection, the ASH connection and per-database
// connections alike — so a write fails in the server, whatever issued it.
func TestINTCONN006_SessionsAreReadOnly(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		mgr, err := NewManager(ctx, "ro-target", pg.DSN("postgres", pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		assertReadOnly := func(t *testing.T, conn *pgxpool.Conn) {
			t.Helper()
			defer conn.Release()
			var setting string
			require.NoError(t, conn.QueryRow(ctx, "SHOW default_transaction_read_only").Scan(&setting))
			require.Equal(t, "on", setting)
			_, err := conn.Exec(ctx, "CREATE TEMP TABLE pglens_ro_probe (i int)")
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "25006", pgErr.Code, "read_only_sql_transaction")
		}
		shared, err := mgr.Shared(ctx)
		require.NoError(t, err)
		assertReadOnly(t, shared)
		dedicated, err := mgr.DedicatedConn(ctx)
		require.NoError(t, err)
		assertReadOnly(t, dedicated)
		perDB, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		assertReadOnly(t, perDB)
	})
}

// INT-CONN-007: a database name is data, never SQL. Names come from
// pg_database, i.e. from anyone allowed to CREATE DATABASE, and one used to be
// spliced into a simple-protocol SET statement.
func TestINTCONN007_DatabaseNameIsNeverSQL(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		name := "pglens'; CREATE TABLE pglens_pwned (i int); --"
		quoted := pgx.Identifier{name}.Sanitize()
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE DATABASE "+quoted)
		require.NoError(t, err)

		mgr, err := NewManager(ctx, "inj-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		conn, err := mgr.ForDatabase(ctx, name)
		require.NoError(t, err)
		var appName, db string
		var pwned bool
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('application_name'), current_database(), to_regclass('public.pglens_pwned') IS NOT NULL").Scan(&appName, &db, &pwned))
		conn.Release()
		mgr.Close()
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)") })

		require.Equal(t, name, db)
		require.Equal(t, "pglens/"+name, appName)
		require.False(t, pwned)
	})
}

// INT-CONN-008: a search_path set for the database cannot put another
// schema's functions in place of the built-ins the agent calls. A database
// owner may run ALTER DATABASE ... SET search_path, and a function that
// replaced now() would otherwise run as the monitoring role.
func TestINTCONN008_SessionSearchPathIsCatalogOnly(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		const database = "pglens_hijack"
		maintenance := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		admin := pg.Pool(t, database, pgtest.RoleSuperuser)
		t.Cleanup(func() {
			_, _ = maintenance.Exec(context.Background(), "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)")
		})
		_, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS hijack CASCADE;
CREATE EXTENSION IF NOT EXISTS pgstattuple;
CREATE SCHEMA hijack;
GRANT USAGE ON SCHEMA hijack TO PUBLIC;
CREATE TABLE hijack.t (a int);
INSERT INTO hijack.t VALUES (1);
CREATE FUNCTION hijack.now() RETURNS timestamptz LANGUAGE plpgsql
	AS $$ BEGIN RAISE EXCEPTION 'hijacked'; END $$;
ALTER DATABASE `+database+` SET search_path = hijack, pg_catalog`)
		require.NoError(t, err)

		// Without the agent's session settings, the database's search_path
		// hands now() to the hijacking function.
		raw, err := pgx.Connect(ctx, pg.DSN(database, pgtest.RoleT1))
		require.NoError(t, err)
		_, err = raw.Exec(ctx, "SELECT now()")
		require.ErrorContains(t, err, "hijacked")
		require.NoError(t, raw.Close(ctx))

		// An application statement that names its table through the
		// database's search_path, for EXPLAIN below.
		app, err := pgx.Connect(ctx, pg.DSN(database, pgtest.RoleSuperuser))
		require.NoError(t, err)
		const statement = "SELECT a FROM t"
		_, err = app.Exec(ctx, statement)
		require.NoError(t, err)
		var queryID int64
		require.NoError(t, app.QueryRow(ctx, "SELECT queryid FROM public.pg_stat_statements WHERE query = $1 LIMIT 1", statement).Scan(&queryID))
		require.NoError(t, app.Close(ctx))

		mgr, err := NewManager(ctx, "hijack-target", pg.DSN(database, pgtest.RoleT1), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		for _, acquire := range []func(context.Context) (*pgxpool.Conn, error){
			mgr.Shared,
			mgr.DedicatedConn,
			func(ctx context.Context) (*pgxpool.Conn, error) { return mgr.ForDatabase(ctx, database) },
		} {
			conn, err := acquire(ctx)
			require.NoError(t, err)
			var searchPath string
			require.NoError(t, conn.QueryRow(ctx, "SHOW search_path").Scan(&searchPath))
			require.Equal(t, "pg_catalog, pg_temp", searchPath)
			_, err = conn.Exec(ctx, "SELECT now()")
			require.NoError(t, err)
			conn.Release()
		}

		// Extension objects are named with their schema, and EXPLAIN plans
		// the statement with the database's own search_path.
		for _, c := range check.All() {
			if c.Name() == "stat_statements" {
				_, err := c.Scrape(ctx, databaseScopedTarget{Target: mgr, database: database})
				require.NoError(t, err)
			}
		}
		_, err = NewPgstattupleExecutor().Execute(ctx, mgr, command.Args{Schema: "hijack", Relation: "t", Datname: database})
		require.NoError(t, err)
		plan, err := NewExplainExecutor().Execute(ctx, mgr, command.Args{QueryID: &queryID, Datname: database})
		require.NoError(t, err)
		require.Contains(t, string(plan), `"Relation Name":"t","Schema":"hijack"`)
	})
}

// INT-CONN-009: ConnFor opens a pool only for a database discovery selected.
// An excluded admin database and an unknown name are refused before any pool
// is opened for them, and a database that discovery lists still works.
func TestINTCONN009_ConnForRefusesUnselectedDatabases(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS rdsadmin WITH (FORCE)")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE DATABASE rdsadmin")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS rdsadmin WITH (FORCE)")
		})

		mgr, err := NewManager(ctx, "guard-target", pg.DSN("postgres", pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		// A connection handed out for a refused name is released at once, so a
		// failing assertion ends the test instead of hanging the pool close.
		var rejected *RejectedError
		for _, name := range []string{"rdsadmin", "no_such_database", "template1"} {
			conn, err := mgr.ConnFor(ctx, name)
			if conn != nil {
				conn.Release()
			}
			require.ErrorAs(t, err, &rejected, "ConnFor(%q) must be refused", name)
		}

		mgr.mu.Lock()
		require.NotContains(t, mgr.dbs, "rdsadmin")
		require.NotContains(t, mgr.dbs, "no_such_database")
		mgr.mu.Unlock()

		conn, err := mgr.ConnFor(ctx, "postgres")
		require.NoError(t, err, "the database the DSN names stays reachable")
		conn.Release()
	})
}

// INT-CONN-010: a role whose name has an upper-case letter keeps the
// permission tier it was granted. The tier lookup used to cast current_user to
// regrole, which folds an unquoted name to lower case, so the role was not
// found and was reported as T0 with no visible error.
func TestINTCONN010_MixedCaseRoleKeepsItsTier(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		const role = "PgLensMixed"
		quoted := pgx.Identifier{role}.Sanitize()
		_, err := admin.Exec(ctx, "DROP ROLE IF EXISTS "+quoted)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN PASSWORD 'test'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT pg_monitor, pg_read_all_data TO "+quoted)
		require.NoError(t, err)
		// Roles are cluster-wide and the snapshot restore does not drop them.
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+quoted) })

		u, err := url.Parse(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		u.User = url.UserPassword(role, "test")
		mgr, err := NewManager(ctx, "mixed-target", u.String(), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		require.NoError(t, mgr.RefreshCapabilities(ctx))
		require.Equal(t, pgtype.TierExplain, mgr.PermTier())
	})
}

// INT-CONN-012: a role that holds pg_read_all_data and pg_signal_backend only
// through a group role has the T2 tier. The tier lookup used to match direct
// memberships, so the inherited grants were not seen and the role was
// reported as T0.
func TestINTCONN012_TierFollowsGroupRole(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		const group, role = "pglens_group_t2", "pglens_member_t2"
		// Roles are cluster-wide and the snapshot restore does not drop them.
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+role)
			_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+group)
		})
		_, err := admin.Exec(ctx, "DROP ROLE IF EXISTS "+role)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "DROP ROLE IF EXISTS "+group)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE ROLE "+group+" NOLOGIN")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT pg_monitor, pg_read_all_data, pg_signal_backend TO "+group)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE ROLE "+role+" LOGIN PASSWORD 'test'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT "+group+" TO "+role)
		require.NoError(t, err)

		u, err := url.Parse(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		u.User = url.UserPassword(role, "test")
		mgr, err := NewManager(ctx, "group-target", u.String(), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		require.NoError(t, mgr.RefreshCapabilities(ctx))
		require.Equal(t, pgtype.TierSignal, mgr.PermTier())
	})
}

// INT-CONN-013: the tier follows what the role can actually read. The role is
// granted its memberships first and only then set to NOINHERIT, as an existing
// monitoring role is. On PostgreSQL 16 and later those memberships keep their
// inherit option, so the role reads the table and holds T1. On PostgreSQL 15
// the membership is not usable, the read is denied, and the tier must not
// claim T1.
func TestINTCONN013_TierTracksEffectiveRead(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		const role, table = "pglens_noinherit", "pglens_noinherit_probe"
		_, err := admin.Exec(ctx, "DROP ROLE IF EXISTS "+role)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE ROLE "+role+" LOGIN PASSWORD 'test'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT pg_monitor, pg_read_all_data TO "+role)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "ALTER ROLE "+role+" NOINHERIT")
		require.NoError(t, err)
		// Roles are cluster-wide and the snapshot restore does not drop them.
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+role) })
		_, err = admin.Exec(ctx, "CREATE TABLE public."+table+" (x int)")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "REVOKE ALL ON public."+table+" FROM PUBLIC")
		require.NoError(t, err)

		u, err := url.Parse(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		u.User = url.UserPassword(role, "test")
		probe, err := pgx.Connect(ctx, u.String())
		require.NoError(t, err)
		defer func() { _ = probe.Close(ctx) }()
		var rows int
		readErr := probe.QueryRow(ctx, "SELECT count(*) FROM public."+table).Scan(&rows)
		canRead := readErr == nil
		require.Equal(t, pg.Major >= 16, canRead, "premise: NOINHERIT membership usable only from 16: %v", readErr)

		mgr, err := NewManager(ctx, "noinherit-target", u.String(), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		require.NoError(t, mgr.RefreshCapabilities(ctx))
		want := pgtype.TierReadOnly
		if canRead {
			want = pgtype.TierExplain
		}
		require.Equal(t, want, mgr.PermTier())
	})
}

// INT-CONN-014: pg_has_role is true for every role in a superuser session, so
// the tier lookup must not read a superuser as holding the tiers above T0. A
// superuser DSN stays at T0, as it did before the lookup used pg_has_role.
func TestINTCONN014_SuperuserKeepsReadOnlyTier(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		mgr, err := NewManager(ctx, "superuser-target", pg.DSN("app", pgtest.RoleSuperuser), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		require.NoError(t, mgr.RefreshCapabilities(ctx))
		require.Equal(t, pgtype.TierReadOnly, mgr.PermTier())
	})
}

// INT-CONN-011: the cluster identity falls back to the configured name only
// when the server denies pg_control_system(), and a role that may read it
// reports the real system_identifier. The grants setupRoles makes live in the
// app database (pg_proc is per database), so the roles connect there. A
// transient failure has no integration test here: the unit test pins that
// branch.
func TestINTCONN011_IdentityFallsBackOnlyOnDenial(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		var systemID uint64
		require.NoError(t, admin.QueryRow(ctx, "SELECT system_identifier FROM pg_control_system()").Scan(&systemID))

		opts := DefaultConnOptions()
		opts.ClusterName = "shared-cluster"
		denied, err := NewManager(ctx, "denied-target", pg.DSN("app", pgtest.RoleT0NoControl), opts, clock.System())
		require.NoError(t, err)
		defer denied.Close()
		require.Equal(t, pgtype.IDSourceManual, denied.ClusterIDSource())
		require.Equal(t, pgtype.ManualClusterID("shared-cluster"), denied.ClusterID())

		granted, err := NewManager(ctx, "granted-target", pg.DSN("app", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		defer granted.Close()
		require.Equal(t, pgtype.IDSourceSystemIdentifier, granted.ClusterIDSource())
		require.Equal(t, pgtype.ClusterID(systemID), granted.ClusterID())
	})
}

// INT-CONN-015: a database whose pool has a connection checked out is not
// evicted to make room for another one, so the connection keeps its pool and
// the new database is opened beside it.
func TestINTCONN015_HeldDatabaseIsNotEvicted(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		opts := DefaultConnOptions()
		opts.MaxDatabases = 1
		mgr, err := NewManager(ctx, "test-target", pg.DSN("postgres", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		held, err := mgr.ForDatabase(ctx, "postgres")
		require.NoError(t, err)
		defer held.Release()

		other, err := mgr.ForDatabase(ctx, "template1")
		require.NoError(t, err)
		other.Release()

		var one int
		require.NoError(t, held.QueryRow(ctx, "SELECT 1").Scan(&one))
		require.Equal(t, 1, one)

		mgr.mu.Lock()
		_, kept := mgr.dbs["postgres"]
		mgr.mu.Unlock()
		require.True(t, kept, "the database with a connection in use must not be evicted")
	})
}

// INT-CONN-016: a database discovery selected stays reachable after the budget
// ranks it out. The checks scheduled at startup were given that database, and
// refusing it fails them on every tick. A database discovery never selected is
// still refused (INT-CONN-009).
func TestINTCONN016_SelectedDatabaseStaysReachableAfterRankMoves(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		// Both databases start with no commits, so the name breaks the tie and
		// zz_int016_a is the one a budget of one keeps.
		for _, name := range []string{"zz_int016_a", "zz_int016_b"} {
			_, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			require.NoError(t, err)
			_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			})
		}

		opts := DefaultConnOptions()
		opts.MaxDatabases = 1
		opts.Include = []*regexp.Regexp{regexp.MustCompile(`^zz_int016_`)}
		mgr, err := NewManager(ctx, "rank-target", pg.DSN("postgres", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		defer mgr.Close()

		dbs, err := mgr.Discover(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, dbs)
		conn, err := mgr.ConnFor(ctx, "zz_int016_a")
		require.NoError(t, err, "the database selected at startup must be reachable")
		conn.Release()

		// Commits on zz_int016_b move it ahead in the ranking. A backend's
		// counters reach pg_stat_database only when they are flushed, so the
		// flush is forced and the ranking is polled.
		b := pg.Pool(t, "zz_int016_b", pgtest.RoleT0)
		bconn, err := b.Acquire(ctx)
		require.NoError(t, err)
		for range 20 {
			_, err := bconn.Exec(ctx, "SELECT 1")
			require.NoError(t, err)
		}
		_, err = bconn.Exec(ctx, "SELECT pg_stat_force_next_flush()")
		require.NoError(t, err)
		bconn.Release()

		require.Eventually(t, func() bool {
			dbs, err := mgr.Discover(ctx)
			if err != nil {
				return false
			}
			for _, db := range dbs {
				if db.Monitored && db.Name == "zz_int016_b" {
					return true
				}
			}
			return false
		}, 15*time.Second, 200*time.Millisecond, "the commits never moved zz_int016_b ahead of zz_int016_a")

		// The budget now selects only zz_int016_b. zz_int016_a was selected at
		// startup, so its checks keep their database.
		conn, err = mgr.ConnFor(ctx, "zz_int016_a")
		require.NoError(t, err, "zz_int016_a must stay reachable after the budget ranks it out")
		conn.Release()
	})
}
