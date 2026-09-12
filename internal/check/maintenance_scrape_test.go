package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/stretchr/testify/require"
)

func maintenanceCardinality() *scopedSelectors {
	return newScopedSelectors(cardinality.Options{TopN: 50})
}

func maintenanceTarget(conn Conn) *SimpleTarget {
	return &SimpleTarget{
		ConnForFunc:   func(context.Context, string) (Conn, error) { return conn, nil },
		ConnFunc:      func(context.Context) (Conn, error) { return conn, nil },
		DatabaseValue: "app",
		ClockValue:    clock.System(),
	}
}

func TestTableStats_ScrapeCoversRows(t *testing.T) {
	now := time.Now()
	conn := &mockConn{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: [][]any{{
				"public", "t",
				int64(1), int64(2), int64(3), int64(4),
				int64(5), int64(6), int64(7), int64(8),
				int64(9), int64(10), int64(11),
				&now, nil, &now, nil,
				float64(1), float64(2), int64(3), float64(4),
				int64(5), int64(6), int64(7), int64(8),
			}}}, nil
		},
	}
	r, err := (&tableStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.NotEmpty(t, r.Metrics)
}

func TestIndexStats_ScrapeCoversRows(t *testing.T) {
	conn := &mockConn{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: [][]any{{
				"public", "t", "i", int64(1), int64(2), int64(3), int64(4),
				int64(5), int64(4096), true, false, true,
				"CREATE INDEX i ON t (a)",
			}}}, nil
		},
	}
	r, err := (&indexStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Len(t, r.Facts, 1)
}

func TestVacuumProgress_ScrapeCoversProbeAndViews(t *testing.T) {
	calls := 0
	conn := &mockConn{
		queryFunc: func(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
			calls++
			switch {
			case strings.Contains(q, "information_schema"):
				return &mockRows{rows: [][]any{{"max_dead_tuple_bytes"}, {"dead_tuple_bytes"}}}, nil
			case strings.Contains(q, "progress_vacuum"):
				return &mockRows{rows: [][]any{{
					int32(1), "app", "t", "scanning", int64(10), int64(2),
					int64(1), int64(0), int64(2), int64(1),
				}}}, nil
			default:
				return &mockRows{}, nil
			}
		},
	}
	r, err := (&vacuumProgressCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Greater(t, calls, 1)
	require.NotEmpty(t, r.Metrics)
}

func TestBloat_ScrapeCoversRows(t *testing.T) {
	conn := &mockConn{
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: [][]any{{
				"public", "t", "", "table", float64(2 << 20), float64(1 << 20),
				float64(1 << 20), 0.5, float64(100),
			}}}, nil
		},
	}
	r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.NotEmpty(t, r.Metrics)
}
