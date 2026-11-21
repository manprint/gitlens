//go:build integration

package watch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// session holds one workload connection for the length of a test.
func session(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	return conn
}

// background runs sql on conn and waits for it at cleanup; its error, a
// cancel or a deadlock, is the test's to cause and is ignored.
func background(t *testing.T, ctx context.Context, conn *pgxpool.Conn, sql string) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = conn.Exec(ctx, sql)
	}()
	t.Cleanup(wg.Wait)
}

func collect(w *Watcher, ctx context.Context, lockWaiting bool, got *[]Incident) {
	w.emit = func(in []Incident) { *got = append(*got, in...) }
	w.Tick(ctx, lockWaiting)
}

// INT-WATCH-001: a session waiting for a row another transaction updated is
// captured with the table, the row's ctid, the lock mode and both sessions'
// statements, read by the monitoring role.
func TestINTWATCH001_RowLockWaitNamesTableAndTuple(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg.Exec(t, `DROP TABLE IF EXISTS watch_t; CREATE TABLE watch_t (id int PRIMARY KEY, v int); INSERT INTO watch_t VALUES (1, 0), (2, 0)`)
		t.Cleanup(func() { pg.Exec(t, `DROP TABLE IF EXISTS watch_t`) })
		work := pg.Pool(t, "app", pgtest.RoleSuperuser)

		holder := session(t, ctx, work)
		_, err := holder.Exec(ctx, `BEGIN; UPDATE watch_t SET v = v + 1 WHERE id = 2`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = holder.Exec(context.Background(), `ROLLBACK`) })
		waiter := session(t, ctx, work)
		background(t, ctx, waiter, `UPDATE watch_t SET v = v + 10 WHERE id = 2`)
		time.Sleep(900 * time.Millisecond)

		mon := pg.Pool(t, "app", pgtest.RoleT0)
		conn := session(t, ctx, mon)
		w := New(conn, clock.System(), Options{SessionEvery: 1000}, nil, nil)
		var got []Incident
		collect(w, ctx, true, &got)
		require.NoError(t, w.LastError())
		require.Len(t, got, 1)
		inc := got[0]
		require.Equal(t, TypeLockWait, inc.Type)
		require.Equal(t, "public.watch_t", inc.Relation)
		require.Equal(t, "(0,2)", inc.CTID)
		require.Equal(t, "ShareLock", inc.LockMode)
		require.Equal(t, "app", inc.Database)
		require.GreaterOrEqual(t, inc.WaitSeconds, 0.5)
		require.Len(t, inc.Sessions, 2)
		require.Contains(t, inc.Sessions[0].Query, "v + 10")
		require.Equal(t, []int32{inc.Sessions[1].PID}, inc.Sessions[0].BlockedBy)
		require.Contains(t, inc.Sessions[1].Query, "v + 1 WHERE id = 2")
		require.Equal(t, "idle in transaction", inc.Sessions[1].State)

		// The same wait on the next probe is not sent again.
		got = nil
		collect(w, ctx, true, &got)
		require.Empty(t, got)
	})
}

