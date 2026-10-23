//go:build integration

package check

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func scrapeAsT0(t *testing.T, pg *pgtest.PG, c Check, role pgtype.Role) Result {
	t.Helper()
	conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
	require.NoError(t, err)
	result, err := c.Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", role: role, permTier: pgtype.TierReadOnly})
	require.NoError(t, err)
	return result
}

// INT-INT-001: at T0 on a primary, slru reports the eight caches under the
// names of 17 and later on every version, wal_directory sizes pg_wal,
// database_size sizes each database but the templates, database_stats
// counts temporary files, and recovery reports nothing.
func TestINTINT001_InternalsOnPrimary(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		slru := scrapeAsT0(t, pg, &slruCheck{}, pgtype.RolePrimary)
		caches := map[string]bool{}
		for _, m := range slru.Metrics {
			if m.Name == "pg_slru_blks_hit_total" {
				caches[m.Labels["slru"]] = true
			}
		}
		require.Equal(t, map[string]bool{
			"commit_timestamp": true, "multixact_member": true, "multixact_offset": true, "notify": true,
			"serializable": true, "subtransaction": true, "transaction": true, "other": true,
		}, caches)

		dir := scrapeAsT0(t, pg, &walDirectoryCheck{}, pgtype.RolePrimary)
		require.GreaterOrEqual(t, metricValue(dir, "pg_wal_dir_files", nil), 1.0)
		require.GreaterOrEqual(t, metricValue(dir, "pg_wal_dir_bytes", nil), float64(1<<20))

		require.Empty(t, scrapeAsT0(t, pg, &recoveryCheck{}, pgtype.RolePrimary).Metrics)

		sizes := scrapeAsT0(t, pg, &databaseSizeCheck{}, pgtype.RolePrimary)
		require.Greater(t, metricValue(sizes, "pg_database_size_bytes", map[string]string{"database": "app"}), float64(1<<20))
		require.Equal(t, -1.0, metricValue(sizes, "pg_database_size_bytes", map[string]string{"database": "template1"}))

		// A sort larger than work_mem writes a temporary file.
		admin, err := pg.Pool(t, "app", pgtest.RoleSuperuser).Acquire(ctx)
		require.NoError(t, err)
		defer admin.Release()
		before := metricValue(scrapeAsT0(t, pg, &databaseStatsCheck{}, pgtype.RolePrimary), "pg_temp_files_total", map[string]string{"database": "app"})
		require.GreaterOrEqual(t, before, 0.0)
		_, err = admin.Exec(ctx, "SET work_mem = '64kB'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT g FROM generate_series(1, 200000) g ORDER BY g DESC")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT pg_stat_force_next_flush()")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			after := metricValue(scrapeAsT0(t, pg, &databaseStatsCheck{}, pgtype.RolePrimary), "pg_temp_files_total", map[string]string{"database": "app"})
			return after > before
		}, 10*time.Second, 200*time.Millisecond, "the sort's temporary file was not counted")
	})
}

// INT-REC-001: on a standby, recovery reports the conflict counters of each
// database, the prefetcher and whether replay is paused; a standby query
// cancelled by a lock it blocks on the primary counts as a lock conflict.
func TestINTREC001_RecoveryOnStandby(t *testing.T) {
	for _, major := range pgtest.Versions() {
		t.Run("pg"+strconv.Itoa(major), func(t *testing.T) {
			ctx := context.Background()
			primary, standby := pgtest.PrimaryStandby(t, major)
			primaryAdmin := primary.Pool(t, "app", pgtest.RoleSuperuser)
			standbyAdmin := standby.Pool(t, "app", pgtest.RoleSuperuser)

			running := scrapeAsT0(t, standby, &recoveryCheck{}, pgtype.RoleStandby)
			require.Equal(t, 0.0, metricValue(running, "pg_recovery_replay_paused", nil))
			require.Equal(t, 0.0, metricValue(running, "pg_recovery_conflicts_total", map[string]string{"database": "app", "reason": "lock"}))
			require.GreaterOrEqual(t, metricValue(running, "pg_recovery_prefetch_blocks_total", map[string]string{"outcome": "hit"}), 0.0)

			_, err := standbyAdmin.Exec(ctx, "SELECT pg_wal_replay_pause()")
			require.NoError(t, err)
			paused := scrapeAsT0(t, standby, &recoveryCheck{}, pgtype.RoleStandby)
			require.Equal(t, 1.0, metricValue(paused, "pg_recovery_replay_paused", nil))
			_, err = standbyAdmin.Exec(ctx, "SELECT pg_wal_replay_resume()")
			require.NoError(t, err)

			// Conflicting standby queries are cancelled after 100 ms instead
			// of the default 30 s.
			_, err = standbyAdmin.Exec(ctx, "ALTER SYSTEM SET max_standby_streaming_delay = '100ms'")
			require.NoError(t, err)
			_, err = standbyAdmin.Exec(ctx, "SELECT pg_reload_conf()")
			require.NoError(t, err)
			_, err = primaryAdmin.Exec(ctx, "CREATE TABLE rec_conflict (id int); INSERT INTO rec_conflict VALUES (1)")
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				var n int
				return standbyAdmin.QueryRow(ctx, "SELECT count(*) FROM rec_conflict").Scan(&n) == nil && n == 1
			}, 30*time.Second, 200*time.Millisecond, "the table did not reach the standby")

			holder, err := standbyAdmin.Acquire(ctx)
			require.NoError(t, err)
			defer holder.Release()
			tx, err := holder.Begin(ctx)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "SELECT count(*) FROM rec_conflict")
			require.NoError(t, err)
			// TRUNCATE takes an ACCESS EXCLUSIVE lock, which the standby
			// replays and which conflicts with the open reader.
			_, err = primaryAdmin.Exec(ctx, "TRUNCATE rec_conflict")
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				r := scrapeAsT0(t, standby, &recoveryCheck{}, pgtype.RoleStandby)
				return metricValue(r, "pg_recovery_conflicts_total", map[string]string{"database": "app", "reason": "lock"}) >= 1
			}, 30*time.Second, 300*time.Millisecond, "the cancelled reader was not counted as a lock conflict")
			_ = tx.Rollback(ctx)
		})
	}
}
