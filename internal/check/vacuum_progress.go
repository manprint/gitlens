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
	columnRows, err := conn.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='pg_stat_progress_vacuum'`)
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
	deadMax, deadNow := "max_dead_tuples", "num_dead_tuples"
	if hasBytes {
		deadMax, deadNow = "max_dead_tuple_bytes", "dead_tuple_bytes"
	}
	query := fmt.Sprintf(`SELECT p.pid,COALESCE(d.datname,''),COALESCE(c.relname,''),p.phase,p.heap_blks_total,p.heap_blks_scanned,p.heap_blks_vacuumed,p.index_vacuum_count,p.%s,p.%s FROM pg_stat_progress_vacuum p JOIN pg_database d ON d.oid=p.datid LEFT JOIN pg_class c ON c.oid=p.relid`, deadMax, deadNow)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	jobs := 0
	for rows.Next() {
		var pid int32
		var datname, relname, phase string
		var total, scanned, vacuumed, idx int64
		var deadMaxV, deadV int64
		if err := rows.Scan(&pid, &datname, &relname, &phase, &total, &scanned, &vacuumed, &idx, &deadMaxV, &deadV); err != nil {
			return Result{}, err
		}
		jobs++
		if jobs <= 20 {
			labels := map[string]string{"datname": datname, "relname": relname, "phase": phase}
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
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running", Kind: pgtype.KindGauge, Value: float64(jobs)})
	analyzeRows, err := conn.Query(ctx, `SELECT p.pid,COALESCE(d.datname,''),COALESCE(c.relname,''),p.phase,p.sample_blks_total,p.sample_blks_scanned FROM pg_stat_progress_analyze p JOIN pg_database d ON d.oid=p.datid LEFT JOIN pg_class c ON c.oid=p.relid`)
	if err != nil {
		return Result{}, err
	}
	defer analyzeRows.Close()
	analyze := 0
	for analyzeRows.Next() {
		var pid int32
		var datname, relname, phase string
		var total, scanned int64
		if err := analyzeRows.Scan(&pid, &datname, &relname, &phase, &total, &scanned); err != nil {
			return Result{}, err
		}
		analyze++
		if analyze <= 20 {
			labels := map[string]string{"datname": datname, "relname": relname, "phase": phase}
			addVacuumMetric(&result, "pg_analyze_sample_blks_total", float64(total), labels)
			addVacuumMetric(&result, "pg_analyze_sample_blks_scanned", float64(scanned), labels)
		}
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_analyze_jobs_running", Kind: pgtype.KindGauge, Value: float64(analyze)})
	result.Truncated = jobs > 20 || analyze > 20
	return result, nil
}
func addVacuumMetric(r *Result, name string, value float64, labels map[string]string) {
	r.Metrics = append(r.Metrics, pgtype.Metric{Name: name, Value: value, Kind: pgtype.KindGauge, Labels: labels})
}
