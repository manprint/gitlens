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
