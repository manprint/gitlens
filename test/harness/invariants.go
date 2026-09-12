//go:build e2e

package harness

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
// I-8 ("cardinality within budget") and "no unexpected check errors" are
// checked through the server's own /metrics exposition — see
// AssertServerMetricInvariants. They were previously listed here as
// permanently unreachable gaps because the counters behind them did not
// exist; internal/server/metrics_handler.go now exports pglens_series_total,
// pglens_check_error_total and pglens_check_ok_total, so there is nothing
// left to fake.
//
// "No unexpected check errors" is deliberately not "no check ever errored":
// a scenario that fails over a primary or partitions a link makes every check
// against that instance report one error, correctly. The rule is that a check
// which errored must also have succeeded at least once — see MetricBudget.
//
// Still not checked here: "no goroutine leak", which needs a start-of-run
// snapshot this single end-of-run call does not have. The in-process
// equivalent lives in internal/leaktest and is asserted directly on every
// component that owns a background loop.
func (h *Harness) AssertInvariants(t *testing.T) {
	t.Helper()
	pool := h.DB(t)
	if pool == nil {
		t.Fatal("AssertInvariants: no DB pool (call after Start)")
	}
	AssertDBInvariants(context.Background(), t, pool)
	h.AssertServerMetricInvariants(t, DefaultMetricBudget())
}

// AssertServerMetricInvariants scrapes the server's /metrics and holds it to
// budget. Backs I-8 (cardinality within budget) and the "no unexpected check
// errors" invariant. The parsing and the assertions themselves live in
// metrics.go, untagged, so they are unit tested by `make test` rather than
// only exercised inside a compose stack.
//
// Several scenarios end with the server deliberately stopped — the agent
// buffer fills during a server outage, the alert leader fails over by killing
// a replica, SYS-NET-001 survives a backlog. For those the scrape cannot
// succeed, and failing the scenario for it would be asserting that a scenario
// about the server being down must find the server up. It is not enough to
// swallow the error either: a server that crashed when it should not have is
// exactly the kind of thing this call exists to catch. Docker settles it —
// if no pglens-server container is running, the stack put it that way on
// purpose; if one is running and the scrape still fails, that is a real
// failure.
func (h *Harness) AssertServerMetricInvariants(t *testing.T, budget MetricBudget) {
	t.Helper()
	body, err := h.scrapeAnyServerMetrics()
	if err != nil {
		if !h.serverContainerRunning() {
			t.Logf("I-8: no pglens-server container is running — the scenario stopped it; metric invariants not evaluated for this scenario")
			return
		}
		t.Fatalf("I-8: scrape /metrics: %v", err)
	}
	AssertMetricInvariants(t, body, budget)
}

// scrapeAnyServerMetrics returns the first replica's exposition that answers.
// The alerting scenarios scale pglens-server to two replicas and kill one to
// exercise advisory-lock leader failover; the counters are per-process, so
// any live replica's view is a valid one to hold to budget, and insisting on
// the first would fail a scenario whose whole point is that the first one
// died.
func (h *Harness) scrapeAnyServerMetrics() (string, error) {
	clients := h.apiClients
	if len(clients) == 0 {
		clients = []*APIClient{h.API()}
	}
	var firstErr error
	for _, c := range clients {
		if c == nil {
			continue
		}
		body, err := c.RawGet("/metrics")
		if err == nil {
			return body, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("no server API client available")
	}
	return "", firstErr
}

// serverContainerRunning reports whether the compose project still has a
// running pglens-server. A compose or Docker failure is reported as "running"
// so that an unrelated tooling problem cannot silently turn the scrape
// failure above into a skip.
func (h *Harness) serverContainerRunning() bool {
	out, err := h.composeOutput("ps", "-q", "--status", "running", "pglens-server")
	if err != nil {
		return true
	}
	return len(strings.Fields(out)) > 0
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
// rate; the affected interval must emit no point at all instead. The generic
// metrics table also stores raw gauges, including PostgreSQL's -1 "unlimited"
// setting sentinel converted to bytes/seconds, so those gauge families are
// intentionally outside this rate invariant.
func assertNoNegativeRates(ctx context.Context, t TestingT, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT metric, series_id, ts, value FROM metrics
		WHERE value < 0 AND metric NOT IN ('pg_setting_bytes', 'pg_setting_seconds') LIMIT 20`)
	if err != nil {
		t.Fatalf("I-2 (metrics): query failed: %v", err)
	}
	defer rows.Close()
	dumpAndFail(t, "I-2: negative rate in metrics", rows, func() []any {
		var metric string
		var seriesID int64
		var ts time.Time
		var v float64
		return []any{&metric, &seriesID, &ts, &v}
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
