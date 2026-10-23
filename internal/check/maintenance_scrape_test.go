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
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "pg_stat_user_tables") {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{{
				"public", "t",
				int64(1), int64(2), int64(3), int64(4),
				int64(5), int64(6), int64(7), int64(8),
				int64(9), int64(10), int64(11),
				&now, nil, &now, nil,
				float64(1), float64(2), int64(3), float64(4),
				int64(5), int64(6), uint32(7),
			}}}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if sql == "SELECT now()" {
				return &mockRow{vals: []any{time.Now()}}
			}
			return &mockRow{vals: []any{int64(100), int64(80), int64(20)}}
		},
	}
	r, err := (&tableStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.NotEmpty(t, r.Metrics)
	require.Equal(t, 100.0, metricValue(r, "pg_table_total_bytes", map[string]string{"schemaname": "public", "relname": "t"}))
}

// TestTableStats_AgeIsMeasuredOnTheServerClock: the agent clock runs an hour
// ahead of the server. A table analyzed ten seconds ago must still read as ten
// seconds, not as an hour and ten seconds.
func TestTableStats_AgeIsMeasuredOnTheServerClock(t *testing.T) {
	serverNow := time.Now()
	analyzed := serverNow.Add(-10 * time.Second)
	conn := &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "pg_stat_user_tables") {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{{
				"public", "t",
				int64(1), int64(2), int64(3), int64(4),
				int64(5), int64(6), int64(7), int64(8),
				int64(9), int64(10), int64(11),
				nil, nil, &analyzed, nil,
				float64(1), float64(2), int64(3), float64(4),
				int64(5), int64(6), uint32(7),
			}}}, nil
		},
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if sql == "SELECT now()" {
				return &mockRow{vals: []any{serverNow}}
			}
			return &mockRow{vals: []any{int64(100), int64(80), int64(20)}}
		},
	}
	target := maintenanceTarget(conn)
	target.ClockValue = clock.NewFake(serverNow.Add(time.Hour))
	r, err := (&tableStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), target)
	require.NoError(t, err)
	require.InDelta(t, 10.0, metricValue(r, "pg_table_last_analyze_age_seconds", map[string]string{"schemaname": "public", "relname": "t"}), 1.0)
}

func TestIndexStats_ScrapeCoversRows(t *testing.T) {
	conn := &mockConn{
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "pg_stat_user_indexes") {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{{
				"public", "t", "i", int64(1), int64(2), int64(3), int64(4),
				int64(5), int64(4096), true, false, true, uint32(16384),
			}}}, nil
		},
		queryRowFunc: func(context.Context, string, ...any) pgx.Row {
			return &mockRow{vals: []any{int64(4096), "CREATE INDEX i ON t (a)"}}
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
					int32(1), "app", uint32(0), "scanning", int64(10), int64(2),
					int64(1), int64(0), int64(2), int64(1), nil,
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
		queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "pg_stats") {
				return &mockRows{}, nil
			}
			return &mockRows{rows: [][]any{{
				"public", "t", "", "table", float64(2 << 20), float64(1 << 20),
				float64(1 << 20), 0.5, float64(100), uint32(16384),
			}}}, nil
		},
		queryRowFunc: func(context.Context, string, ...any) pgx.Row {
			return &mockRow{vals: []any{int64(2 << 20)}}
		},
	}
	r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.NotEmpty(t, r.Metrics)
	require.Equal(t, float64(1<<20), metricValue(r, "pg_bloat_bytes", map[string]string{
		"schemaname": "public", "relname": "t", "indexrelname": "", "object_kind": "table", "method": "estimate",
	}))
}

// TestRelationSizes_VanishedRelationIsLeftOut: a relation dropped after the
// catalog read gets NULL from the size functions. The scrape must leave that
// relation out, report the others and keep the result partial, rather than
// fail on the NULL or publish a zero size.
func TestRelationSizes_VanishedRelationIsLeftOut(t *testing.T) {
	t.Parallel()
	vanished := func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch sql {
		case tableSizesQuery:
			return &mockRow{vals: []any{nil, nil, nil}}
		case indexDefinitionQuery:
			return &mockRow{vals: []any{nil, nil}}
		case bloatSizeQuery:
			return &mockRow{vals: []any{nil}}
		case "SELECT now()":
			return &mockRow{vals: []any{time.Now()}}
		}
		return &mockRow{vals: []any{int64(0)}}
	}
	t.Run("table_stats", func(t *testing.T) {
		now := time.Now()
		conn := &mockConn{
			queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "pg_stat_user_tables") {
					return &mockRows{}, nil
				}
				return &mockRows{rows: [][]any{{
					"public", "t",
					int64(1), int64(2), int64(3), int64(4),
					int64(5), int64(6), int64(7), int64(8),
					int64(9), int64(10), int64(11),
					&now, nil, &now, nil,
					float64(1), float64(2), int64(3), float64(4),
					int64(5), int64(6), uint32(7),
				}}}, nil
			},
			queryRowFunc: vanished,
		}
		r, err := (&tableStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, -1.0, metricValue(r, "pg_table_total_bytes", map[string]string{"schemaname": "public", "relname": "t"}))
		require.True(t, r.Partial)
	})
	t.Run("index_stats", func(t *testing.T) {
		conn := &mockConn{
			queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "pg_stat_user_indexes") {
					return &mockRows{}, nil
				}
				return &mockRows{rows: [][]any{{
					"public", "t", "i", int64(1), int64(2), int64(3), int64(4),
					int64(5), int64(4096), true, false, true, uint32(16384),
				}}}, nil
			},
			queryRowFunc: vanished,
		}
		r, err := (&indexStatsCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, -1.0, metricValue(r, "pg_index_index_bytes", map[string]string{"schemaname": "public", "relname": "t", "indexrelname": "i"}))
		require.Empty(t, r.Facts)
		require.True(t, r.Partial)
	})
	t.Run("bloat_estimate", func(t *testing.T) {
		conn := &mockConn{
			queryFunc: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "pg_stats") {
					return &mockRows{}, nil
				}
				return &mockRows{rows: [][]any{{
					"public", "t", "", "table", float64(2 << 20), float64(1 << 20),
					float64(1 << 20), 0.5, float64(100), uint32(16384),
				}}}, nil
			},
			queryRowFunc: vanished,
		}
		r, err := (&bloatEstimateCheck{selectors: maintenanceCardinality()}).Scrape(context.Background(), maintenanceTarget(conn))
		require.NoError(t, err)
		require.Equal(t, -1.0, metricValue(r, "pg_bloat_bytes", map[string]string{"schemaname": "public", "relname": "t", "indexrelname": "", "object_kind": "table", "method": "estimate"}))
		require.True(t, r.Partial)
	})
}
