//go:build e2e

package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestingT is the subset of *testing.T the invariant checks need. Accepting
// this instead of the concrete type lets a test prove "this check would have
// failed" via a substitute that records the call instead of aborting the
// test that is verifying detection works (see test/e2e/invariants_test.go).
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

var _ TestingT = (*testing.T)(nil)

// dumpAndFail fails t if rows has any results, printing every offending row.
// newDest must return a fresh slice of scan-destination pointers each call.
func dumpAndFail(t TestingT, label string, rows pgx.Rows, newDest func() []any) {
	t.Helper()
	var offenders []string
	for rows.Next() {
		dest := newDest()
		if err := rows.Scan(dest...); err != nil {
			t.Fatalf("%s: scan offending row: %v", label, err)
		}
		vals := make([]any, len(dest))
		for i, d := range dest {
			vals[i] = derefAny(d)
		}
		offenders = append(offenders, fmt.Sprintf("%+v", vals))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: rows error: %v", label, err)
	}
	if len(offenders) > 0 {
		t.Errorf("%s (%d row(s)):\n  %s", label, len(offenders), joinLines(offenders))
	}
}

func derefAny(p any) any {
	switch v := p.(type) {
	case *int64:
		return *v
	case *time.Time:
		return *v
	case *float64:
		return *v
	case *string:
		return *v
	case *[]byte:
		return string(*v)
	case *any:
		return *v
	default:
		return p
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n  "
		}
		out += l
	}
	return out
}

// AssertInvariants checks the properties nobody thought to assert, at the end
// of every scenario (phase_06.md §5.6). Failures name the invariant and dump
// the offending rows.
//
// Not yet checked here, and why: "cardinality within budget" (I-8) and "no
// unexpected errors" depend on a `pglens_series_total` / `check_error_total`
// counter the agent does not expose yet (no sub-phase has built it); "no
// goroutine leak" needs a start-of-run snapshot this single end-of-run call
// does not have. Faking these would be worse than omitting them — see
// STATE.md §6 for the tracked gap.
func (h *Harness) AssertInvariants(t *testing.T) {
	t.Helper()
	pool := h.DB(t)
	if pool == nil {
		t.Fatal("AssertInvariants: no DB pool (call after Start)")
	}
	AssertDBInvariants(context.Background(), t, pool)
}

// AssertDBInvariants runs the DB-backed invariant checks against pool. Split
// out from AssertInvariants so unit tests can drive it directly against a
// pgtest fixture instead of a full docker-compose stack.
func AssertDBInvariants(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	assertNoDuplicateSamples(ctx, t, pool)
	assertNoNegativeRates(ctx, t, pool)
	assertNoOrphanMetrics(ctx, t, pool)
	assertClusterIDStable(ctx, t, pool)
}

// assertNoDuplicateSamples backs I-3: the same (series, ts) is never written
// twice, whatever the buffer replay path did.
func assertNoDuplicateSamples(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT series_id, ts, count(*)
		  FROM metrics
		 GROUP BY series_id, ts
		HAVING count(*) > 1
		 LIMIT 20`)
	if err != nil {
		t.Fatalf("I-3 (metrics): query failed: %v", err)
	}
	defer rows.Close()
	dumpAndFail(t, "I-3: duplicate (series_id, ts) in metrics", rows, func() []any {
		var seriesID int64
		var ts time.Time
		var n int
		return []any{&seriesID, &ts, &n}
	})
}

// assertNoNegativeRates backs I-2: a counter reset never produces a negative
// rate; the affected interval must emit no point at all instead.
func assertNoNegativeRates(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT series_id, ts, value FROM metrics WHERE value < 0 LIMIT 20`)
	if err != nil {
		t.Fatalf("I-2 (metrics): query failed: %v", err)
	}
	defer rows.Close()
	dumpAndFail(t, "I-2: negative rate in metrics", rows, func() []any {
		var seriesID int64
		var ts time.Time
		var v float64
		return []any{&seriesID, &ts, &v}
	})

	rows2, err := pool.Query(ctx, `
		SELECT instance_id, datname, queryid, ts
		  FROM metrics_statements
		 WHERE calls_rate < 0 OR exec_time_rate_ms < 0 OR rows_rate < 0
		    OR shared_blks_hit_rate < 0 OR shared_blks_read_rate < 0 OR wal_bytes_rate < 0
		 LIMIT 20`)
	if err != nil {
		t.Fatalf("I-2 (metrics_statements): query failed: %v", err)
	}
	defer rows2.Close()
	dumpAndFail(t, "I-2: negative rate in metrics_statements", rows2, func() []any {
		var instID any
		var datname string
		var queryid int64
		var ts time.Time
		return []any{&instID, &datname, &queryid, &ts}
	})
}

