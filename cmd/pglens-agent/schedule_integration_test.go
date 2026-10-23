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

// TestINTSCH002_ScheduleFollowsTheTargetRole: a target that cannot be read when
// the schedule is first built gets its checks once it can be read, and its
// role-gated checks follow the role: a standby runs replication_receiver, and
// after a promotion the former standby runs replication_streaming instead.
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
			require.Equal(t, "role unknown not allowed", skips["replication_streaming"])
			require.Equal(t, "role unknown not allowed", skips["replication_receiver"])

			// The identity store recovers. The next reconcile reads the standby
			// in full and schedules its receiver check.
			t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(dir, "identity.json"))
			schedules.reconcile()

			skips = takeSkips(&mu, pending, target)
			require.Equal(t, "role standby not allowed", skips["replication_streaming"])
			require.NotContains(t, skips, "replication_receiver")

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

		// The lockout must be real, or the test proves nothing.
		_, err = mgr.LocateStatements(ctx, []wire.Database{{Name: extDB, Monitored: true}})
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
