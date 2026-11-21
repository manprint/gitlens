package watch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A deadlock broken before the probe saw the cycle is rebuilt from the waits
// seen in its database when the counter grows.
func TestDeadlockRebuiltFromCounter(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 4), rowWait(200, 100, 0.3))
	h.q.deadlocks = []byte(`{"app": 2, "postgres": 0}`)

	require.Empty(t, h.tick(true), "a short wait is no lock_wait; the counters are the baseline")
	require.Equal(t, 1, h.q.count("deadlocks"))

	h.clk.Advance(time.Second)
	h.q.deadlocks = []byte(`{"app": 3, "postgres": 0}`)
	got := h.tick(false)
	require.Len(t, got, 1)
	inc := got[0]
	require.Equal(t, TypeDeadlock, inc.Type)
	require.True(t, inc.Rebuilt)
	require.Equal(t, int64(1), inc.Deadlocks)
	require.Equal(t, "app", inc.Database)
	require.Equal(t, "public.orders", inc.Relation)
	require.Equal(t, "(3,7)", inc.CTID)
	require.Equal(t, "ShareLock", inc.LockMode)
	require.Equal(t, t0.Add(time.Second), inc.CapturedAt)
	require.Equal(t, t0, *inc.Since)
	require.True(t, strings.HasPrefix(inc.ID, "deadlock-"))
	require.Equal(t, []int32{200, 100}, []int32{inc.Sessions[0].PID, inc.Sessions[1].PID}, "waiter first, then the holder")

	h.clk.Advance(time.Second)
	h.q.deadlocks = []byte(`{"app": 4, "postgres": 0}`)
	require.Empty(t, h.tick(false), "the sessions were used: a later count has nothing to rebuild from")
	require.Equal(t, 2, h.q.count("deadlocks"), "with no session kept the counters are not read")
}

func TestDeadlockCounterAfterCapturedCycleIsNotRebuilt(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, rowWait(10, 20, 0.9), rowWait(20, 10, 0.4))
	h.q.deadlocks = []byte(`{"app": 0}`)
	got := h.tick(true)
	require.Len(t, got, 1)
	require.False(t, got[0].Rebuilt)

	h.clk.Advance(time.Second)
	h.q.deadlocks = []byte(`{"app": 1}`)
	require.Empty(t, h.tick(false))

	h.q.locks = asJSON(t, rowWait(30, 40, 0.2), holder(40, 2))
	require.Empty(t, h.tick(true))
	h.q.deadlocks = []byte(`{"app": 3}`)
	got = h.tick(false)
	require.Len(t, got, 1, "two more deadlocks and no cycle seen: rebuilt")
	require.Equal(t, int64(2), got[0].Deadlocks)
}

func TestDeadlockCountersReadOnlyAfterLockWaits(t *testing.T) {
	h := newHarness(Options{DeadlockLookback: 10 * time.Second}, nil)
	h.q.deadlocks = []byte(`{"app": 5}`)
	for range 5 {
		require.Empty(t, h.tick(false))
	}
	require.Zero(t, h.q.count("deadlocks"))

	h.q.locks = asJSON(t, holder(100, 4), rowWait(200, 100, 0.1))
	require.Empty(t, h.tick(true))
	h.clk.Advance(11 * time.Second)
	h.q.deadlocks = []byte(`{"app": 6}`)
	require.Empty(t, h.tick(false), "past the lookback the waits are forgotten")
	require.Equal(t, 1, h.q.count("deadlocks"))

	h.q.locks = asJSON(t, holder(100, 4), rowWait(200, 100, 0.1))
	require.Empty(t, h.tick(true), "a new baseline, not the count since the old one")
}

func TestDeadlockCountersNewDatabaseOrResetIgnored(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 4), rowWait(200, 100, 0.1))
	h.q.deadlocks = []byte(`{"app": 9}`)
	require.Empty(t, h.tick(true))
	h.q.deadlocks = []byte(`{"app": 1, "new": 3}`)
	require.Empty(t, h.tick(true))
}

func TestDeadlockCounterErrors(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 4), rowWait(200, 100, 0.1))
	h.q.deadlocks = []byte(`not json`)
	h.tick(true)
	require.ErrorContains(t, h.w.LastError(), "decode deadlock counters")

	h.q.deadlocks = []byte(`{}`)
	h.q.err = errors.New("boom")
	h.tick(false)
	require.ErrorContains(t, h.w.LastError(), "boom")
}

