//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// takeSkips returns the skip reasons queued for target since the last call,
// keyed by check name. The schedule queues its skips synchronously while it is
// built, so the pending batch is what a rebuild reports.
func takeSkips(mu *sync.Mutex, pending map[string][]wire.Result, target string) map[string]string {
	mu.Lock()
	defer mu.Unlock()
	skips := map[string]string{}
	for _, r := range pending[target] {
		if r.SkipReason != "" {
			skips[r.Check] = r.SkipReason
		}
	}
	delete(pending, target)
	return skips
}

// scheduledDatabasesOf copies the databases a target's database-scoped checks
// are scheduled for.
func scheduledDatabasesOf(mu *sync.Mutex, scheduledDBs map[string]map[string]bool, target string) map[string]bool {
	mu.Lock()
	defer mu.Unlock()
	out := map[string]bool{}
	for name := range scheduledDBs[target] {
		out[name] = true
	}
	return out
}

// TestINTSCH001_DatabaseDiscoveredAfterStartIsScheduled: discovery fails when the
// schedule is first built, so no database-scoped check is scheduled. The next
// reconcile reads the databases again and schedules them, instead of leaving
// those checks off for the life of the process.
func TestINTSCH001_DatabaseDiscoveredAfterStartIsScheduled(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const target = "discover-target"
		mgr, err := agent.NewManager(ctx, target, pg.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)

		// The first reads run on a cancelled context, so Discover fails.
		readCtx, cancelRead := context.WithCancel(ctx)
		cancelRead()
		require.NoError(t, schedules.start(readCtx))
		t.Cleanup(schedules.stop)

		require.Empty(t, scheduledDatabasesOf(&mu, scheduledDBs, target), "no database is scheduled while discovery fails")

		schedules.reconcile()

		require.True(t, scheduledDatabasesOf(&mu, scheduledDBs, target)["postgres"], "the reconcile after discovery succeeds must schedule the databases it finds")
	})
}

// TestINTSCH008_DatabaseCreatedAfterStartIsReachable: the first discovery
// finds the maintenance database only. A database created afterwards is
// scheduled by the next reconcile, and its checks must then reach it through
// ScheduledConnFor instead of being refused for the life of the process.
func TestINTSCH008_DatabaseCreatedAfterStartIsReachable(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		const dbname = "sch007_late_database"
		const target = "late-database-target"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+dbname+" WITH (FORCE)")

		opts := agent.DefaultConnOptions()
		opts.Include = []*regexp.Regexp{regexp.MustCompile("^(postgres|" + dbname + ")$")}
		mgr, err := agent.NewManager(ctx, target, pg.DSN("postgres", pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)

		_, err = admin.Exec(ctx, "CREATE DATABASE "+dbname)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbname+" WITH (FORCE)")
		})
		_, err = mgr.ScheduledConnFor(ctx, dbname)
		require.Error(t, err, "the schedule built at startup does not hold the new database")

		schedules.reconcile()

		require.True(t, scheduledDatabasesOf(&mu, scheduledDBs, target)[dbname], "the reconcile must schedule the new database")
		conn, err := mgr.ScheduledConnFor(ctx, dbname)
		require.NoError(t, err, "the checks scheduled for the new database must reach it")
		var current string
		require.NoError(t, conn.QueryRow(ctx, "SELECT current_database()").Scan(&current))
		require.Equal(t, dbname, current)
		conn.Release()
	})
}

