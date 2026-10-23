package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&archiverCheck{}) }

type archiverCheck struct{}

func (c *archiverCheck) Name() string { return "archiver" }
func (c *archiverCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *archiverCheck) DefaultInterval() time.Duration { return time.Minute }
func (c *archiverCheck) Timeout() time.Duration         { return 5 * time.Second }

func (c *archiverCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	var mode string
	if err := conn.QueryRow(ctx, "SELECT current_setting('archive_mode')").Scan(&mode); err != nil {
		return Result{}, err
	}
	result := Result{Metrics: []pgtype.Metric{{Name: "pg_archive_mode_enabled", Value: archiveModeValue(mode), Kind: pgtype.KindGauge}}}
	// A base backup does not need WAL archiving (pg_basebackup -X stream is
	// the default), so its progress is reported whatever archive_mode says.
	backups, err := basebackupMetrics(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, backups...)
	if mode != "on" && mode != "always" {
		return result, nil
	}
	var archived, failed float64
	var lastArchived, lastFailed, statsReset *time.Time
	// The ages are measured on the server clock, read in the same statement
	// as the timestamps they are measured from. The agent's clock may be
	// skewed from the server's, which would move every age by the skew.
	var now time.Time
	row := conn.QueryRow(ctx, `SELECT archived_count, last_archived_wal, last_archived_time, failed_count, last_failed_wal, last_failed_time, stats_reset, now() FROM pg_stat_archiver`)
	var lastArchivedWAL, lastFailedWAL *string
	if err := row.Scan(&archived, &lastArchivedWAL, &lastArchived, &failed, &lastFailedWAL, &lastFailed, &statsReset, &now); err != nil {
		return Result{}, err
	}
	result.StatsReset = statsReset
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_archiver_archived_total", Value: archived, Kind: pgtype.KindCounter},
		pgtype.Metric{Name: "pg_archiver_failed_total", Value: failed, Kind: pgtype.KindCounter},
		pgtype.Metric{Name: "pg_archiver_failed_ratio", Value: archiverFailedRatio(lastArchived, lastFailed), Kind: pgtype.KindGauge},
	)
	if lastArchived != nil {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_last_archived_age_seconds", Value: nonNegativeAge(now, *lastArchived), Kind: pgtype.KindGauge})
	}
	if lastFailed != nil {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_last_failed_age_seconds", Value: nonNegativeAge(now, *lastFailed), Kind: pgtype.KindGauge})
	}
	// The segments waiting for archive_command. The age of the last archived
	// segment cannot tell a stuck archiver from an idle server that has
	// nothing to archive. The function needs pg_monitor; without it, or when
	// the server answers with an error such as a statement timeout on a long
	// archive directory, the metric is left out rather than failing the scrape
	// and dropping the counters above. Only a failure to reach the server
	// fails the scrape.
	var ready float64
	readyKnown := false
	if err := conn.QueryRow(ctx, `SELECT count(*)::float8 FROM pg_ls_archive_statusdir() WHERE name LIKE '%.ready'`).Scan(&ready); err == nil {
		readyKnown = true
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_ready_files", Value: ready, Kind: pgtype.KindGauge})
	} else if pgErrorCode(err) == "" {
		return Result{}, err
	}
	// A server that has never archived has no last archived time, so the age
	// above is missing and the stall rule has nothing to judge. Archiving has
	// been expected since the server started (archive_mode changes only at a
	// restart), or since the statistics were reset when that is later, so the
	// age is at least the time since then. It is reported only together with
	// the waiting segments: an idle server that never archived has nothing
	// stuck, and the age alone would read as a stall once it had been up for
	// an hour.
	if lastArchived == nil && readyKnown {
		var since time.Time
		if err := conn.QueryRow(ctx, `SELECT pg_postmaster_start_time()`).Scan(&since); err != nil {
			return Result{}, err
		}
		if statsReset != nil && statsReset.After(since) {
			since = *statsReset
		}
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_last_archived_age_seconds", Value: nonNegativeAge(now, since), Kind: pgtype.KindGauge})
	}
	return result, nil
}

// basebackupMetrics reports running base backups. backup_total is NULL until
// the size estimate is done, and for the whole backup with --no-estimate-size:
// scanning it into a float64 failed the entire archiver scrape.
func basebackupMetrics(ctx context.Context, conn Conn) ([]pgtype.Metric, error) {
	rows, err := conn.Query(ctx, `SELECT phase, COALESCE(backup_total, 0)::float8, COALESCE(backup_streamed, 0)::float8 FROM pg_stat_progress_basebackup`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var metrics []pgtype.Metric
	jobs := 0
	for rows.Next() {
		var phase string
		var total, streamed float64
		if err := rows.Scan(&phase, &total, &streamed); err != nil {
			return nil, err
		}
		jobs++
		if total > 0 {
			metrics = append(metrics, pgtype.Metric{Name: "pg_basebackup_progress_ratio", Value: streamed / total, Kind: pgtype.KindGauge, Labels: map[string]string{"phase": phase}})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(metrics, pgtype.Metric{Name: "pg_basebackup_jobs_running", Value: float64(jobs), Kind: pgtype.KindGauge}), nil
}

func archiveModeValue(mode string) float64 {
	if mode == "on" || mode == "always" {
		return 1
	}
	return 0
}

func archiverFailedRatio(lastArchived, lastFailed *time.Time) float64 {
	if lastFailed != nil && (lastArchived == nil || lastFailed.After(*lastArchived)) {
		return 1
	}
	return 0
}

func nonNegativeAge(now, then time.Time) float64 {
	age := now.Sub(then).Seconds()
	if age < 0 {
		return 0
	}
	return age
}
