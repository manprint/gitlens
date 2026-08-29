package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/clock"
	"github.com/stretchr/testify/require"
)

func TestArchiver_FailedRatioWhenLastFailedIsNewer(t *testing.T) {
	now := time.Now()
	require.Equal(t, 1.0, archiverFailedRatio(&now, func() *time.Time { v := now.Add(time.Second); return &v }()))
}

func TestArchiver_FailedRatioZeroWhenLastArchivedIsNewer(t *testing.T) {
	now := time.Now()
	require.Equal(t, 0.0, archiverFailedRatio(func() *time.Time { v := now.Add(time.Second); return &v }(), &now))
}

func TestArchiver_OmitsAgesWhenNull(t *testing.T) {
	r := scrapeArchiverForTest(t, "on", []any{1.0, nil, nil, 0.0, nil, nil, nil}, nil)
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_last_archived_age_seconds", nil))
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_last_failed_age_seconds", nil))
}

func TestArchiver_ArchiveModeOffEmitsOnlyTheFlag(t *testing.T) {
	r := scrapeArchiverForTest(t, "off", nil, nil)
	require.Len(t, r.Metrics, 1)
	require.Equal(t, "pg_archive_mode_enabled", r.Metrics[0].Name)
}

func TestArchiver_StatsResetIsPropagated(t *testing.T) {
	reset := time.Now().Add(-time.Hour)
	r := scrapeArchiverForTest(t, "on", []any{2.0, nil, nil, 1.0, nil, nil, &reset}, nil)
	require.NotNil(t, r.StatsReset)
}

func TestArchiver_BasebackupRatioOmittedWhenTotalZero(t *testing.T) {
	r := scrapeArchiverForTest(t, "on", []any{0.0, nil, nil, 0.0, nil, nil, nil}, [][]any{{int32(1), "streaming", float64(0), float64(0), float64(0), float64(0)}})
	require.Equal(t, -1.0, metricValue(r, "pg_basebackup_progress_ratio", map[string]string{"phase": "streaming"}))
	require.Equal(t, 1.0, metricValue(r, "pg_basebackup_jobs_running", nil))
}

func scrapeArchiverForTest(t *testing.T, mode string, stats []any, progress [][]any) Result {
	t.Helper()
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "archive_mode"):
				return &mockRow{vals: []any{mode}}
			default:
				return &mockRow{vals: stats}
			}
		},
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: progress}, nil
		},
	}
	r, err := (&archiverCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.NoError(t, err)
	return r
}