// TestINTSCH002_ScheduleFollowsTheTargetRole: a target that cannot be read when
// the schedule is first built gets its checks once it can be read, and its
// role-gated check follows the role: a standby runs replication_receiver, and
// after a promotion the former standby no longer does. replication_streaming
// runs on every role, since a cascading standby has walsenders of its own.
func TestINTSCH002_ScheduleFollowsTheTargetRole(t *testing.T) {
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			_, standby := pgtest.PrimaryStandby(t, major)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			const target = "standby-target"
			mgr, err := agent.NewManager(ctx, target, standby.DSN("postgres", pgtest.RoleT0), agent.DefaultConnOptions(), clock.System())
			require.NoError(t, err)
			t.Cleanup(mgr.Close)

			// The identity store is opened from PGLENS_IDENTITY_PATH, and a
			// corrupt file makes every cache read fail. The target then looks
			// unreachable for role and version while its server is up.
			dir := t.TempDir()
			corrupt := filepath.Join(dir, "corrupt.json")
			require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o600))
			t.Setenv("PGLENS_IDENTITY_PATH", corrupt)

			var mu sync.Mutex
			pending := map[string][]wire.Result{}
			scheduledDBs := map[string]map[string]bool{}
			schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
			require.NoError(t, schedules.start(ctx))
			t.Cleanup(schedules.stop)

			skips := takeSkips(&mu, pending, target)
			require.Equal(t, "role unknown not allowed", skips["replication_receiver"])
			require.NotContains(t, skips, "replication_streaming")

			// The identity store recovers. The next reconcile reads the standby
			// in full and schedules its receiver check.
			t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(dir, "identity.json"))
			schedules.reconcile()

			skips = takeSkips(&mu, pending, target)
			require.NotContains(t, skips, "replication_receiver")
			require.NotContains(t, skips, "replication_streaming")

			// Promote the standby. The push cycle refreshes the role before the
			// reconcile reads it, so do the same here.
			admin, err := pgxpool.New(ctx, standby.DSN("postgres", pgtest.RoleSuperuser))
			require.NoError(t, err)
			t.Cleanup(admin.Close)
			_, err = admin.Exec(ctx, "SELECT pg_promote()")
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				var recovering bool
				if err := admin.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&recovering); err != nil {
					return false
				}
				return !recovering
			}, 15*time.Second, 200*time.Millisecond, "promotion should complete")

			refreshCtx, cancelRefresh := context.WithTimeout(ctx, 10*time.Second)
			require.NoError(t, mgr.RefreshRole(refreshCtx))
			cancelRefresh()

			schedules.reconcile()

			skips = takeSkips(&mu, pending, target)
			require.Equal(t, "role primary not allowed", skips["replication_receiver"])
			require.NotContains(t, skips, "replication_streaming")
		})
	}
}

// TestINTSCH003_ExtensionInstalledAfterStartIsScheduled: pg_stat_statements is
// not installed when the schedule is first built, so the check is skipped with
// the extension as the reason. Installing it afterwards must reach the schedule
// on the next reconcile, instead of leaving the check off until a restart.
func TestINTSCH003_ExtensionInstalledAfterStartIsScheduled(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// A database of its own, monitored alone, where the extension is absent:
		// the skip then comes from the extension and not from the role or tier.
		const dbname = "sch003_late_extension"
		const target = "late-extension-target"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "CREATE DATABASE "+dbname)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+dbname+" WITH (FORCE)")
		})

		opts := agent.DefaultConnOptions()
		opts.Include = []*regexp.Regexp{regexp.MustCompile("^" + dbname + "$")}
		mgr, err := agent.NewManager(ctx, target, pg.DSN(dbname, pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)

		skips := takeSkips(&mu, pending, target)
		require.Equal(t, "missing extension pg_stat_statements", skips["stat_statements"])

		installer, err := pgxpool.New(ctx, pg.DSN(dbname, pgtest.RoleSuperuser))
		require.NoError(t, err)
		t.Cleanup(installer.Close)
		_, err = installer.Exec(ctx, "CREATE EXTENSION pg_stat_statements")
		require.NoError(t, err)

		schedules.reconcile()

		require.True(t, schedules.gates[target].exts["pg_stat_statements"], "the reconcile must read the extension the check needs")
		skips = takeSkips(&mu, pending, target)
		require.NotContains(t, skips, "stat_statements", "the check must no longer be skipped once the extension is installed")
	})
}

