package check

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestCheckpointer_PG16UsesBgwriter(t *testing.T) {
	r := scrapeCheckpointerForTest(t, pgtype.PG16)
	require.Equal(t, 1.0, metricValue(r, "pg_checkpoints_timed_total", nil))
}

func TestCheckpointer_PG17UsesCheckpointer(t *testing.T) {
	r := scrapeCheckpointerForTest(t, pgtype.PG17)
	require.Equal(t, 2.0, metricValue(r, "pg_checkpoints_requested_total", nil))
}

func TestCheckpointer_MetricNamesIdenticalAcrossVersions(t *testing.T) {
	old := checkpointerMetricNames(scrapeCheckpointerForTest(t, pgtype.PG16))
	new := checkpointerMetricNames(scrapeCheckpointerForTest(t, pgtype.PG17))
	require.Equal(t, old, new)
}

func checkpointerMetricNames(result Result) []string {
	names := make([]string, 0, len(result.Metrics))
	for _, metric := range result.Metrics {
		names = append(names, metric.Name)
	}
	sort.Strings(names)
	return names
}

func TestCheckpointer_AllCountersAreCounterKind(t *testing.T) {
	r := scrapeCheckpointerForTest(t, pgtype.PG18)
	for _, metric := range r.Metrics {
		require.Equal(t, pgtype.KindCounter, metric.Kind, metric.Name)
	}
}

// A reset of pg_stat_bgwriter is reported as the result's reset time up to
// PostgreSQL 16. From 17 no reset time is reported, since the checkpoint
// counters reset separately from the bgwriter ones.
func TestCheckpointer_ReportsBgwriterResetBefore17(t *testing.T) {
	reset := time.Now().Add(-time.Hour)
	r := scrapeCheckpointerWithReset(t, pgtype.PG16, &reset)
	require.NotNil(t, r.StatsReset)
	require.True(t, reset.Equal(*r.StatsReset))
}

func TestCheckpointer_NoResetTimeFrom17(t *testing.T) {
	reset := time.Now().Add(-time.Hour)
	r := scrapeCheckpointerWithReset(t, pgtype.PG17, &reset)
	require.Nil(t, r.StatsReset)
}

func scrapeCheckpointerForTest(t *testing.T, version pgtype.PGVersion) Result {
	t.Helper()
	return scrapeCheckpointerWithReset(t, version, nil)
}

func scrapeCheckpointerWithReset(t *testing.T, version pgtype.PGVersion, reset *time.Time) Result {
	t.Helper()
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) pgx.Row {
			if strings.Contains(query, "pg_stat_checkpointer") {
				return &mockRow{vals: []any{float64(1), float64(2), float64(3), float64(4)}}
			}
			if strings.Contains(query, "checkpoints_timed") {
				return &mockRow{vals: []any{float64(1), float64(2), float64(3), float64(4), reset}}
			}
			return &mockRow{vals: []any{float64(5), float64(6), float64(7)}}
		},
	}
	r, err := (&checkpointerCheck{}).Scrape(context.Background(), &SimpleTarget{
		PGVersionValue: version, ConnFunc: func(context.Context) (Conn, error) { return conn, nil },
	})
	require.NoError(t, err)
	return r
}
