package check

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestWAL_UsesReplayLSNOnStandby(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"stats_reset", "wal_bytes"})
	require.Equal(t, 32.0, metricValue(r, "pg_wal_lsn_bytes", nil))
}

func TestWAL_UsesCurrentLSNOnPrimary(t *testing.T) {
	r := scrapeWALForTest(t, "0/40", 64, []string{"stats_reset", "wal_records"})
	require.Equal(t, 64.0, metricValue(r, "pg_wal_lsn_bytes", nil))
}

func TestWAL_LSNIsACounter(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"wal_bytes"})
	for _, metric := range r.Metrics {
		if metric.Name == "pg_wal_lsn_bytes" {
			require.Equal(t, pgtype.KindCounter, metric.Kind)
			return
		}
	}
	t.Fatal("pg_wal_lsn_bytes was not emitted")
}

func TestWAL_ProbesColumnsOnce(t *testing.T) {
	calls := 0
	conn := walMockConn(&calls, []string{"wal_records"})
	_, err := (&walCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 2, calls) // probe and statistics query; LSN uses QueryRow
}

func TestWAL_SkipsAbsentColumns(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"wal_bytes"})
	require.NotEmpty(t, r.Metrics)
	require.Equal(t, 1.0, metricValue(r, "pg_wal_stats_columns_available", nil))
	require.Equal(t, -1.0, metricValue(r, "pg_wal_wal_records", nil))
}

func TestWAL_EmitsColumnAvailabilityGauge(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"stats_reset", "wal_records", "wal_bytes"})
	require.Equal(t, 3.0, metricValue(r, "pg_wal_stats_columns_available", nil))
	require.Equal(t, 7.0, metricValue(r, "pg_wal_wal_records", nil))
	require.Equal(t, pgtype.KindCounter, metricKind(r, "pg_wal_wal_bytes"))
}

func scrapeWALForTest(t *testing.T, lsn string, lsnBytes float64, columns []string) Result {
	t.Helper()
	calls := 0
	conn := walMockConn(&calls, columns)
	conn.queryRowFunc = func(context.Context, string, ...any) pgx.Row {
		return &mockRow{vals: []any{lsn, lsnBytes}}
	}
	r, err := (&walCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	return r
}

func walMockConn(calls *int, columns []string) *mockConn {
	conn := &mockConn{
		queryFunc: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
			*calls++
			if strings.Contains(query, "information_schema") {
				rows := make([][]any, len(columns))
				for i, column := range columns {
					rows[i] = []any{column}
				}
				return &mockRows{rows: rows}, nil
			}
			if strings.Contains(query, "stats_reset") {
				return nil, errors.New("stats_reset is not a statistic")
			}
			var values []any
			for _, column := range columns {
				if column != "stats_reset" {
					values = append(values, "7")
				}
			}
			return &mockRows{rows: [][]any{values}}, nil
		},
	}
	conn.queryRowFunc = func(context.Context, string, ...any) pgx.Row {
		return &mockRow{vals: []any{"0/20", float64(32)}}
	}
	return conn
}

// TestWAL_StatsResetIsNotReadAsAStatistic: stats_reset is a timestamp. It
// was parsed as a number and logged as non-numeric on every scrape. It is not
// a reset hint either, since pg_wal_lsn_bytes does not restart with it.
func TestWAL_StatsResetIsNotReadAsAStatistic(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"wal_records", "stats_reset", "wal_bytes"})
	require.Nil(t, r.StatsReset)
	require.Equal(t, -1.0, metricValue(r, "pg_wal_stats_reset", nil))
	require.Equal(t, 7.0, metricValue(r, "pg_wal_wal_bytes", nil))
	require.Equal(t, 3.0, metricValue(r, "pg_wal_stats_columns_available", nil))
}

func TestWAL_CumulativeTimesAreCounters(t *testing.T) {
	r := scrapeWALForTest(t, "0/20", 32, []string{"wal_write_time", "wal_sync_time", "wal_buffers_full"})
	require.Equal(t, pgtype.KindCounter, metricKind(r, "pg_wal_wal_write_time"))
	require.Equal(t, pgtype.KindCounter, metricKind(r, "pg_wal_wal_sync_time"))
	require.Equal(t, pgtype.KindCounter, metricKind(r, "pg_wal_wal_buffers_full"))
}

func metricKind(result Result, name string) pgtype.MetricKind {
	for _, metric := range result.Metrics {
		if metric.Name == name {
			return metric.Kind
		}
	}
	return ""
}
