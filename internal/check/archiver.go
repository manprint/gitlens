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
	if mode != "on" && mode != "always" {
		return result, nil
	}
	var archived, failed float64
	var lastArchived, lastFailed, statsReset *time.Time
	row := conn.QueryRow(ctx, `SELECT archived_count, last_archived_wal, last_archived_time, failed_count, last_failed_wal, last_failed_time, stats_reset FROM pg_stat_archiver`)
	var lastArchivedWAL, lastFailedWAL *string
	if err := row.Scan(&archived, &lastArchivedWAL, &lastArchived, &failed, &lastFailedWAL, &lastFailed, &statsReset); err != nil {
		return Result{}, err
	}
	result.StatsReset = statsReset
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_archiver_archived_total", Value: archived, Kind: pgtype.KindCounter},
		pgtype.Metric{Name: "pg_archiver_failed_total", Value: failed, Kind: pgtype.KindCounter},
		pgtype.Metric{Name: "pg_archiver_failed_ratio", Value: archiverFailedRatio(lastArchived, lastFailed), Kind: pgtype.KindGauge},
	)
	now := t.Clock().Now()
	if lastArchived != nil {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_last_archived_age_seconds", Value: nonNegativeAge(now, *lastArchived), Kind: pgtype.KindGauge})
	}
	if lastFailed != nil {
		result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_archiver_last_failed_age_seconds", Value: nonNegativeAge(now, *lastFailed), Kind: pgtype.KindGauge})
	}
	rows, err := conn.Query(ctx, `SELECT pid, phase, backup_total, backup_streamed, tablespaces_total, tablespaces_streamed FROM pg_stat_progress_basebackup`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	jobs := 0
	for rows.Next() {
		var pid int32
		var phase string
		var total, streamed, tablespacesTotal, tablespacesStreamed float64
		if err := rows.Scan(&pid, &phase, &total, &streamed, &tablespacesTotal, &tablespacesStreamed); err != nil {
			return Result{}, err
		}
		jobs++
		if total > 0 {
			result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_basebackup_progress_ratio", Value: streamed / total, Kind: pgtype.KindGauge, Labels: map[string]string{"phase": phase}})
		}
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_basebackup_jobs_running", Value: float64(jobs), Kind: pgtype.KindGauge})
	return result, rows.Err()
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
