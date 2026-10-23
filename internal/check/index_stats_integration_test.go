//go:build integration

package check

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTIDX001_IndexDefinitionHashes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_idx_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer,b integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_a ON %s(a)", table, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_a2 ON %s(a)", table, table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		hashes := map[string]bool{}
		for _, f := range result.Facts {
			if f.Labels["relname"] == table {
				hashes[f.Labels["def_hash"]] = true
			}
		}
		require.Len(t, hashes, 1)
	})
}

func TestINTIDX002_IndexStatsUnusedStartsAtZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_unused_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_i ON %s(a)", table, table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		found := false
		for _, m := range result.Metrics {
			if m.Name == "pg_index_idx_scan" && m.Labels["relname"] == table {
				found = true
				require.Equal(t, float64(0), m.Value)
			}
		}
		require.True(t, found)
	})
}

func TestINTIDX003_InvalidIndexReportsZero(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_invalid_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s_i ON %s(a)", table, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("UPDATE pg_index SET indisvalid=false WHERE indexrelid='%s_i'::regclass", table))
		require.NoError(t, err)
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.NotEmpty(t, result.Metrics)
	})
}

// TestINTIDX004_IndexStatsSkipsLockedIndex: an index another session holds
// under ACCESS EXCLUSIVE cannot have its size read. The scrape must not fail
// for it; the other index is still reported and the result is partial.
func TestINTIDX004_IndexStatsSkipsLockedIndex(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		suffix := testSuffix()
		table := fmt.Sprintf("pglens_idx_t_%d", suffix)
		locked := fmt.Sprintf("pglens_idx_locked_%d", suffix)
		open := fmt.Sprintf("pglens_idx_open_%d", suffix)
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,1000)", table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(a)", locked, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(a)", open, table))
		require.NoError(t, err)
		// Locking the table does not lock its indexes; REINDEX INDEX inside a
		// transaction holds ACCESS EXCLUSIVE on the index until the rollback.
		holdLock(t, admin, fmt.Sprintf("REINDEX INDEX %s", locked))
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		indexes := map[string]bool{}
		for _, f := range result.Facts {
			if f.Labels["relname"] == table {
				indexes[f.Labels["indexrelname"]] = true
			}
		}
		require.True(t, indexes[open])
		require.False(t, indexes[locked])
		require.True(t, result.Partial)
	})
}

// TestINTIDX005_IndexStatsSkipsOtherSessionsTempIndexes: an index on another
// session's temporary table is listed by pg_stat_user_indexes under its
// pg_temp_N schema. It lasts only as long as that session, so it is not an
// index to report.
func TestINTIDX005_IndexStatsSkipsOtherSessionsTempIndexes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		// The read-only monitoring role cannot create a temporary table, so the
		// other session is the superuser; the check itself runs as the role.
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_tmp_idx_%d", testSuffix())
		index := table + "_id"
		// The temporary table and its index belong to this session, which stays
		// open while the check runs.
		session, err := admin.Acquire(ctx)
		require.NoError(t, err)
		defer session.Release()
		_, err = session.Exec(ctx, fmt.Sprintf("CREATE TEMP TABLE %s (id integer)", table))
		require.NoError(t, err)
		_, err = session.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(id)", index, table))
		require.NoError(t, err)
		_, err = session.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT generate_series(1,100)", table))
		require.NoError(t, err)

		// The session's statistics reach the shared views only when it flushes
		// them, so wait until the index is visible before checking the filter.
		observer, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer observer.Release()
		require.Eventually(t, func() bool {
			var seen int
			return observer.QueryRow(ctx, "SELECT count(*) FROM pg_stat_user_indexes WHERE indexrelname = $1", index).Scan(&seen) == nil && seen > 0
		}, 30*time.Second, 100*time.Millisecond)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		for _, m := range result.Metrics {
			require.False(t, strings.HasPrefix(m.Labels["schemaname"], "pg_temp"), "temporary index %s of another session was reported", m.Labels["indexrelname"])
		}
		for _, f := range result.Facts {
			require.False(t, strings.HasPrefix(f.Labels["schemaname"], "pg_temp"), "temporary index %s of another session was reported", f.Labels["indexrelname"])
		}
	})
}

// TestINTIDX006_LastScanAndResetLabels: from PostgreSQL 16 the index_def fact
// says when each index was last scanned since the statistics reset. An index
// scanned after the reset carries that time, an index never scanned carries an
// empty label, and both carry the reset time. On 19 an index's own reset
// (pg_stat_user_indexes.stats_reset) replaces the database's when it is later.
// On 15 the labels are absent.
func TestINTIDX006_LastScanAndResetLabels(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		table := fmt.Sprintf("pglens_lastscan_%d", testSuffix())
		used := table + "_used"
		unused := table + "_unused"
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (a integer, b integer)", table))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", table)) })
		_, err = admin.Exec(ctx, fmt.Sprintf("INSERT INTO %s SELECT g, g FROM generate_series(1,1000) g", table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(a)", used, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX %s ON %s(b)", unused, table))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "ANALYZE "+table)
		require.NoError(t, err)

		// The database's statistics are reset first, then only the used index is
		// scanned. The scan and its flush run on one session, so the flush covers it.
		_, err = admin.Exec(ctx, "SELECT pg_stat_reset()")
		require.NoError(t, err)
		resetAt := time.Now()
		session, err := admin.Acquire(ctx)
		require.NoError(t, err)
		_, err = session.Exec(ctx, "SET enable_seqscan = off")
		require.NoError(t, err)
		_, err = session.Exec(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE a = 1", table))
		require.NoError(t, err)
		_, err = session.Exec(ctx, "SELECT pg_stat_force_next_flush()")
		require.NoError(t, err)
		session.Release()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()
		if pg.Version >= pgtype.PG19 {
			// An index's own reset is later than the database's: the label must follow it.
			_, err = admin.Exec(ctx, fmt.Sprintf("SELECT pg_stat_reset_single_table_counters('%s'::regclass)", unused))
			require.NoError(t, err)
		}
		indexResetAt := time.Now()
		result, err := (&indexStatsCheck{selectors: cardinalityForIntegration()}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		labels := map[string]map[string]string{}
		for _, f := range result.Facts {
			if f.Labels["relname"] == table {
				labels[f.Labels["indexrelname"]] = f.Labels
			}
		}
		require.Contains(t, labels, used)
		require.Contains(t, labels, unused)
		if pg.Version < pgtype.PG16 {
			require.NotContains(t, labels[used], "last_idx_scan")
			require.NotContains(t, labels[used], "stats_reset")
			return
		}
		const slack = 5 * time.Second
		usedAt := parseLabelTime(t, labels[used]["last_idx_scan"])
		require.False(t, usedAt.IsZero(), "the scanned index has no last scan")
		require.WithinDuration(t, resetAt, usedAt, slack)
		require.Empty(t, labels[unused]["last_idx_scan"], "an index never scanned since the reset must have an empty last scan")
		require.WithinDuration(t, resetAt, parseLabelTime(t, labels[used]["stats_reset"]), slack)
		if pg.Version >= pgtype.PG19 {
			require.WithinDuration(t, indexResetAt, parseLabelTime(t, labels[unused]["stats_reset"]), slack)
		} else {
			require.WithinDuration(t, resetAt, parseLabelTime(t, labels[unused]["stats_reset"]), slack)
		}
	})
}

// parseLabelTime reads a timestamp label; an empty label is the zero time.
func parseLabelTime(t *testing.T, value string) time.Time {
	t.Helper()
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}
