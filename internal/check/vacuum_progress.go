package check

import (
	"context"
	"fmt"
	"sort"
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
	query := fmt.Sprintf(`SELECT p.pid,COALESCE(d.datname,''),COALESCE(p.relid,0::oid),COALESCE(p.phase,''),COALESCE(p.heap_blks_total,0),COALESCE(p.heap_blks_scanned,0),COALESCE(p.heap_blks_vacuumed,0),COALESCE(p.index_vacuum_count,0),COALESCE(p.%s,0),COALESCE(p.%s,0),a.query_start FROM pg_stat_progress_vacuum p JOIN pg_database d ON d.oid=p.datid LEFT JOIN pg_stat_activity a ON a.pid=p.pid`, deadMax, deadNow)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var jobs []progressJob
	for rows.Next() {
		var job progressJob
		if err := rows.Scan(&job.pid, &job.datname, &job.relid, &job.phase, &job.total, &job.scanned, &job.vacuumed, &job.idx, &job.deadMax, &job.dead, &job.started); err != nil {
			return Result{}, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	var result Result
	oldestFirst(jobs)
	maintenance, err := currentDatabase(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	for i, job := range jobs {
		if i == 20 {
			break
		}
		labels := map[string]string{"datname": job.datname, "relname": vacuumRelname(ctx, t, c.Timeout(), conn, maintenance, job.datname, job.relid), "phase": job.phase}
		addVacuumMetric(&result, "pg_vacuum_heap_blks_total", float64(job.total), labels)
		addVacuumMetric(&result, "pg_vacuum_heap_blks_scanned", float64(job.scanned), labels)
		addVacuumMetric(&result, "pg_vacuum_heap_blks_vacuumed", float64(job.vacuumed), labels)
		addVacuumMetric(&result, "pg_vacuum_index_vacuum_count", float64(job.idx), labels)
		name := "pg_vacuum_dead_tuples"
		if hasBytes {
			name = "pg_vacuum_dead_tuple_bytes"
		}
		addVacuumMetric(&result, name, float64(job.dead), labels)
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_vacuum_jobs_running", Kind: pgtype.KindGauge, Value: float64(len(jobs))})
	analyzeRows, err := conn.Query(ctx, `SELECT p.pid,COALESCE(d.datname,''),COALESCE(p.relid,0::oid),COALESCE(p.phase,''),COALESCE(p.sample_blks_total,0),COALESCE(p.sample_blks_scanned,0),a.query_start FROM pg_stat_progress_analyze p JOIN pg_database d ON d.oid=p.datid LEFT JOIN pg_stat_activity a ON a.pid=p.pid`)
	if err != nil {
		return Result{}, err
	}
	defer analyzeRows.Close()
	var analyzeJobs []progressJob
	for analyzeRows.Next() {
		var job progressJob
		if err := analyzeRows.Scan(&job.pid, &job.datname, &job.relid, &job.phase, &job.total, &job.scanned, &job.started); err != nil {
			return Result{}, err
		}
		analyzeJobs = append(analyzeJobs, job)
	}
	if err := analyzeRows.Err(); err != nil {
		return Result{}, err
	}
	oldestFirst(analyzeJobs)
	for i, job := range analyzeJobs {
		if i == 20 {
			break
		}
		labels := map[string]string{"datname": job.datname, "relname": vacuumRelname(ctx, t, c.Timeout(), conn, maintenance, job.datname, job.relid), "phase": job.phase}
		addVacuumMetric(&result, "pg_analyze_sample_blks_total", float64(job.total), labels)
		addVacuumMetric(&result, "pg_analyze_sample_blks_scanned", float64(job.scanned), labels)
	}
	result.Metrics = append(result.Metrics, pgtype.Metric{Name: "pg_analyze_jobs_running", Kind: pgtype.KindGauge, Value: float64(len(analyzeJobs))})
	// The 20-row cap is a fixed top-N subset, not a configured budget that
	// dropped keys, so it is Partial. Truncated would raise cardinality_truncated
	// and ask the operator to raise a budget this check does not have.
	result.Partial = len(jobs) > 20 || len(analyzeJobs) > 20
	return result, nil
}

// progressJob is one running vacuum or analyze. The progress views have no
// start time, so started is when its backend began the current command, from
// pg_stat_activity; it is nil when that session has already gone.
type progressJob struct {
	pid                           int32
	datname, phase                string
	relid                         uint32
	started                       *time.Time
	total, scanned, vacuumed, idx int64
	deadMax, dead                 int64
}

// oldestFirst orders jobs by when they started, so the 20-series cap keeps
// the longest-running ones. The views list backends in slot order, which
// says nothing about age: a vacuum that had run for hours could sit past the
// cap for its whole run. A job with no known start goes last; pid breaks ties.
func oldestFirst(jobs []progressJob) {
	sort.Slice(jobs, func(i, j int) bool {
		a, b := jobs[i].started, jobs[j].started
		switch {
		case a == nil && b == nil:
		case a == nil:
			return false
		case b == nil:
			return true
		case !a.Equal(*b):
			return a.Before(*b)
		}
		return jobs[i].pid < jobs[j].pid
	})
}
func addVacuumMetric(r *Result, name string, value float64, labels map[string]string) {
	r.Metrics = append(r.Metrics, pgtype.Metric{Name: name, Value: value, Kind: pgtype.KindGauge, Labels: labels})
}

// currentDatabase names the database conn is attached to. The shared
// connection is attached to the maintenance database, which is the one whose
// pg_class vacuumRelname can read through it.
func currentDatabase(ctx context.Context, conn Conn) (string, error) {
	var name string
	err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name)
	return name, err
}

// vacuumRelname names the relation a progress row is about. The progress views
// give the relation's OID for every database, but pg_class answers only for the
// database its connection is attached to. The row's own database is read
// through its own connection, and only when the agent collects from it: the
// connection manager would otherwise open a pool to a database the agent does
// not monitor. The gate and the connection both use the running schedule's set,
// so a database the discovery has since ranked out is still read, and a
// discovery is not repeated for every row. When the name cannot be read it
// stays empty, and the relid still identifies the relation.
func vacuumRelname(ctx context.Context, t Target, timeout time.Duration, shared Conn, maintenance, datname string, relid uint32) string {
	if relid == 0 {
		return ""
	}
	conn := shared
	if datname != maintenance {
		if !monitorsDatabase(t, datname) {
			return ""
		}
		other, err := connectScheduled(ctx, t, datname)
		if err != nil {
			return ""
		}
		defer other.Release()
		if err := ApplySessionLimits(ctx, other, timeout); err != nil {
			return ""
		}
		conn = other
	}
	var relname string
	if err := conn.QueryRow(ctx, `SELECT relname FROM pg_class WHERE oid = $1`, relid).Scan(&relname); err != nil {
		return ""
	}
	return relname
}