// TestINTSCH004_UncheckableDatabaseKeepsTheStatementsAnswer: pg_stat_statements
// is installed in a monitored database that the role is then locked out of, as
// a DBA does when hardening PUBLIC. The view cannot be looked for there, so its
// absence is not known. The reconcile must keep the schedule it has, instead of
// rebuilding it without the check on every pass.
func TestINTSCH004_UncheckableDatabaseKeepsTheStatementsAnswer(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// The maintenance database has no view; the extension is in a second
		// monitored database.
		const maintDB, extDB = "sch004_maintenance", "sch004_extension"
		const target = "uncheckable-target"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		for _, db := range []string{maintDB, extDB} {
			_, err := admin.Exec(ctx, "CREATE DATABASE "+db)
			require.NoError(t, err)
		}
		t.Cleanup(func() {
			for _, db := range []string{maintDB, extDB} {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
			}
		})
		installer, err := pgxpool.New(ctx, pg.DSN(extDB, pgtest.RoleSuperuser))
		require.NoError(t, err)
		t.Cleanup(installer.Close)
		_, err = installer.Exec(ctx, "CREATE EXTENSION pg_stat_statements")
		require.NoError(t, err)

		opts := agent.DefaultConnOptions()
		opts.Include = []*regexp.Regexp{regexp.MustCompile("^(" + maintDB + "|" + extDB + ")$")}
		mgr, err := agent.NewManager(ctx, target, pg.DSN(maintDB, pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)

		require.True(t, schedules.gates[target].exts["pg_stat_statements"], "the view is found in the second database")
		require.NotContains(t, takeSkips(&mu, pending, target), "stat_statements")

		// Revoke CONNECT from PUBLIC on the extension database and end its
		// sessions: pooled connections opened before the REVOKE would otherwise
		// keep working.
		_, err = admin.Exec(ctx, "REVOKE CONNECT ON DATABASE "+extDB+" FROM PUBLIC")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(context.Background(), "GRANT CONNECT ON DATABASE "+extDB+" TO PUBLIC")
		})
		_, err = admin.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", extDB)
		require.NoError(t, err)

		// The lockout must be real, or the test proves nothing. It is checked
		// through ForDatabase, not LocateStatements, so the precondition does not
		// depend on the function under test.
		conn, err := mgr.ForDatabase(ctx, extDB)
		if err == nil {
			conn.Release()
		}
		require.Error(t, err, "the role must be locked out of the extension database")

		currentScheduler := func() *agent.Scheduler {
			schedules.schedMu.Lock()
			defer schedules.schedMu.Unlock()
			return schedules.current
		}
		before := currentScheduler()
		schedules.reconcile()

		require.Same(t, before, currentScheduler(), "a database that cannot be checked must not rebuild the schedule")
		require.True(t, schedules.gates[target].exts["pg_stat_statements"], "the last answer is kept")
		require.NotContains(t, takeSkips(&mu, pending, target), "stat_statements", "the check must not be skipped as missing")
	})
}

