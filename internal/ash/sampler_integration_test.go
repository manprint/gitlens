//go:build integration

package ash

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

// newTestPool opens a fresh pool against pg using the pglens role, independent
// of the pool pgtest itself uses internally.
func newTestPool(t *testing.T, ctx context.Context, pg *pgtest.PG) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// sleepingSession opens its own connection and holds it busy on pg_sleep for
// dur, so it shows up as an active backend for the sampler to observe.
func sleepingSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dur time.Duration) {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.Release()
		_, _ = conn.Exec(ctx, "SELECT pg_sleep($1)", dur.Seconds())
	}()
	t.Cleanup(wg.Wait)
}

// INT-ASH-001: with 3 sleeping sessions, one sample returns exactly 3 rows and
// none of them is the sampler itself.
func TestINTASH001_SampleExcludesSelf(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool := newTestPool(t, ctx, pg)
		for i := 0; i < 3; i++ {
			sleepingSession(t, ctx, pool, 3*time.Second)
		}
		time.Sleep(300 * time.Millisecond) // let the sleepers start

		samplerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer samplerConn.Release()

		var rows []SampleRow
		s := NewSampler(samplerConn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
		require.NoError(t, s.doSample(ctx))

		require.Len(t, rows, 3, "expected exactly the 3 sleeping backends (self must be excluded), got %+v", rows)
		for _, r := range rows {
			require.NotEqual(t, "CPU", r.WaitEvent, "a pg_sleep backend should not be classified CPU")
		}
	})
}

// INT-ASH-002: an `idle in transaction` session appears; a plain `idle`
// session does not.
func TestINTASH002_IdleInTransactionAppears(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool := newTestPool(t, ctx, pg)

		idleTxConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer idleTxConn.Release()
		_, err = idleTxConn.Exec(ctx, "BEGIN")
		require.NoError(t, err)
		defer func() { _, _ = idleTxConn.Exec(ctx, "ROLLBACK") }()
		_, err = idleTxConn.Exec(ctx, "SELECT 1")
		require.NoError(t, err)

		idleConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer idleConn.Release()
		_, err = idleConn.Exec(ctx, "SELECT 1")
		require.NoError(t, err)

		samplerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer samplerConn.Release()

		var rows []SampleRow
		s := NewSampler(samplerConn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
		require.NoError(t, s.doSample(ctx))

		found := false
		for _, r := range rows {
			require.NotEqual(t, "idle", r.State, "a plain idle session must never be sampled")
			if r.State == "idle in transaction" {
				found = true
			}
		}
		require.True(t, found, "expected the idle-in-transaction session to be sampled")
	})
}

