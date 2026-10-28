//go:build integration

package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// perDatabaseTarget answers ConnFor with a connection to the named database,
// as the agent does, so the maintenance connection and the database a vacuum
// runs in are different.
type perDatabaseTarget struct {
	registryTestTarget
	pools map[string]*pgxpool.Pool
}

func (m *perDatabaseTarget) ConnFor(ctx context.Context, datname string) (Conn, error) {
	conn, err := m.pools[datname].Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// MonitorsDatabase collects from exactly the databases it has pools for, as the
// agent does for its monitored set.
func (m *perDatabaseTarget) MonitorsDatabase(datname string) bool {
	_, ok := m.pools[datname]
	return ok
}

func TestINTVAC001_NoRunningVacuumReportsZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		r, err := (&vacuumProgressCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running", Kind: pgtype.KindGauge, Value: 0})
	})
}
func TestINTVAC002_StableMetricNamesAcrossVersions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		r, err := (&vacuumProgressCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		names := map[string]bool{}
		for _, m := range r.Metrics {
			names[m.Name] = true
		}
		require.True(t, names["pg_vacuum_jobs_running"])
		require.True(t, names["pg_analyze_jobs_running"])
	})
}

// TestINTVAC003_VacuumInAnotherDatabaseIsNamed: a vacuum in a database other
// than the maintenance one is reported under its table's name. The name is
// read from the database the vacuum runs in; the maintenance database's
// pg_class does not know the table's OID, which left the name empty.
func TestINTVAC003_VacuumInAnotherDatabaseIsNamed(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		const datname = "vacprobe"
		admin := pg.Pool(t, datname, pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, `DROP TABLE IF EXISTS zz_vac_probe;
CREATE TABLE zz_vac_probe AS SELECT g AS id, repeat('x', 200) AS pad FROM generate_series(1, 100000) g;
DELETE FROM zz_vac_probe WHERE id % 2 = 0`)
		require.NoError(t, err)

		// The vacuum is throttled so that it is still running while the check
		// looks at it. It is cancelled once the check has seen it.
		vac, err := admin.Acquire(ctx)
		require.NoError(t, err)
		_, err = vac.Exec(ctx, "SET vacuum_cost_delay = 20; SET vacuum_cost_limit = 1")
		require.NoError(t, err)
		var vacPID int32
		require.NoError(t, vac.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&vacPID))
		vacCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			// The error is the cancellation this test asks for.
			_, _ = vac.Exec(vacCtx, "VACUUM zz_vac_probe")
			vac.Release()
		}()
		t.Cleanup(func() {
			cancel()
			<-done
			stopVacuum(t, admin, vacPID)
		})

		maintenance := pg.Pool(t, "app", pgtest.RoleT0)
		target := &perDatabaseTarget{pools: map[string]*pgxpool.Pool{datname: pg.Pool(t, datname, pgtest.RoleT0)}}
		deadline := time.Now().Add(30 * time.Second)
		relname, seen := "", false
		for !seen {
			conn, err := maintenance.Acquire(ctx)
			require.NoError(t, err)
			target.registryTestTarget = registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly}
			r, err := (&vacuumProgressCheck{}).Scrape(ctx, target)
			require.NoError(t, err)
			for _, m := range r.Metrics {
				if m.Name == "pg_vacuum_heap_blks_total" && m.Labels["datname"] == datname {
					relname, seen = m.Labels["relname"], true
				}
			}
			require.True(t, time.Now().Before(deadline), "the vacuum was never seen")
			time.Sleep(200 * time.Millisecond)
		}
		require.Equal(t, "zz_vac_probe", relname)
	})
}

// stopVacuum cancels the vacuum backend pid on the server and waits until it
// has stopped. When a query's context is cancelled, pgx returns at once and
// sends the cancel request from a background goroutine, so the throttled probe
// vacuum can still be running when its test ends. The snapshot restore resets
// only the app database and leaves it alone, and a later test sees it running:
// TestINTVAC001 failed that way when -shuffle ran it after TestINTVAC003.
func stopVacuum(t *testing.T, pool *pgxpool.Pool, pid int32) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var running int
		require.NoError(t, pool.QueryRow(ctx,
			"SELECT count(pg_cancel_backend(pid)) FROM pg_stat_progress_vacuum WHERE pid = $1", pid).Scan(&running))
		if running == 0 {
			return
		}
		require.True(t, time.Now().Before(deadline), "the probe vacuum did not stop")
		time.Sleep(100 * time.Millisecond)
	}
}