// TestINTSCH005_ScheduledDatabaseOutsideTheBudgetKeepsItsStatementsView: the
// budget ranks databases by commit count and monitors one of them. When another
// database becomes the busier one, the schedule keeps the first, and its
// pg_stat_statements view still counts. The reconcile must look at the databases
// the schedule runs checks for, not only at the ones the budget monitors, or it
// rebuilds the schedule without the check.
func TestINTSCH005_ScheduledDatabaseOutsideTheBudgetKeepsItsStatementsView(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// The view is in viewDB. The role connects to plainDB, which has none, so
		// the view is found only when the schedule still looks at viewDB.
		const viewDB, plainDB = "sch005_view", "sch005_plain"
		const target = "budget-target"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		for _, db := range []string{viewDB, plainDB} {
			_, err := admin.Exec(ctx, "CREATE DATABASE "+db)
			require.NoError(t, err)
		}
		t.Cleanup(func() {
			for _, db := range []string{viewDB, plainDB} {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
			}
		})
		installer, err := pgxpool.New(ctx, pg.DSN(viewDB, pgtest.RoleSuperuser))
		require.NoError(t, err)
		t.Cleanup(installer.Close)
		_, err = installer.Exec(ctx, "CREATE EXTENSION pg_stat_statements")
		require.NoError(t, err)

		opts := agent.DefaultConnOptions()
		opts.MaxDatabases = 1
		opts.Include = []*regexp.Regexp{regexp.MustCompile("^(" + viewDB + "|" + plainDB + ")$")}
		mgr, err := agent.NewManager(ctx, target, pg.DSN(plainDB, pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		// commit runs n statements in db. Each one is its own commit, which is
		// what the budget counts.
		commit := func(db string, n int) {
			pool, err := pgxpool.New(ctx, pg.DSN(db, pgtest.RoleT0))
			require.NoError(t, err)
			defer pool.Close()
			for range n {
				_, err := pool.Exec(ctx, "SELECT 1")
				require.NoError(t, err)
			}
		}
		// budgetMonitors reports which databases the budget monitors now. A
		// failed discovery reads as none, so Eventually keeps polling.
		budgetMonitors := func() map[string]bool {
			dbs, err := mgr.Discover(ctx)
			monitored := map[string]bool{}
			if err != nil {
				return monitored
			}
			for _, db := range dbs {
				if db.Monitored {
					monitored[db.Name] = true
				}
			}
			return monitored
		}

		commit(viewDB, 200)
		require.Eventually(t, func() bool { return budgetMonitors()[viewDB] }, 20*time.Second, 200*time.Millisecond,
			"the budget should monitor the busier database first")

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)
		require.True(t, schedules.gates[target].exts["pg_stat_statements"], "the view is found in the monitored database")
		require.True(t, schedules.gates[target].databases[viewDB])

		// plainDB now ranks above the view, so the budget moves on.
		commit(plainDB, 600)
		require.Eventually(t, func() bool {
			monitored := budgetMonitors()
			return monitored[plainDB] && !monitored[viewDB]
		}, 20*time.Second, 200*time.Millisecond, "the budget should move to the other database")

		currentScheduler := func() *agent.Scheduler {
			schedules.schedMu.Lock()
			defer schedules.schedMu.Unlock()
			return schedules.current
		}
		before := currentScheduler()
		schedules.reconcile()

		require.Same(t, before, currentScheduler(), "a database the budget pushed out but the schedule holds must not rebuild it")
		require.True(t, schedules.gates[target].databases[viewDB], "the database the schedule still runs checks for stays")
		require.True(t, schedules.gates[target].exts["pg_stat_statements"], "its view still counts")
		require.NotContains(t, takeSkips(&mu, pending, target), "stat_statements")
	})
}

// backendPIDs lists the sessions the agent holds on db. Its pools name their
// sessions after the database they connect to.
func backendPIDs(t *testing.T, admin *pgxpool.Pool, db string) []int32 {
	t.Helper()
	rows, err := admin.Query(context.Background(), "SELECT pid FROM pg_stat_activity WHERE application_name = $1 ORDER BY pid", "pglens/"+db)
	require.NoError(t, err)
	defer rows.Close()
	var pids []int32
	for rows.Next() {
		var pid int32
		require.NoError(t, rows.Scan(&pid))
		pids = append(pids, pid)
	}
	require.NoError(t, rows.Err())
	return pids
}