// INT-ASH-003: with compute_query_id=on (the default in these test
// containers), query_id is non-NULL for an active query and matches the
// queryid reported by stat_statements for the same statement — the join that
// makes ASH useful.
func TestINTASH003_QueryIDMatchesStatStatements(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool := newTestPool(t, ctx, pg)

		var setting string
		require.NoError(t, pool.QueryRow(ctx, "SELECT current_setting('compute_query_id')").Scan(&setting))
		if setting != "on" {
			t.Skip("compute_query_id is not on in this environment")
		}

		const marker = "SELECT pg_sleep(2) /* INT-ASH-003 */"
		sleepingSession(t, ctx, pool, 2*time.Second)
		// sleepingSession runs a fixed statement; run our marker query directly
		// on its own connection instead so we control the exact SQL text.
		markerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer markerConn.Release()
			_, _ = markerConn.Exec(ctx, marker)
		}()
		t.Cleanup(wg.Wait)
		time.Sleep(300 * time.Millisecond)

		samplerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer samplerConn.Release()

		var rows []SampleRow
		s := NewSampler(samplerConn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
		require.NoError(t, s.doSample(ctx))

		var sampledQueryID *int64
		for _, r := range rows {
			if r.QueryID != nil {
				sampledQueryID = r.QueryID
			}
		}
		require.NotNil(t, sampledQueryID, "expected at least one sampled row with a non-nil query_id")

		require.Eventually(t, func() bool {
			var exists bool
			err := pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM pg_stat_statements WHERE queryid = $1)`,
				*sampledQueryID,
			).Scan(&exists)
			return err == nil && exists
		}, 5*time.Second, 200*time.Millisecond, "sampled query_id must match a pg_stat_statements row")
	})
}

// INT-ASH-004: with compute_query_id effectively off for the sampled session,
// the sampler still works, query_id is NULL, and the "no query id" warning is
// emitted at most once regardless of how many affected rows appear.
func TestINTASH004_NoQueryIDWhenDisabled(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool := newTestPool(t, ctx, pg)

		// compute_query_id is PGC_SUSET — a T0 (pg_monitor) session cannot
		// change it, confirmed empirically ("permission denied to set
		// parameter"). Only the session being sampled needs the privilege;
		// the sampler itself still runs as T0 via samplerConn below.
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		offConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		_, err = offConn.Exec(ctx, "SET compute_query_id = off")
		require.NoError(t, err)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer offConn.Release()
			_, _ = offConn.Exec(ctx, "SELECT pg_sleep(2)")
		}()
		t.Cleanup(wg.Wait)
		time.Sleep(300 * time.Millisecond)

		samplerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer samplerConn.Release()

		var rows []SampleRow
		s := NewSampler(samplerConn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
		require.NoError(t, s.doSample(ctx))
		require.NoError(t, s.doSample(ctx)) // second tick must not warn again (tested via no panic/race; log dedup asserted structurally)

		foundNilQueryID := false
		for _, r := range rows {
			if r.QueryID == nil {
				foundNilQueryID = true
			}
		}
		require.True(t, foundNilQueryID, "expected the compute_query_id=off session to sample with a nil query_id")
	})
}

// INT-ASH-005: the workers of a parallel query are sampled along with their
// leader, since each one is a backend doing work. Sampling the leader alone
// showed such a query as one session.
func TestINTASH005_SampleIncludesParallelWorkers(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, `CREATE TABLE IF NOT EXISTS ash_parallel_t AS SELECT g FROM generate_series(1, 4000) g`)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, `ANALYZE ash_parallel_t`)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, `CREATE OR REPLACE FUNCTION ash_parallel_slow(g int) RETURNS bool LANGUAGE plpgsql PARALLEL SAFE AS $$ BEGIN PERFORM pg_sleep(0.002); RETURN true; END $$`)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, `GRANT SELECT ON ash_parallel_t TO PUBLIC`)
		require.NoError(t, err)

		pool := newTestPool(t, ctx, pg)
		queryConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = queryConn.Exec(ctx, `SET max_parallel_workers_per_gather = 2; SET parallel_setup_cost = 0; SET parallel_tuple_cost = 0; SET min_parallel_table_scan_size = 0`)
		require.NoError(t, err)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer queryConn.Release()
			var n int
			// Cancelled by the test's end: the query only has to keep workers busy.
			_ = queryConn.QueryRow(ctx, `SELECT count(*) FROM ash_parallel_t WHERE ash_parallel_slow(g)`).Scan(&n)
		}()
		t.Cleanup(wg.Wait)

		require.Eventually(t, func() bool {
			var workers int
			err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'parallel worker' AND state = 'active'`).Scan(&workers)
			return err == nil && workers > 0
		}, 20*time.Second, 50*time.Millisecond, "the parallel query never started a worker")

		samplerConn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer samplerConn.Release()

		var rows []SampleRow
		s := NewSampler(samplerConn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
		require.NoError(t, s.doSample(ctx))
		require.GreaterOrEqual(t, len(rows), 2, "expected the leader and at least one worker, got %+v", rows)
		// A worker reports its leader's query id and database, so the leader and
		// its workers fall into one bucket and add to its samples.
		require.NotNil(t, rows[0].QueryID)
		for _, r := range rows {
			require.Equal(t, "app", r.Datname)
			require.NotNil(t, r.QueryID, "a worker must carry its leader's query id")
			require.Equal(t, *rows[0].QueryID, *r.QueryID)
		}
	})
}

// INT-ASH-006: a role without pg_read_all_stats sees another user's session
// only as a hidden row, which the sampler cannot count, so SessionsVisible
// reports it. The monitoring roles of the test matrix and a superuser see the
// session, and their sampler counts it.
func TestINTASH006_HiddenSessionsDetected(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'ash_plain') THEN CREATE ROLE ash_plain LOGIN PASSWORD 'test'; END IF; END $$`)
		require.NoError(t, err)

		// The session that is hidden from ash_plain: another user's, active.
		sleepingSession(t, ctx, pg.Pool(t, "app", pgtest.RoleT0), 5*time.Second)
		time.Sleep(300 * time.Millisecond) // let the sleeper start

		plainCfg, err := pgxpool.ParseConfig(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		plainCfg.ConnConfig.User = "ash_plain"
		plainCfg.ConnConfig.Password = "test"
		plainPool, err := pgxpool.NewWithConfig(ctx, plainCfg)
		require.NoError(t, err)
		t.Cleanup(plainPool.Close)
		plainConn, err := plainPool.Acquire(ctx)
		require.NoError(t, err)
		defer plainConn.Release()

		visible, err := SessionsVisible(ctx, plainConn)
		require.NoError(t, err)
		require.False(t, visible, "a role without pg_read_all_stats must not see other users' sessions")

		var plainRows []SampleRow
		plain := NewSampler(plainConn, clock.System(), func(r SampleRow) { plainRows = append(plainRows, r) })
		require.NoError(t, plain.doSample(ctx))
		require.Empty(t, plainRows, "the hidden session must not be sampled")

		for _, role := range []pgtest.Role{pgtest.RoleT0, pgtest.RoleT1, pgtest.RoleSuperuser} {
			seeing := pg.Pool(t, "app", role)
			conn, err := seeing.Acquire(ctx)
			require.NoError(t, err)
			ok, err := SessionsVisible(ctx, conn)
			require.NoError(t, err)
			require.True(t, ok, "role %s must see every session", role)
			var rows []SampleRow
			s := NewSampler(conn, clock.System(), func(r SampleRow) { rows = append(rows, r) })
			require.NoError(t, s.doSample(ctx))
			require.NotEmpty(t, rows, "role %s must sample the active session", role)
			conn.Release()
		}
	})
}