// INT-WATCH-002: two transactions waiting for each other's row are captured
// as a deadlock before PostgreSQL's detector breaks the cycle.
func TestINTWATCH002_CycleIsCapturedAsDeadlock(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg.Exec(t, `DROP TABLE IF EXISTS watch_a, watch_b; CREATE TABLE watch_a (id int PRIMARY KEY); CREATE TABLE watch_b (id int PRIMARY KEY); INSERT INTO watch_a VALUES (1); INSERT INTO watch_b VALUES (1)`)
		t.Cleanup(func() { pg.Exec(t, `DROP TABLE IF EXISTS watch_a, watch_b`) })
		work := pg.Pool(t, "app", pgtest.RoleSuperuser)

		a := session(t, ctx, work)
		b := session(t, ctx, work)
		for _, c := range []*pgxpool.Conn{a, b} {
			_, err := c.Exec(ctx, `SET deadlock_timeout = '20s'`)
			require.NoError(t, err)
		}
		_, err := a.Exec(ctx, `BEGIN; UPDATE watch_a SET id = id WHERE id = 1`)
		require.NoError(t, err)
		_, err = b.Exec(ctx, `BEGIN; UPDATE watch_b SET id = id WHERE id = 1`)
		require.NoError(t, err)
		background(t, ctx, a, `UPDATE watch_b SET id = id WHERE id = 1`)
		time.Sleep(200 * time.Millisecond)
		background(t, ctx, b, `UPDATE watch_a SET id = id WHERE id = 1`)
		time.Sleep(300 * time.Millisecond)
		t.Cleanup(func() {
			pg.Exec(t, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE 'UPDATE watch_%' AND pid <> pg_backend_pid()`)
		})

		mon := pg.Pool(t, "app", pgtest.RoleT0)
		conn := session(t, ctx, mon)
		w := New(conn, clock.System(), Options{SessionEvery: 1000}, nil, nil)
		var got []Incident
		collect(w, ctx, true, &got)
		require.NoError(t, w.LastError())
		require.Len(t, got, 1)
		require.Equal(t, TypeDeadlock, got[0].Type)
		require.Len(t, got[0].Sessions, 2)
		rels := map[string]bool{}
		for _, s := range got[0].Sessions {
			require.Len(t, s.BlockedBy, 1)
			if s.Row != nil {
				rels[s.Row.Relation] = true
			}
		}
		require.Equal(t, map[string]bool{"public.watch_a": true, "public.watch_b": true}, rels)
	})
}

// INT-WATCH-003: a transaction left idle is captured once, with the tables it
// holds locks on.
func TestINTWATCH003_IdleTransactionNamesHeldTables(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pg.Exec(t, `DROP TABLE IF EXISTS watch_idle; CREATE TABLE watch_idle (id int PRIMARY KEY); INSERT INTO watch_idle VALUES (1)`)
		t.Cleanup(func() { pg.Exec(t, `DROP TABLE IF EXISTS watch_idle`) })
		work := pg.Pool(t, "app", pgtest.RoleSuperuser)
		idle := session(t, ctx, work)
		_, err := idle.Exec(ctx, `BEGIN; SELECT * FROM watch_idle FOR UPDATE`)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = idle.Exec(context.Background(), `ROLLBACK`) })
		time.Sleep(1200 * time.Millisecond)

		mon := pg.Pool(t, "app", pgtest.RoleT0)
		conn := session(t, ctx, mon)
		w := New(conn, clock.System(), Options{SessionEvery: 1, IdleInTransaction: time.Second}, nil, nil)
		var got []Incident
		collect(w, ctx, false, &got)
		require.NoError(t, w.LastError())
		require.Len(t, got, 1)
		require.Equal(t, TypeIdleInXact, got[0].Type)
		require.Equal(t, "public.watch_idle", got[0].Relation)
		require.Equal(t, []string{"public.watch_idle"}, got[0].Sessions[0].HeldRelations)
		require.NotNil(t, got[0].Sessions[0].XactStart)

		got = nil
		collect(w, ctx, false, &got)
		require.Empty(t, got)
	})
}

// INT-WATCH-004: two INSERTs of each other's key deadlock; the cycle is
// broken before the probe sees it, so the deadlock is rebuilt from the wait
// seen before it, with the table the INSERT writes (it holds no row lock).
func TestINTWATCH004_DeadlockRebuiltFromCounter(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		pg.Exec(t, `DROP TABLE IF EXISTS watch_k; CREATE TABLE watch_k (id int PRIMARY KEY)`)
		t.Cleanup(func() { pg.Exec(t, `DROP TABLE IF EXISTS watch_k`) })
		work := pg.Pool(t, "app", pgtest.RoleSuperuser)

		a := session(t, ctx, work)
		b := session(t, ctx, work)
		_, err := a.Exec(ctx, `SET deadlock_timeout = '20s'`)
		require.NoError(t, err)
		_, err = b.Exec(ctx, `SET deadlock_timeout = '100ms'`)
		require.NoError(t, err)
		_, err = a.Exec(ctx, `BEGIN; INSERT INTO watch_k VALUES (1)`)
		require.NoError(t, err)
		_, err = b.Exec(ctx, `BEGIN; INSERT INTO watch_k VALUES (2)`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = a.Exec(context.Background(), `ROLLBACK`)
			_, _ = b.Exec(context.Background(), `ROLLBACK`)
		})
		background(t, ctx, a, `INSERT INTO watch_k VALUES (2)`)
		time.Sleep(200 * time.Millisecond)

		mon := pg.Pool(t, "app", pgtest.RoleT0)
		conn := session(t, ctx, mon)
		w := New(conn, clock.System(), Options{SessionEvery: 1000}, nil, nil)
		var got []Incident
		collect(w, ctx, true, &got)
		require.NoError(t, w.LastError())
		require.Empty(t, got, "a wait of 0.2 s is no lock_wait")

		_, err = b.Exec(ctx, `INSERT INTO watch_k VALUES (1)`)
		require.ErrorContains(t, err, "deadlock detected")
		_, err = b.Exec(ctx, `ROLLBACK`)
		require.NoError(t, err)

		// The deadlock counter reaches pg_stat_database when b's backend
		// flushes its statistics, within about ten seconds.
		require.Eventually(t, func() bool {
			collect(w, ctx, false, &got)
			return len(got) > 0
		}, 20*time.Second, 250*time.Millisecond)
		require.NoError(t, w.LastError())
		inc := got[0]
		require.Equal(t, TypeDeadlock, inc.Type)
		require.True(t, inc.Rebuilt)
		require.Equal(t, int64(1), inc.Deadlocks)
		require.Equal(t, "app", inc.Database)
		require.Equal(t, "public.watch_k", inc.Relation)
		require.Equal(t, "ShareLock", inc.LockMode)
		require.NotEmpty(t, inc.Sessions)
		require.Contains(t, inc.Sessions[0].Query, "INSERT INTO watch_k VALUES (2)")
		require.Contains(t, inc.Sessions[0].HeldRelations, "public.watch_k")
	})
}