// TestINTSCH006_WaitingDatabaseIsNotConnected: the budget monitors two databases
// when the schedule is built, and the schedule runs checks for both. Two busier
// databases then take the budget. The schedule keeps the first two while they
// are listed, so the new ones wait and pglens reports them as not monitored. The
// reconcile must not connect to them to look for pg_stat_statements: such a
// connection takes a slot under the manager's cap and closes a pool the schedule
// runs its checks through. A view installed in a waiting database does not count
// either.
func TestINTSCH006_WaitingDatabaseIsNotConnected(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// The maintenance database is outside the include list and has no view,
		// so the statements lookup looks only at the monitored databases.
		const maintDB = "sch006_maint"
		const heldA, heldB, waitingA, waitingB = "sch006_held_a", "sch006_held_b", "sch006_waiting_a", "sch006_waiting_b"
		const target = "waiting-target"
		dbs := []string{maintDB, heldA, heldB, waitingA, waitingB}
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		for _, db := range dbs {
			_, err := admin.Exec(ctx, "CREATE DATABASE "+db)
			require.NoError(t, err)
		}
		t.Cleanup(func() {
			for _, db := range dbs {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
			}
		})

		opts := agent.DefaultConnOptions()
		opts.MaxDatabases = 2
		opts.Include = []*regexp.Regexp{regexp.MustCompile("^sch006_(held|waiting)_")}
		mgr, err := agent.NewManager(ctx, target, pg.DSN(maintDB, pgtest.RoleT0), opts, clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)

		// commit runs n statements in db. Each one is its own commit, which is
		// what the budget counts.
		commit := func(db string, n int) {
			pool, err := pgxpool.New(ctx, pg.DSN(db, pgtest.RoleT0))
			require.NoError(t, err)
			defer pool.Close()
			for range n {
				_, err := pool.Exec(ctx, "SELECT 1")
				require.NoError(t, err)
			}
		}
		// budgetMonitors reports which databases the budget monitors now. A
		// failed discovery reads as none, so Eventually keeps polling.
		budgetMonitors := func() map[string]bool {
			dbs, err := mgr.Discover(ctx)
			monitored := map[string]bool{}
			if err != nil {
				return monitored
			}
			for _, db := range dbs {
				if db.Monitored {
					monitored[db.Name] = true
				}
			}
			return monitored
		}

		commit(heldA, 200)
		commit(heldB, 200)
		require.Eventually(t, func() bool {
			monitored := budgetMonitors()
			return monitored[heldA] && monitored[heldB]
		}, 20*time.Second, 200*time.Millisecond, "the budget should monitor the two busiest databases at start")

		var mu sync.Mutex
		pending := map[string][]wire.Result{}
		scheduledDBs := map[string]map[string]bool{}
		schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
		require.NoError(t, schedules.start(ctx))
		t.Cleanup(schedules.stop)
		require.Equal(t, map[string]bool{heldA: true, heldB: true}, schedules.gates[target].databases)
		require.NotEmpty(t, backendPIDs(t, admin, heldA), "the schedule runs through a pool for each database it holds")
		require.NotEmpty(t, backendPIDs(t, admin, heldB))

		// The view is in a waiting database. It must not count: pglens does not
		// collect from that database, so the schedule must not read through it.
		installer, err := pgxpool.New(ctx, pg.DSN(waitingA, pgtest.RoleSuperuser))
		require.NoError(t, err)
		t.Cleanup(installer.Close)
		_, err = installer.Exec(ctx, "CREATE EXTENSION pg_stat_statements")
		require.NoError(t, err)

		// The two busier databases take the budget. The schedule keeps the first
		// two, since they are still listed.
		commit(waitingA, 800)
		commit(waitingB, 800)
		require.Eventually(t, func() bool {
			monitored := budgetMonitors()
			return monitored[waitingA] && monitored[waitingB] && !monitored[heldA] && !monitored[heldB]
		}, 20*time.Second, 200*time.Millisecond, "the budget should move to the busier databases")
		require.Empty(t, backendPIDs(t, admin, waitingA), "nothing has connected to a database the schedule does not hold")
		require.Empty(t, backendPIDs(t, admin, waitingB))

		pidsA, pidsB := backendPIDs(t, admin, heldA), backendPIDs(t, admin, heldB)
		require.NotEmpty(t, pidsA)
		require.NotEmpty(t, pidsB)

		currentScheduler := func() *agent.Scheduler {
			schedules.schedMu.Lock()
			defer schedules.schedMu.Unlock()
			return schedules.current
		}
		before := currentScheduler()
		schedules.reconcile()

		require.Same(t, before, currentScheduler(), "a database the budget pushed out but the schedule holds must not rebuild it")
		require.Empty(t, backendPIDs(t, admin, waitingA), "the reconcile must not connect to a database the schedule does not hold")
		require.Empty(t, backendPIDs(t, admin, waitingB))
		// With the cap at two, a lookup that did reach the waiting databases
		// would evict them again before the reconcile returns. The survival of
		// the scheduled pools below is what shows that it did.
		require.Subset(t, backendPIDs(t, admin, heldA), pidsA, "the pool of a scheduled database must survive the reconcile")
		require.Subset(t, backendPIDs(t, admin, heldB), pidsB, "the pool of a scheduled database must survive the reconcile")
		require.Equal(t, map[string]bool{heldA: true, heldB: true}, schedules.gates[target].databases)
		require.False(t, schedules.gates[target].exts["pg_stat_statements"], "a view in a database the schedule does not hold must not count")
	})
}

