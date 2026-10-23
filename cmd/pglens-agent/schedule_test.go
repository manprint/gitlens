package main

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// TestNextGate pins the rules that decide when the schedule is rebuilt: a change
// to a gate rebuilds it, an unreadable target keeps the gate it had, and the
// databases are kept while discovery lists them. A free slot is filled from the
// busiest monitored databases, and no more than the budget allows is scheduled.
func TestNextGate(t *testing.T) {
	mon := func(name string) wire.Database { return wire.Database{Name: name, Monitored: true} }
	pushedOut := func(name string) wire.Database { return wire.Database{Name: name, SkipReason: "db_budget"} }
	// withDBs is a read whose discovery returned dbs, busiest first.
	withDBs := func(g targetGate, dbs ...wire.Database) targetGate {
		g.discovered = dbs
		g.databases = map[string]bool{}
		for _, db := range dbs {
			if db.Monitored {
				g.databases[db.Name] = true
			}
		}
		return g
	}
	// scheduled is a last gate that runs checks for names.
	scheduled := func(g targetGate, names ...string) targetGate {
		g.databases = map[string]bool{}
		for _, n := range names {
			g.databases[n] = true
		}
		return g
	}
	primary := withDBs(targetGate{
		role:    pgtype.RolePrimary,
		version: pgtype.PGVersion(190000),
		tier:    pgtype.TierReadOnly,
		exts:    map[string]bool{"pg_stat_statements": true},
	}, mon("app"))
	standby := primary
	standby.role = pgtype.RoleStandby

	tests := []struct {
		name        string
		last        targetGate
		seen        bool
		read        targetGate
		readOK      bool
		wantChanged bool
		wantDBs     []string
	}{
		{
			name:        "first read is always a change",
			seen:        false,
			read:        primary,
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "unreadable target keeps the gate it was scheduled against",
			last:        primary,
			seen:        true,
			read:        withDBs(primary, mon("other")),
			readOK:      false,
			wantChanged: false,
			wantDBs:     []string{"app"},
		},
		{
			name:        "same gate and databases is no change",
			last:        primary,
			seen:        true,
			read:        primary,
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a rejoin as a standby changes the role",
			last:        primary,
			seen:        true,
			read:        standby,
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a promotion changes the role",
			last:        standby,
			seen:        true,
			read:        primary,
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:   "an extension installed later changes the gate",
			last:   primary,
			seen:   true,
			read:   func() targetGate { g := primary; g.exts = map[string]bool{"pg_stat_statements": false}; return g }(),
			readOK: true,
			// the set is still app, so only the extension changed
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a free slot takes a new database",
			last:        scheduled(primary, "app"),
			seen:        true,
			read:        withDBs(primary, mon("app"), mon("created_later")),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app", "created_later"},
		},
		{
			name:        "a database the budget pushed out stays while discovery lists it",
			last:        scheduled(primary, "app", "old"),
			seen:        true,
			read:        withDBs(primary, mon("app"), mon("created_later"), pushedOut("old")),
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app", "old"},
		},
		{
			name:        "newcomers wait while every scheduled database is still listed",
			last:        scheduled(primary, "app", "old"),
			seen:        true,
			read:        withDBs(primary, mon("created_later"), mon("other"), pushedOut("app"), pushedOut("old")),
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app", "old"},
		},
		{
			name:        "a dropped database leaves and its slot is refilled",
			last:        scheduled(primary, "app", "old"),
			seen:        true,
			read:        withDBs(primary, mon("app"), mon("created_later")),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app", "created_later"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := nextGate(tt.last, tt.seen, tt.read, tt.readOK)
			require.Equal(t, tt.wantChanged, changed)
			var dbs []string
			for name := range got.databases {
				dbs = append(dbs, name)
			}
			require.ElementsMatch(t, tt.wantDBs, dbs)
		})
	}
}

