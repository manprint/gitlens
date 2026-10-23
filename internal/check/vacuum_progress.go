package check

import (
	"context"
	"fmt"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&vacuumProgressCheck{}) }

type vacuumProgressCheck struct{}

func (c *vacuumProgressCheck) Name() string { return "vacuum_progress" }
func (c *vacuumProgressCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *vacuumProgressCheck) DefaultInterval() time.Duration { return 30 * time.Second }
func (c *vacuumProgressCheck) Timeout() time.Duration         { return 5 * time.Second }

func (c *vacuumProgressCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err = ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	columnRows, err := conn.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='pg_catalog' AND table_name='pg_stat_progress_vacuum'`)
	if err != nil {
		return Result{}, err
	}
	hasBytes := false
	for columnRows.Next() {
		var name string
		if err := columnRows.Scan(&name); err != nil {
			columnRows.Close()
			return Result{}, err
		}
		if name == "max_dead_tuple_bytes" || name == "dead_tuple_bytes" {
			hasBytes = true
		}
	}
	columnRows.Close()
	if err := columnRows.Err(); err != nil {
		return Result{}, err
	}
	deadMax, deadNow := "max_dead_tuples", "num_dead_tuples"
	if hasBytes {
		deadMax, deadNow = "max_dead_tuple_bytes", "dead_tuple_bytes"
	}
	// Progress columns are NULL for a command the monitoring role may not
	// see; one such row used to fail the whole scrape.
	query := fmt.Sprintf(`SELECT p.pid,COALESCE(d.datname,''),COALESCE(p.relid,0::oid),COALESCE(p.phase,''),COALESCE(p.heap_blks_total,0),COALESCE(p.heap_blks_scanned,0),COALESCE(p.heap_blks_vacuumed,0),COALESCE(p.index_vacuum_count,0),COALESCE(p.%s,0),COALESCE(p.%s,0) FROM pg_stat_progress_vacuum p JOIN pg_database d ON d.oid=p.datid`, deadMax, deadNow)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	jobs := 0
	for rows.Next() {
		var pid int32
		var datname, phase string
		var relid uint32
		var total, scanned, vacuumed, idx int64
		var deadMaxV, deadV int64
		if err := rows.Scan(&pid, &datname, &relid, &phase, &total, &scanned, &vacuumed, &idx, &deadMaxV, &deadV); err != nil {
			return Result{}, err
		}
		jobs++
		if jobs <= 20 {
			labels := map[string]string{"datname": datname, "relname": vacuumRelname(ctx, t, c.Timeout(), datname, relid), "phase": phase}
			addVacuumMetric(&result, "pg_vacuum_heap_blks_total", float64(total), labels)
			addVacuumMetric(&result, "pg_vacuum_heap_blks_scanned", float64(scanned), labels)
			addVacuumMetric(&result, "pg_vacuum_heap_blks_vacuumed", float64(vacuumed), labels)
			addVacuumMetric(&result, "pg_vacuum_index_vacuum_count", float64(idx), labels)
			name := "pg_vacuum_dead_tuples"
			if hasBytes {
				name = "pg_vacuum_dead_tuple_bytes"
			}
			addVacuumMetric(&result, name, float64(deadV), labels)
		}
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running", Kind: pgtype.KindGauge, Value: float64(jobs)})
	analyzeRows, err := conn.Query(ctx, `SELECT p.pid,COALESCE(d.datname,''),COALESCE(p.relid,0::oid),COALESCE(p.phase,''),COALESCE(p.sample_blks_total,0),COALESCE(p.sample_blks_scanned,0) FROM pg_stat_progress_analyze p JOIN pg_database d ON d.oid=p.datid`)
	if err != nil {
		return Result{}, err
	}
	defer analyzeRows.Close()
	analyze := 0
	for analyzeRows.Next() {
		var pid int32
		var datname, phase string
		var relid uint32
		var total, scanned int64
		if err := analyzeRows.Scan(&pid, &datname, &relid, &phase, &total, &scanned); err != nil {
			return Result{}, err
		}
		analyze++
		if analyze <= 20 {
			labels := map[string]string{"datname": datname, "relname": vacuumRelname(ctx, t, c.Timeout(), datname, relid), "phase": phase}
			addVacuumMetric(&result, "pg_analyze_sample_blks_total", float64(total), labels)
			addVacuumMetric(&result, "pg_analyze_sample_blks_scanned", float64(scanned), labels)
		}
	}
	if err := analyzeRows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_analyze_jobs_running", Kind: pgtype.KindGauge, Value: float64(analyze)})
	// The 20-row cap is a fixed top-N subset, not a configured budget that
	// dropped keys, so it is Partial. Truncated would raise cardinality_truncated
	// and ask the operator to raise a budget this check does not have.
	result.Partial = jobs > 20 || analyze > 20
	return result, nil
}
func addVacuumMetric(r *Result, name string, value float64, labels map[string]string) {
	r.Metrics = append(r.Metrics, pgtype.Metric{Name: name, Value: value, Kind: pgtype.KindGauge, Labels: labels})
}

// vacuumRelname names the relation a progress row is about. The progress views
// give the relation's OID for every database, but pg_class answers only for
// the database the connection is attached to, which is the maintenance
// database at instance scope. The name is therefore read from the row's own
// database. When that database cannot be read the name stays empty, as it
// always was for a relation outside the maintenance database.
func vacuumRelname(ctx context.Context, t Target, timeout time.Duration, datname string, relid uint32) string {
	if relid == 0 {
		return ""
	}
	conn, err := t.ConnFor(ctx, datname)
	if err != nil {
		return ""
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, timeout); err != nil {
		return ""
	}
	var relname string
	if err := conn.QueryRow(ctx, `SELECT relname FROM pg_class WHERE oid = $1`, relid).Scan(&relname); err != nil {
		return ""
	}
	return relname
}