// assertNoOrphanMetrics backs I-4: every instance_id in every metric table
// resolves to an existing instances row.
func assertNoOrphanMetrics(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	tables := []string{"metrics", "metrics_statements", "metrics_ash", "metrics_replication"}
	for _, table := range tables {
		rows, err := pool.Query(ctx, fmt.Sprintf(`
			SELECT DISTINCT m.instance_id
			  FROM %s m
			  LEFT JOIN instances i ON i.instance_id = m.instance_id
			 WHERE i.instance_id IS NULL
			 LIMIT 20`, table))
		if err != nil {
			t.Fatalf("I-4 (%s): query failed: %v", table, err)
		}
		dumpAndFail(t, fmt.Sprintf("I-4: orphan instance_id in %s (no matching instances row)", table), rows, func() []any {
			var instID any
			return []any{&instID}
		})
		rows.Close()
	}
}

// assertClusterIDStable backs I-1: cluster_id never changes for a cluster
// across failover, promote, rename, or IP change — asserted here by the
// absence of any cluster_id_changed event, which internal/server/inventory.go
// emits the moment it observes a cluster_id mismatch for a known instance.
func assertClusterIDStable(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT event_id, ts, instance_id, payload
		  FROM events
		 WHERE type = 'cluster_id_changed'
		 LIMIT 20`)
	if err != nil {
		t.Fatalf("I-1: query failed: %v", err)
	}
	defer rows.Close()
	dumpAndFail(t, "I-1: cluster_id_changed event(s) found — cluster_id must never change", rows, func() []any {
		var eventID int64
		var ts time.Time
		var instID any
		var payload []byte
		return []any{&eventID, &ts, &instID, &payload}
	})
}

// AssertConnectionCeiling backs I-6: the agent never holds more than
// maxConnsPerInstance connections to a monitored instance. This is a
// point-in-time snapshot via pg_stat_activity, not a true peak-during-run
// sample — a scenario asserting this meaningfully must call it while load is
// active, not only at teardown.
func AssertConnectionCeiling(ctx context.Context, t *testing.T, monitoredPool *pgxpool.Pool, maxConnsPerInstance int) {
	t.Helper()
	var n int
	err := monitoredPool.QueryRow(ctx,
		`SELECT count(*) FROM pg_stat_activity WHERE application_name LIKE 'pglens/%'`).Scan(&n)
	if err != nil {
		t.Fatalf("I-6: query pg_stat_activity failed: %v", err)
	}
	if n > maxConnsPerInstance {
		t.Errorf("I-6: connection ceiling violated: %d pglens/%% backends open, ceiling is %d", n, maxConnsPerInstance)
	}
}

// AssertCleanTeardown backs the "no container/volume/network survives"
// check. Unlike the other invariants it only makes sense to run after
// cleanup() has completed, not from inside AssertInvariants — call it after
// the harness's own teardown (e.g. by wrapping Start's t.Cleanup) if a
// scenario needs to prove it explicitly.
func (h *Harness) AssertCleanTeardown(t *testing.T) {
	t.Helper()
	out, err := h.composeOutput("ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		t.Fatalf("clean-teardown check: docker compose ps failed: %v", err)
	}
	if out != "" {
		t.Errorf("clean teardown violated: containers still present for project %s:\n%s", h.projectName, out)
	}
}