// TestNextGate_NeverSchedulesMoreThanTheBudget: under churn in the budget's
// ranking, and databases that come and go, the scheduled set stays within the
// budget and only holds databases discovery lists.
func TestNextGate_NeverSchedulesMoreThanTheBudget(t *testing.T) {
	const budget = 2
	mon := func(name string) wire.Database { return wire.Database{Name: name, Monitored: true} }
	pushedOut := func(name string) wire.Database { return wire.Database{Name: name, SkipReason: "db_budget"} }
	// Each read is discovery, busiest first; the first budget entries are monitored.
	reads := [][]wire.Database{
		{mon("a"), mon("b"), pushedOut("c")},
		{mon("c"), mon("b"), pushedOut("a")},
		{mon("c"), pushedOut("b"), pushedOut("a")},
		{mon("b"), mon("c"), pushedOut("d")},
		{mon("d"), mon("a"), pushedOut("b"), pushedOut("c")},
		{mon("d"), mon("e"), pushedOut("a")},
		{mon("e"), mon("d"), pushedOut("a")},
		{mon("a"), mon("e")},
		{mon("e")},
		{mon("c"), mon("b"), pushedOut("a"), pushedOut("d"), pushedOut("e")},
	}

	gate := targetGate{role: pgtype.RolePrimary, version: pgtype.PGVersion(190000), tier: pgtype.TierReadOnly, exts: map[string]bool{}}
	var last targetGate
	seen := false
	for i, discovered := range reads {
		read := gate
		read.discovered = discovered
		read.databases = map[string]bool{}
		for _, db := range discovered {
			if db.Monitored && len(read.databases) < budget {
				read.databases[db.Name] = true
			}
		}
		got, _ := nextGate(last, seen, read, true)
		require.LessOrEqual(t, len(got.databases), budget, "read %d scheduled more than the budget", i)
		listed := map[string]bool{}
		for _, db := range discovered {
			listed[db.Name] = true
		}
		for name := range got.databases {
			require.True(t, listed[name], "read %d scheduled %q, which discovery does not list", i, name)
		}
		last, seen = got, true
	}
}

// TestNextGate_UnknownStatementsKeepTheAnswer: a database that cannot be checked
// for pg_stat_statements must not read as "absent" and rebuild the schedule. The
// other gate values still follow the read.
func TestNextGate_UnknownStatementsKeepTheAnswer(t *testing.T) {
	last := targetGate{
		role:      pgtype.RolePrimary,
		version:   pgtype.PGVersion(190000),
		tier:      pgtype.TierReadOnly,
		exts:      map[string]bool{"pg_stat_statements": true},
		databases: map[string]bool{"app": true},
	}
	read := last
	read.discovered = []wire.Database{{Name: "app", Monitored: true}}
	read.exts = map[string]bool{"pg_stat_statements": false}
	read.statementsUnknown = true

	got, changed := nextGate(last, true, read, true)
	require.False(t, changed, "an unknown answer is not a change")
	require.True(t, got.exts["pg_stat_statements"])

	read.role = pgtype.RoleStandby
	got, changed = nextGate(last, true, read, true)
	require.True(t, changed, "a role change in the same read still rebuilds the schedule")
	require.Equal(t, pgtype.RoleStandby, got.role)
	require.True(t, got.exts["pg_stat_statements"])

	read.statementsUnknown = false
	read.role = pgtype.RolePrimary
	got, changed = nextGate(last, true, read, true)
	require.True(t, changed, "once the view can be looked for, its absence is an answer again")
	require.False(t, got.exts["pg_stat_statements"])
}

// hungTarget is a listener that accepts every connection and never answers one:
// a target the agent cannot reach, behind a route that drops its replies.
// attempts counts the connections it has accepted.
type hungTarget struct {
	dsn      string
	attempts atomic.Int32
}

func newHungTarget(t *testing.T) *hungTarget {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	h := &hungTarget{dsn: fmt.Sprintf("postgres://pglens@%s/postgres?sslmode=disable", ln.Addr())}
	go func() {
		var held []net.Conn
		defer func() {
			for _, c := range held {
				_ = c.Close()
			}
		}()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held = append(held, c)
			h.attempts.Add(1)
		}
	}()
	return h
}

// TestScheduleStopDoesNotWaitForAReadInFlight: a read of an unreachable target
// takes its whole timeout. A stop that comes while it is in flight, as the
// revoke path does, must not wait for it, and the read must not build a
// schedule once the stop has run.
func TestScheduleStopDoesNotWaitForAReadInFlight(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	target := newHungTarget(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr, err := agent.NewManager(ctx, "hung", target.dsn, agent.DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	t.Cleanup(mgr.Close)

	var mu sync.Mutex
	schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, map[string][]wire.Result{}, map[string]map[string]bool{})
	// NewManager dials the pool's first connection itself. Count the attempts
	// from after that one, so the read's own dial is the one that shows it in flight.
	require.Eventually(t, func() bool { return target.attempts.Load() >= 1 }, 5*time.Second, 10*time.Millisecond, "NewManager should dial the pool's first connection")
	dialed := target.attempts.Load()
	done := make(chan struct{})
	go func() {
		defer close(done)
		schedules.reconcile()
	}()
	require.Eventually(t, func() bool { return target.attempts.Load() > dialed }, 5*time.Second, 10*time.Millisecond, "the read should dial the target")

	began := time.Now()
	schedules.stop()
	require.Less(t, time.Since(began), 5*time.Second, "stop waited for a read of an unreachable target")

	// The read ends with its context, as it does at shutdown.
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("reconcile did not return once its context ended")
	}
	require.Nil(t, schedules.current, "no schedule may be built after stop")
}

