package check

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func partitionRow(table, strategy string, defaultTable string, defaultRows float64, invalid []string, analyzed int64, covered *time.Time, forever bool) []any {
	defaultSchema := ""
	if defaultTable != "" {
		defaultSchema = "public"
	}
	return []any{"public." + table, "public", table, strategy, "RANGE (at)",
		int64(3), int64(1), int64(8192 * 10), float64(1000), int64(3),
		defaultSchema, defaultTable, defaultRows, invalid, int64(0), analyzed, covered, forever}
}

func scrapePartitionsForTest(t *testing.T, rows [][]any) Result {
	t.Helper()
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		require.Equal(t, partitionsQuery, sql)
		require.Equal(t, []any{partitionedTableFactLimit + 1}, args)
		return &mockRows{rows: rows}, nil
	}}
	r, err := (&partitionsCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func TestPartitions_Requirements(t *testing.T) {
	c := &partitionsCheck{}
	require.Equal(t, "partitions", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 10*time.Minute, c.DefaultInterval())
}

// The query must not call a function that locks the tables it reads: one
// partition held by ALTER TABLE would make the whole check wait and fail.
func TestPartitions_QueryTakesNoTableLock(t *testing.T) {
	locking := regexp.MustCompile(`pg_partition_tree|pg_partition_root|pg_partition_ancestors|_size\(|pg_get_partkeydef|pg_get_expr\([^)]*, (pc|c)\.oid\)`)
	require.False(t, locking.MatchString(partitionsQuery), locking.FindString(partitionsQuery))
	require.False(t, locking.MatchString(fkUnindexedQuery), locking.FindString(fkUnindexedQuery))
	require.False(t, locking.MatchString(postgisColumnsQuery), locking.FindString(postgisColumnsQuery))
}

func TestPartitions_ReportsTrees(t *testing.T) {
	until := time.Date(2027, 7, 1, 0, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	r := scrapePartitionsForTest(t, [][]any{
		partitionRow("events", "range", "events_def", 12, []string{"ev_uq"}, 1_780_000_000, &until, false),
		partitionRow("logs", "list", "", 0, []string{}, 0, nil, false),
		partitionRow("forever", "range", "", 0, []string{}, 0, nil, true),
	})
	require.False(t, r.Truncated)
	require.Len(t, r.Facts, 3)
	events := r.Facts[0]
	require.Equal(t, partitionedTableFactKind, events.Kind)
	require.Equal(t, "public.events", events.Key)
	require.Equal(t, "range", events.ValueText)
	require.Equal(t, "public.events_def", events.Labels["default_partition"])
	require.Equal(t, "events_def", events.Labels["default_relname"])
	require.Equal(t, "12", events.Labels["default_rows"])
	require.Equal(t, "ev_uq", events.Labels["invalid_indexes"])
	require.Equal(t, "2027-06-30T22:00:00Z", events.Labels["covered_until"])
	require.Equal(t, "2026-05-28T20:26:40Z", events.Labels["analyzed_at"])
	require.Equal(t, "81920", events.Labels["bytes"])

	logs := r.Facts[1]
	require.Empty(t, logs.Labels["default_partition"])
	require.Empty(t, logs.Labels["default_rows"])
	require.Empty(t, logs.Labels["analyzed_at"])
	require.Empty(t, logs.Labels["covered_until"])
	require.Equal(t, "unbounded", r.Facts[2].Labels["covered_until"])

	require.ElementsMatch(t, []pgtype.Metric{
		{Name: "pg_partitioned_tables", Value: 3, Kind: pgtype.KindGauge},
		{Name: "pg_partitions", Value: 9, Kind: pgtype.KindGauge},
		{Name: "pg_partitioned_tables_default_rows", Value: 1, Kind: pgtype.KindGauge},
		{Name: "pg_partitioned_indexes_invalid", Value: 1, Kind: pgtype.KindGauge},
	}, r.Metrics)
}

func TestPartitions_Truncates(t *testing.T) {
	var rows [][]any
	for range partitionedTableFactLimit + 1 {
		rows = append(rows, partitionRow("t", "hash", "", 0, nil, 0, nil, false))
	}
	r := scrapePartitionsForTest(t, rows)
	require.True(t, r.Truncated)
	require.Len(t, r.Facts, partitionedTableFactLimit)
}