// TestINTSCH007_UnreadableTargetDoesNotConnectToNewDatabase: the identity store is
// corrupt, so the target's version and role stay unknown, although discovery
// still works. That read is not adopted, and the schedule keeps the databases it
// holds. A database created afterwards fits the budget and takes the free slot
// on discovery, but pglens must not connect to it while the target is unreadable.
// Once the identity store recovers, the read is adopted and the database is
// scheduled, as it was before.
func TestINTSCH007_UnreadableTargetDoesNotConnectToNewDatabase(t *testing.T) {
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			primary, _ := pgtest.PrimaryStandby(t, major)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// The corrupt file is set before the first read: the cache of a version
			// that was read once is kept, so it could not become unknown later.
			// Discovery does not use the identity store, so it still works.
			dir := t.TempDir()
			corrupt := filepath.Join(dir, "corrupt.json")
			require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o600))
			t.Setenv("PGLENS_IDENTITY_PATH", corrupt)

			const target = "unreadable-target"
			const dbA, dbB = "sch007_a", "sch007_b"
			admin := primary.Pool(t, "postgres", pgtest.RoleSuperuser)
			_, err := admin.Exec(ctx, "CREATE DATABASE "+dbA)
			require.NoError(t, err)
			t.Cleanup(func() {
				for _, db := range []string{dbA, dbB} {
					_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
				}
			})

			// The budget takes two databases. The include list leaves the
			// maintenance database out, so only the sch007 databases are listed.
			opts := agent.DefaultConnOptions()
			opts.MaxDatabases = 2
			opts.Include = []*regexp.Regexp{regexp.MustCompile("^sch007_")}
			mgr, err := agent.NewManager(ctx, target, primary.DSN("postgres", pgtest.RoleT0), opts, clock.System())
			require.NoError(t, err)
			t.Cleanup(mgr.Close)

			var mu sync.Mutex
			pending := map[string][]wire.Result{}
			scheduledDBs := map[string]map[string]bool{}
			schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, scheduledDBs)
			require.NoError(t, schedules.start(ctx))
			t.Cleanup(schedules.stop)
			require.Zero(t, mgr.PGVersion(), "the version must stay unknown while the identity store is corrupt")
			require.Equal(t, map[string]bool{dbA: true}, schedules.gates[target].databases)

			// The new database fits the budget, so discovery would fill the free slot
			// with it. The read is not adopted, so nothing may connect to it.
			_, err = admin.Exec(ctx, "CREATE DATABASE "+dbB)
			require.NoError(t, err)
			currentScheduler := func() *agent.Scheduler {
				schedules.schedMu.Lock()
				defer schedules.schedMu.Unlock()
				return schedules.current
			}
			before := currentScheduler()
			schedules.reconcile()

			require.Empty(t, backendPIDs(t, admin, dbB), "a read that is not adopted must not connect to a database the schedule does not hold")
			require.Same(t, before, currentScheduler(), "an unreadable target keeps the schedule it has")
			require.Equal(t, map[string]bool{dbA: true}, schedules.gates[target].databases)

			// The identity store recovers. The read is adopted, and the free slot is
			// filled as it was before the target became unreadable.
			t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(dir, "identity.json"))
			schedules.reconcile()

			require.Equal(t, map[string]bool{dbA: true, dbB: true}, schedules.gates[target].databases)
		})
	}
}