// TestScheduleStartReadsTargetsTogether: each unreachable target holds its read
// for the whole discovery timeout. Start must read the targets together, so the
// second target is dialled while the first is still being read, instead of only
// after the first read has used its timeout.
func TestScheduleStartReadsTargetsTogether(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	targets := []*hungTarget{newHungTarget(t), newHungTarget(t)}
	var mgrs []*agent.Manager
	for i, target := range targets {
		mgr, err := agent.NewManager(ctx, fmt.Sprintf("hung-%d", i), target.dsn, agent.DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		t.Cleanup(mgr.Close)
		mgrs = append(mgrs, mgr)
	}

	var mu sync.Mutex
	schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), mgrs, &mu, map[string][]wire.Result{}, map[string]map[string]bool{})
	// NewManager dials each pool's first connection itself. Count from after it.
	for _, target := range targets {
		require.Eventually(t, func() bool { return target.attempts.Load() >= 1 }, 5*time.Second, 10*time.Millisecond, "NewManager should dial the pool's first connection")
	}
	dialed := targets[1].attempts.Load()
	started := make(chan error, 1)
	go func() {
		started <- schedules.start(ctx)
	}()
	// The second target's own read dials it again: that is the next attempt.
	require.Eventually(t, func() bool { return targets[1].attempts.Load() > dialed }, 3*time.Second, 10*time.Millisecond,
		"the second target was not read until the first one had finished")

	cancel()
	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("start did not return once its context ended")
	}
	t.Cleanup(schedules.stop)
}

// TestScheduleRebuildDropsTheSkipsOfTheLastBuild: a check the schedule skips is
// queued as a skip result when the schedule is built, and it waits there for the
// next push. A rebuild on gates that run the check must replace that skip, or the
// push would still report the check as skipped after the schedule runs it.
func TestScheduleRebuildDropsTheSkipsOfTheLastBuild(t *testing.T) {
	t.Setenv("PGLENS_IDENTITY_PATH", filepath.Join(t.TempDir(), "identity.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Nothing listens on port 1, so every scrape fails at once.
	const target = "rebuild-target"
	mgr, err := agent.NewManager(ctx, target, "postgres://pglens@127.0.0.1:1/postgres?sslmode=disable", agent.DefaultConnOptions(), clock.System())
	require.NoError(t, err)
	t.Cleanup(mgr.Close)

	gate := func(role pgtype.Role) targetGate {
		return targetGate{role: role, version: 190000, tier: pgtype.TierReadOnly, exts: map[string]bool{}, databases: map[string]bool{}}
	}
	var mu sync.Mutex
	pending := map[string][]wire.Result{}
	schedules := newScheduleSet(ctx, &agent.Config{}, clock.System(), []*agent.Manager{mgr}, &mu, pending, map[string]map[string]bool{})
	t.Cleanup(schedules.stop)

	// pendingSkips reads the skip reasons pending holds for the target without
	// taking them, so the next push would still send them.
	pendingSkips := func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		skips := map[string]string{}
		for _, r := range pending[target] {
			if r.SkipReason != "" {
				skips[r.Check] = r.SkipReason
			}
		}
		return skips
	}

	schedules.schedMu.Lock()
	schedules.gates = map[string]targetGate{target: gate(pgtype.RoleStandby)}
	require.NoError(t, schedules.buildLocked())
	schedules.schedMu.Unlock()
	require.Equal(t, "role standby not allowed", pendingSkips()["replication_streaming"], "the first build queues its skip")

	// The target is promoted and the schedule is rebuilt for the primary.
	schedules.schedMu.Lock()
	schedules.stopCurrentLocked()
	schedules.gates = map[string]targetGate{target: gate(pgtype.RolePrimary)}
	require.NoError(t, schedules.buildLocked())
	schedules.schedMu.Unlock()

	skips := pendingSkips()
	require.Equal(t, "role primary not allowed", skips["replication_receiver"])
	require.NotContains(t, skips, "replication_streaming", "the rebuild must replace the standby's skip, not leave it pending")
}
