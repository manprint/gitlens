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

// TestNextGate pins the rules that decide when the schedule is rebuilt: a
// change to a gate rebuilds it, an unreadable target keeps the gate it had, and
// the database set only grows.
func TestNextGate(t *testing.T) {
	primary := targetGate{
		role:      pgtype.RolePrimary,
		version:   pgtype.PGVersion(190000),
		tier:      pgtype.TierReadOnly,
		exts:      map[string]bool{"pg_stat_statements": true},
		databases: map[string]bool{"app": true},
	}
	withDBs := func(g targetGate, names ...string) targetGate {
		g.databases = map[string]bool{}
		for _, n := range names {
			g.databases[n] = true
		}
		return g
	}

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
			read:        withDBs(primary, "other"),
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
			name:        "a promotion changes the role",
			last:        primary,
			seen:        true,
			read:        func() targetGate { g := primary; g.role = pgtype.RoleStandby; return g }(),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "an extension installed later changes the gate",
			last:        primary,
			seen:        true,
			read:        func() targetGate { g := primary; g.exts = map[string]bool{"pg_stat_statements": false}; return g }(),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app"},
		},
		{
			name:        "a new database is a change and is kept",
			last:        primary,
			seen:        true,
			read:        withDBs(primary, "app", "created_later"),
			readOK:      true,
			wantChanged: true,
			wantDBs:     []string{"app", "created_later"},
		},
		{
			name:        "a database the budget pushed out is still collected and is no change",
			last:        withDBs(primary, "app", "old"),
			seen:        true,
			read:        primary,
			readOK:      true,
			wantChanged: false,
			wantDBs:     []string{"app", "old"},
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
	done := make(chan struct{})
	go func() {
		defer close(done)
		schedules.reconcile()
	}()
	// NewManager dials the pool's first connection itself. The read's discovery
	// dials a second one, so a second attempt means the read is in flight.
	require.Eventually(t, func() bool { return target.attempts.Load() >= 2 }, 5*time.Second, 10*time.Millisecond)

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
