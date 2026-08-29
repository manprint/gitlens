package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&databaseStatsCheck{})
}

type databaseStatsCheck struct{}

func (c *databaseStatsCheck) Name() string { return "database_stats" }
func (c *databaseStatsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *databaseStatsCheck) DefaultInterval() time.Duration { return 30 * time.Second }
func (c *databaseStatsCheck) Timeout() time.Duration         { return 2 * time.Second }

type dbStatRow struct {
	Datname      string
	XactCommit   int64
	XactRollback int64
	BlksRead     int64
	BlksHit      int64
	Deadlocks    int64
	TempBytes    int64
	Conflicts    int64
	Numbackends  int64
	StatsReset   *time.Time
}

func (c *databaseStatsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, `
SELECT datname,
       COALESCE(xact_commit,0) AS xact_commit,
       COALESCE(xact_rollback,0) AS xact_rollback,
       COALESCE(blks_read,0) AS blks_read,
       COALESCE(blks_hit,0) AS blks_hit,
       COALESCE(deadlocks,0) AS deadlocks,
       COALESCE(temp_bytes,0) AS temp_bytes,
       COALESCE(conflicts,0) AS conflicts,
       COALESCE(numbackends,0) AS numbackends,
       stats_reset
FROM pg_stat_database
WHERE datname IS NOT NULL
`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []dbStatRow
	for rows.Next() {
		var r dbStatRow
		if err := rows.Scan(&r.Datname, &r.XactCommit, &r.XactRollback, &r.BlksRead, &r.BlksHit, &r.Deadlocks, &r.TempBytes, &r.Conflicts, &r.Numbackends, &r.StatsReset); err != nil {
			continue
		}
		parsed = append(parsed, r)
	}
	return buildDatabaseStatsResult(parsed), nil
}

func buildDatabaseStatsResult(rows []dbStatRow) Result {
	var metrics []pgtype.Metric
	var maxReset *time.Time
	for _, r := range rows {
		labels := map[string]string{"database": r.Datname}
		metrics = append(metrics, pgtype.Metric{Name: "pg_xact_commit_total", Value: float64(r.XactCommit), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_xact_rollback_total", Value: float64(r.XactRollback), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_blks_read_total", Value: float64(r.BlksRead), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_blks_hit_total", Value: float64(r.BlksHit), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_deadlocks_total", Value: float64(r.Deadlocks), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_temp_bytes_total", Value: float64(r.TempBytes), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_conflicts_total", Value: float64(r.Conflicts), Kind: pgtype.KindCounter, Labels: labels})
		metrics = append(metrics, pgtype.Metric{Name: "pg_numbackends", Value: float64(r.Numbackends), Kind: pgtype.KindGauge, Labels: labels})
		if total := r.XactCommit + r.XactRollback; total > 0 {
			metrics = append(metrics, pgtype.Metric{Name: "pg_xact_rollback_ratio", Value: float64(r.XactRollback) / float64(total), Kind: pgtype.KindGauge, Labels: labels})
		}
		if total := r.BlksRead + r.BlksHit; total > 0 {
			metrics = append(metrics, pgtype.Metric{Name: "pg_blks_hit_ratio", Value: float64(r.BlksHit) / float64(total), Kind: pgtype.KindGauge, Labels: labels})
		}
		if r.StatsReset != nil {
			if maxReset == nil || r.StatsReset.After(*maxReset) {
				maxReset = r.StatsReset
			}
		}
	}
	return Result{Metrics: metrics, StatsReset: maxReset}
}
