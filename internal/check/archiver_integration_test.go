//go:build integration

package check

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTARCH001_FailingArchiveCommand(t *testing.T) {
	// pgtest cannot bootstrap archive_mode=on. This deterministic acceptance
	// test validates the scraper contract for the catalog state produced by a
	// failing archive_command without claiming a live archive-mode transition.
	now := time.Now().UTC()
	r := scrapeArchiverForTest(t, "on", []any{0.0, nil, nil, 1.0, nil, &now, nil}, nil)
	require.Equal(t, 1.0, metricValue(r, "pg_archive_mode_enabled", nil))
	require.Equal(t, 1.0, metricValue(r, "pg_archiver_failed_total", nil))
	require.Equal(t, 1.0, metricValue(r, "pg_archiver_failed_ratio", nil))
}

func TestINTARCH002_SuccessfulArchiveCommand(t *testing.T) {
	// See INT-ARCH-001: the supported equivalent is a deterministic contract
	// check for successful archiver statistics, not a fake live archive run.
	now := time.Now().UTC()
	r := scrapeArchiverForTest(t, "on", []any{1.0, nil, &now, 0.0, nil, nil, nil}, nil)
	require.Equal(t, 1.0, metricValue(r, "pg_archive_mode_enabled", nil))
	require.Equal(t, 1.0, metricValue(r, "pg_archiver_archived_total", nil))
	require.Equal(t, 0.0, metricValue(r, "pg_archiver_failed_ratio", nil))
}

// TestINTARCH003_DisabledArchiverEmitsNoArchiverCounters: with archive_mode
// off only the flag and the base backup gauge are reported. A base backup
// does not depend on archiving (pg_basebackup -X stream is the default).
func TestINTARCH003_DisabledArchiverEmitsNoArchiverCounters(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
		require.NoError(t, err)
		result, err := (&archiverCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
		require.Equal(t, 0.0, metricValue(result, "pg_archive_mode_enabled", nil))
		require.Equal(t, 0.0, metricValue(result, "pg_basebackup_jobs_running", nil))
		require.Len(t, result.Metrics, 2)
	})
}

// TestINTARCH004_MonitoringRoleCanCountReadySegments: the query behind
// pg_archiver_ready_files runs as the pg_monitor role on every supported
// major. pgtest cannot enable archiving, so this proves the privilege and
// the syntax rather than a live backlog.
func TestINTARCH004_MonitoringRoleCanCountReadySegments(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		var ready float64
		err := pg.Pool(t, "app", pgtest.RoleT0).QueryRow(context.Background(), `SELECT count(*)::float8 FROM pg_ls_archive_statusdir() WHERE name LIKE '%.ready'`).Scan(&ready)
		require.NoError(t, err)
		require.GreaterOrEqual(t, ready, 0.0)
	})
}
