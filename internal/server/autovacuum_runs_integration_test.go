//go:build integration

package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestINTAVH002: the same autovacuum_run pushed twice is stored once, no object
// fact is written, and the 90-day retention policy exists.
func TestINTAVH002(t *testing.T) {
	router, count := planRouter(t)
	pool := getSharedPool(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := autovacuumFact(map[string]string{"ts": now.Add(-time.Second).Format(time.RFC3339Nano)}, `{"tuples_removed":5,"elapsed_seconds":0.5}`)
	pushPlanFacts(t, router, instance, now, f)
	pushPlanFacts(t, router, instance, now, f)
	require.Equal(t, 1, count("autovacuum_runs", instance))

	ctx := context.Background()
	var removed int64
	var scanned *int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT tuples_removed, pages_scanned FROM autovacuum_runs WHERE instance_id = $1 AND relname = 't'`, instance).Scan(&removed, &scanned))
	require.Equal(t, int64(5), removed)
	require.Nil(t, scanned)
	var facts int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE kind = 'autovacuum_run'`).Scan(&facts))
	require.Zero(t, facts)

	var dropAfter string
	require.NoError(t, pool.QueryRow(ctx, `SELECT config->>'drop_after' FROM timescaledb_information.jobs WHERE proc_name = 'policy_retention' AND hypertable_name = 'autovacuum_runs'`).Scan(&dropAfter))
	require.Equal(t, "90 days", dropAfter)
}

// TestINTAVH003: runs filter by relation and order by elapsed time; the
// summary sums each table and puts the most autovacuum time first.
func TestINTAVH003(t *testing.T) {
	router, count := planRouter(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	at := func(s int) string { return now.Add(-time.Duration(s) * time.Minute).Format(time.RFC3339Nano) }
	pushPlanFacts(t, router, instance, now,
		autovacuumFact(map[string]string{"ts": at(3), "relname": "a"}, `{"tuples_removed":10,"elapsed_seconds":1}`),
		autovacuumFact(map[string]string{"ts": at(2), "relname": "a"}, `{"tuples_removed":20,"elapsed_seconds":2}`),
		autovacuumFact(map[string]string{"ts": at(1), "relname": "b", "wraparound": "true"}, `{"tuples_removed":5,"elapsed_seconds":5}`),
	)
	require.Equal(t, 3, count("autovacuum_runs", instance))

	var runs struct {
		Runs []autovacuumRunJSON `json:"runs"`
	}
	getPlanJSON(t, router, fmt.Sprintf("/api/v1/autovacuum/runs?instance_id=%s&relation=a&order_by=elapsed_seconds", instance), &runs)
	require.Len(t, runs.Runs, 2)
	require.Equal(t, "a", runs.Runs[0].Relname)
	require.Equal(t, 2.0, *runs.Runs[0].ElapsedSeconds, "longest first")
	require.Equal(t, 1.0, *runs.Runs[1].ElapsedSeconds)

	getPlanJSON(t, router, fmt.Sprintf("/api/v1/autovacuum/runs?instance_id=%s", instance), &runs)
	require.Len(t, runs.Runs, 3)
	require.Equal(t, "b", runs.Runs[0].Relname, "newest first by default")

	var summary struct {
		Tables []autovacuumSummaryJSON `json:"tables"`
	}
	getPlanJSON(t, router, fmt.Sprintf("/api/v1/autovacuum/summary?instance_id=%s", instance), &summary)
	require.Len(t, summary.Tables, 2)
	b, a := summary.Tables[0], summary.Tables[1]
	require.Equal(t, "b", b.Relname)
	require.Equal(t, int64(1), b.WraparoundRuns)
	require.Equal(t, 5.0, b.TotalElapsedSeconds)
	require.Equal(t, "a", a.Relname)
	require.Equal(t, int64(2), a.Runs)
	require.Equal(t, 3.0, a.TotalElapsedSeconds)
	require.Equal(t, 2.0, a.MaxElapsedSeconds)
	require.Equal(t, int64(30), a.TuplesRemoved)
	require.Zero(t, a.WraparoundRuns)
	require.WithinDuration(t, now.Add(-2*time.Minute), a.LastTS, time.Second)
}
