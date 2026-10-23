package check

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	denied := &mockRow{err: &pgconn.PgError{Code: "42501", Message: "permission denied for function pg_ls_archive_statusdir"}}
	r := scrapeArchiverWithReady(t, "on", []any{1.0, nil, nil, 0.0, nil, nil, nil}, nil, denied)
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_last_archived_age_seconds", nil))
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_last_failed_age_seconds", nil))
}

// TestArchiver_NeverArchivedAgeStartsAtPostmaster: an archiver that has never
// archived still reports an age, from the server start, so that archive.stalled
// can judge the segments waiting behind it. Without it the stall was invisible.
func TestArchiver_NeverArchivedAgeStartsAtPostmaster(t *testing.T) {
	never := []any{0.0, nil, nil, 0.0, nil, nil, nil}
	r := scrapeArchiverWithReady(t, "on", never, nil, &mockRow{vals: []any{3.0}})
	require.Greater(t, metricValue(r, "pg_archiver_last_archived_age_seconds", nil), 3600.0)
	require.Equal(t, 3.0, metricValue(r, "pg_archiver_ready_files", nil))
}

// TestArchiver_NeverArchivedAgeStartsAtStatsReset: when the statistics were
// reset after the server started, the age runs from the reset, the later time.
func TestArchiver_NeverArchivedAgeStartsAtStatsReset(t *testing.T) {
	reset := time.Now().Add(-10 * time.Minute)
	r := scrapeArchiverWithReady(t, "on", []any{0.0, nil, nil, 0.0, nil, nil, &reset}, nil, &mockRow{vals: []any{3.0}})
	age := metricValue(r, "pg_archiver_last_archived_age_seconds", nil)
	require.GreaterOrEqual(t, age, 600.0)
	require.Less(t, age, 3600.0)
}

// TestArchiver_NeverArchivedAgeNeedsReadyFiles: without the waiting-segment
// count an idle server that never archived would read as stalled once it had
// been up for an hour, so no age is reported.
func TestArchiver_NeverArchivedAgeNeedsReadyFiles(t *testing.T) {
	never := []any{0.0, nil, nil, 0.0, nil, nil, nil}
	denied := &mockRow{err: &pgconn.PgError{Code: "42501", Message: "permission denied for function pg_ls_archive_statusdir"}}
	r := scrapeArchiverWithReady(t, "on", never, nil, denied)
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_last_archived_age_seconds", nil))
}

// A statement timeout on the archive directory listing is an answer from the
// server, not a broken connection: the archiver counters still report.
func TestArchiver_ReadyFilesTimeoutKeepsCounters(t *testing.T) {
	timeout := &mockRow{err: &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}}
	r := scrapeArchiverWithReady(t, "on", []any{5.0, nil, nil, 1.0, nil, nil, nil}, nil, timeout)
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_ready_files", nil))
	require.Equal(t, 5.0, metricValue(r, "pg_archiver_archived_total", nil))
	require.Equal(t, 1.0, metricValue(r, "pg_archiver_failed_total", nil))
}

func TestArchiver_ArchiveModeOffEmitsNoArchiverCounters(t *testing.T) {
	r := scrapeArchiverForTest(t, "off", nil, nil)
	require.Equal(t, 0.0, metricValue(r, "pg_archive_mode_enabled", nil))
	require.Equal(t, 0.0, metricValue(r, "pg_basebackup_jobs_running", nil))
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_archived_total", nil))
}

// TestArchiver_BasebackupReportedWithoutArchiving covers pg_basebackup -X
// stream, the default: a backup runs whatever archive_mode says.
func TestArchiver_BasebackupReportedWithoutArchiving(t *testing.T) {
	r := scrapeArchiverForTest(t, "off", nil, [][]any{{"streaming database files", float64(100), float64(25)}})
	require.Equal(t, 1.0, metricValue(r, "pg_basebackup_jobs_running", nil))
	require.Equal(t, 0.25, metricValue(r, "pg_basebackup_progress_ratio", map[string]string{"phase": "streaming database files"}))
}

func TestArchiver_StatsResetIsPropagated(t *testing.T) {
	reset := time.Now().Add(-time.Hour)
	r := scrapeArchiverForTest(t, "on", []any{2.0, nil, nil, 1.0, nil, nil, &reset}, nil)
	require.NotNil(t, r.StatsReset)
}

func TestArchiver_BasebackupRatioOmittedWhenTotalZero(t *testing.T) {
	r := scrapeArchiverForTest(t, "on", []any{0.0, nil, nil, 0.0, nil, nil, nil}, [][]any{{"streaming", float64(0), float64(0)}})
	require.Equal(t, -1.0, metricValue(r, "pg_basebackup_progress_ratio", map[string]string{"phase": "streaming"}))
	require.Equal(t, 1.0, metricValue(r, "pg_basebackup_jobs_running", nil))
}

// TestArchiver_ReportsSegmentsWaitingForArchive: the .ready files are what
// separates a stuck archiver from an idle server.
func TestArchiver_ReportsSegmentsWaitingForArchive(t *testing.T) {
	r := scrapeArchiverWithReady(t, "on", []any{1.0, nil, nil, 0.0, nil, nil, nil}, nil, &mockRow{vals: []any{3.0}})
	require.Equal(t, 3.0, metricValue(r, "pg_archiver_ready_files", nil))
}

// TestArchiver_ReadyFilesNeedPgMonitor: without the privilege the metric is
// left out and the rest of the scrape survives; any other error fails it.
func TestArchiver_ReadyFilesNeedPgMonitor(t *testing.T) {
	denied := &mockRow{err: &pgconn.PgError{Code: "42501", Message: "permission denied for function pg_ls_archive_statusdir"}}
	r := scrapeArchiverWithReady(t, "on", []any{1.0, nil, nil, 0.0, nil, nil, nil}, nil, denied)
	require.Equal(t, -1.0, metricValue(r, "pg_archiver_ready_files", nil))
	require.Equal(t, 1.0, metricValue(r, "pg_archiver_archived_total", nil))

	conn := archiverMockConn("on", []any{1.0, nil, nil, 0.0, nil, nil, nil}, nil, &mockRow{err: errors.New("connection reset")})
	_, err := (&archiverCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.Error(t, err)
}

func scrapeArchiverForTest(t *testing.T, mode string, stats []any, progress [][]any) Result {
	t.Helper()
	return scrapeArchiverWithReady(t, mode, stats, progress, &mockRow{vals: []any{0.0}})
}

func scrapeArchiverWithReady(t *testing.T, mode string, stats []any, progress [][]any, ready *mockRow) Result {
	t.Helper()
	conn := archiverMockConn(mode, stats, progress, ready)
	r, err := (&archiverCheck{}).Scrape(context.Background(), &SimpleTarget{
		ConnFunc: func(context.Context) (Conn, error) { return conn, nil }, ClockValue: clock.System(),
	})
	require.NoError(t, err)
	return r
}

func archiverMockConn(mode string, stats []any, progress [][]any, ready *mockRow) *mockConn {
	return &mockConn{
		queryRowFunc: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "archive_mode"):
				return &mockRow{vals: []any{mode}}
			case strings.Contains(query, "pg_ls_archive_statusdir"):
				return ready
			case strings.Contains(query, "pg_postmaster_start_time"):
				return &mockRow{vals: []any{time.Now().Add(-2 * time.Hour)}}
			default:
				return &mockRow{vals: stats}
			}
		},
		queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &mockRows{rows: progress}, nil
		},
	}
}