func TestRememberCapsSessionsPerDatabase(t *testing.T) {
	h := newHarness(Options{}, nil)
	var sessions []Session
	for pid := range int32(maxRecentPerDatabase + 5) {
		sessions = append(sessions, holder(pid+1, 1))
	}
	h.w.remember(t0, sessions[:5])
	h.w.remember(t0.Add(time.Second), sessions[5:])
	require.Len(t, h.w.recent["app"], maxRecentPerDatabase)
	for pid := range int32(5) {
		_, kept := h.w.recent["app"][pid+1]
		require.False(t, kept, fmt.Sprintf("oldest pid %d dropped", pid+1))
	}
	h.w.remember(t0, []Session{{PID: 1}})
	require.NotContains(t, h.w.recent, "", "a session with no database is not kept")
	_, err := h.w.deadlockIncidents(context.Background())
	require.NoError(t, err)
}

// Two INSERTs of the same key: the waiter holds no row lock, only the
// relation locks of the table it writes, which name the table.
func TestInsertConflictNamesTheWrittenTable(t *testing.T) {
	var gotOIDs []uint32
	resolve := func(_ context.Context, db string, oids []uint32) map[uint32]string {
		require.Equal(t, "logistics", db)
		gotOIDs = oids
		return map[uint32]string{16500: "public.shipments", 16501: "public.audit"}
	}
	h := newHarness(Options{}, resolve)
	h.q.locks = []byte(`[
	 {"pid": 7, "datname": "logistics", "state": "active", "query": "INSERT INTO \"shipments\" (id) VALUES (1)", "blocked_by": [8],
	  "wait": {"locktype": "transactionid", "mode": "ShareLock", "transactionid": "99", "wait_seconds": 0.9},
	  "writes": [{"locktype": "relation", "mode": "RowExclusiveLock", "relation_oid": 16501, "database": "logistics"},
	             {"locktype": "relation", "mode": "RowExclusiveLock", "relation_oid": 16500, "database": "logistics"},
	             {"locktype": "relation", "mode": "RowExclusiveLock", "relation": "pg_catalog.pg_class", "database": "logistics"},
	             {"locktype": "relation", "mode": "RowExclusiveLock", "relation_oid": 16500, "database": "logistics"}]},
	 {"pid": 8, "datname": "logistics", "state": "idle in transaction", "query": "INSERT INTO shipments (id) VALUES (1)",
	  "writes": [{"locktype": "relation", "mode": "RowExclusiveLock", "relation_oid": 16500, "database": "logistics"}]}]`)
	got := h.tick(true)
	require.Len(t, got, 1)
	inc := got[0]
	require.Equal(t, TypeLockWait, inc.Type)
	require.Equal(t, "public.shipments", inc.Relation, "the table the statement names, not the first written")
	require.Empty(t, inc.CTID)
	require.ElementsMatch(t, []uint32{16500, 16501}, gotOIDs)
	require.Equal(t, []string{"public.audit", "public.shipments"}, inc.Sessions[0].HeldRelations)
	require.Equal(t, []string{"public.shipments"}, inc.Sessions[1].HeldRelations)
}

func TestWrittenTable(t *testing.T) {
	require.Empty(t, writtenTable(Session{}))
	require.Equal(t, "public.a", writtenTable(Session{Query: "SELECT 1", HeldRelations: []string{"public.a", "public.b"}}))
	require.Equal(t, `s."Mixed"`, writtenTable(Session{Query: `UPDATE s."Mixed" SET x = 1`, HeldRelations: []string{"public.a", `s."Mixed"`}}))
	require.True(t, systemRelation("pg_toast.pg_toast_1"))
	require.False(t, systemRelation("public.pg_like"))
}

func TestHeldFromWritesCaps(t *testing.T) {
	s := Session{}
	for i := range maxHeldRelations + 3 {
		s.writes = append(s.writes, LockRef{Relation: fmt.Sprintf("public.t%d", i)})
	}
	sessions := []Session{s}
	heldFromWrites(sessions)
	require.Len(t, sessions[0].HeldRelations, maxHeldRelations)
}

func TestContainsIdent(t *testing.T) {
	require.True(t, containsIdent(`insert into "shipments" (id)`, "shipments"))
	require.True(t, containsIdent("shipments", "shipments"))
	require.False(t, containsIdent("insert into shipments_log", "shipments"))
	require.False(t, containsIdent("update x", "a"))
	require.True(t, containsIdent("update x_a set a = 1", "a"))
}
