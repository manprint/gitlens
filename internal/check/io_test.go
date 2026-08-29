package check

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestIO_RequiresPG16(t *testing.T) {
	require.Equal(t, pgtype.PG16, (&ioCheck{}).Requires().MinPG)
	allowed, reason := (&ioCheck{}).Requires().Supports(pgtype.RolePrimary, pgtype.PG15, pgtype.TierReadOnly, nil)
	require.False(t, allowed)
	require.Contains(t, reason, "requires PG >=")
}

func TestIO_EmitsLabeledCountersAndTimingGauge(t *testing.T) {
	conn := &mockConn{
		queryFunc: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(query, "information_schema") {
				return &mockRows{rows: [][]any{{"backend_type"}, {"object"}, {"context"}, {"reads"}, {"read_bytes"}, {"writes"}, {"write_bytes"}, {"extends"}, {"hits"}, {"evictions"}, {"fsyncs"}, {"read_time"}, {"write_time"}, {"fsync_time"}}}, nil
			}
			return &mockRows{rows: [][]any{{"client backend", "relation", "normal", "2", "4096", "3", "8192", "1", "4", "0", "0", "0", "0", "0"}}}, nil
		},
		queryRowFunc: func(context.Context, string, ...any) pgx.Row { return &mockRow{vals: []any{true}} },
	}
	r, err := (&ioCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1.0, metricValue(r, "pg_io_timing_enabled", nil))
	require.Equal(t, 2.0, metricValue(r, "pg_io_reads", map[string]string{"backend_type": "client backend", "object": "relation", "context": "normal"}))
	require.Equal(t, pgtype.KindCounter, metricKind(r, "pg_io_read_bytes"))
}

func TestIO_SkipsAbsentColumns(t *testing.T) {
	conn := &mockConn{
		queryFunc: func(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
			if strings.Contains(query, "information_schema") {
				return &mockRows{rows: [][]any{{"backend_type"}, {"object"}, {"context"}, {"reads"}}}, nil
			}
			return &mockRows{rows: [][]any{{"client", "relation", "normal", "1"}}}, nil
		},
		queryRowFunc: func(context.Context, string, ...any) pgx.Row { return &mockRow{vals: []any{false}} },
	}
	r, err := (&ioCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1.0, metricValue(r, "pg_io_reads", map[string]string{"backend_type": "client", "object": "relation", "context": "normal"}))
}
