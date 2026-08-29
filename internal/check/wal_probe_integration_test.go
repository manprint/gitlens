//go:build integration

package check

import (
	"context"
	"sort"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-WAL-000 records the live WAL/statistics view shape before the WAL check
// is implemented. The exact assertions are intentionally version-specific.
func TestINTWAL000_WALStatisticsColumnShape(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		ctx := context.Background()
		columns := func(view string) []string {
			rows, err := pool.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='pg_catalog' AND table_name=$1 ORDER BY column_name`, view)
			require.NoError(t, err)
			defer rows.Close()
			var got []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				got = append(got, name)
			}
			require.NoError(t, rows.Err())
			sort.Strings(got)
			return got
		}
		wal := columns("pg_stat_wal")
		io := columns("pg_stat_io")
		t.Logf("PG%d pg_stat_wal columns: %v", pg.Version/10000, wal)
		t.Logf("PG%d pg_stat_io columns: %v", pg.Version/10000, io)
		if pg.Version < 180000 {
			require.ElementsMatch(t, []string{"stats_reset", "wal_buffers_full", "wal_bytes", "wal_fpi", "wal_records", "wal_sync", "wal_sync_time", "wal_write", "wal_write_time"}, wal)
			require.Empty(t, io)
			return
		}
		require.ElementsMatch(t, []string{"stats_reset", "wal_buffers_full", "wal_bytes", "wal_fpi", "wal_records"}, wal)
		require.ElementsMatch(t, []string{"backend_type", "context", "evictions", "extend_bytes", "extend_time", "extends", "fsync_time", "fsyncs", "hits", "object", "read_bytes", "read_time", "reads", "reuses", "stats_reset", "write_bytes", "write_time", "writeback_time", "writebacks", "writes"}, io)
	})
}
