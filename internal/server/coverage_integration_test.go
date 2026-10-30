//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// COV-INT-001: two pushes of one agent run carry different checks, and the
// coverage of the instance counts each check once, with the failed one apart
// and the unreported one pending until its first-run bound. A third push from
// a restarted agent makes the checks it has not reported again pending. The
// inventory keeps the agent start and the intervals, the pipeline the results.
func TestCOVINT001_TwoPushesCountRight(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	inv := NewInventory(pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	h := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(h)

	agentID, instID := uuid.NewString(), uuid.New()
	started := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	intervals := map[string]int{"activity": 15, "locks": 30, "settings": 3600}
	push := func(start time.Time, results ...wire.Result) {
		t.Helper()
		env := wire.Envelope{AgentID: agentID, Instances: []wire.Instance{{
			InstanceID: instID.String(), ClusterID: pgtype.ClusterID(71320).String(), ClusterIDSource: string(pgtype.IDSourceManual),
			Addr: "10.0.13.2", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0",
			AgentStartedAt: &start, CheckIntervals: intervals, Results: results,
		}}}
		res, err := inv.Upsert(ctx, env)
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted, "%v", res.Errors)
		_, err = p.Process(ctx, env)
		require.NoError(t, err)
	}
	coverage := func() coverageJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instID.String()+"/coverage", nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out coverageJSON
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}
	states := func(c coverageJSON) map[string]string {
		out := map[string]string{}
		for _, check := range c.Checks {
			out[check.Check] = check.State
		}
		return out
	}
	gauge := []wire.Metric{{Name: "pg_connections_used", Value: 3, Kind: "gauge"}}

	push(started, wire.Result{Check: "activity", TS: started.Add(10 * time.Second), Metrics: gauge})
	push(started,
		wire.Result{Check: "activity", TS: started.Add(25 * time.Second), Metrics: gauge},
		wire.Result{Check: "locks", TS: started.Add(25 * time.Second), Error: "permission denied for function pg_blocking_pids"},
		wire.Result{Check: "settings", TS: started.Add(25 * time.Second), SkipReason: "not due"})

	var gotStart time.Time
	var gotIntervals []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT agent_started_at, check_intervals FROM instances WHERE instance_id=$1`, instID).Scan(&gotStart, &gotIntervals))
	require.True(t, started.Equal(gotStart))
	require.JSONEq(t, `{"activity":15,"locks":30,"settings":3600}`, string(gotIntervals))
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM check_results WHERE instance_id=$1`, instID).Scan(&rows))
	require.Equal(t, 2, rows, "a skipped check has no result")

	c := coverage()
	require.Equal(t, instID.String(), c.InstanceID)
	require.Equal(t, map[string]string{"activity": "reported", "locks": "failed", "settings": "pending"}, states(c))
	require.Equal(t, 1, c.Reported)
	require.Equal(t, 3, c.Total)
	for _, check := range c.Checks {
		switch check.Check {
		case "activity":
			require.True(t, started.Add(10*time.Second).Equal(*check.FirstResultAt), "the first result of the run stays")
			require.True(t, started.Add(25*time.Second).Equal(*check.LastResultAt))
		case "settings":
			require.Nil(t, check.LastResultAt)
			require.NotNil(t, check.ExpectedBy)
			require.True(t, started.Add(wire.FirstRunBound(time.Hour)+coverageGrace).Equal(*check.ExpectedBy))
		}
	}

	// The agent restarts: only the check it has reported since counts.
	restarted := started.Add(2 * time.Minute)
	push(restarted, wire.Result{Check: "locks", TS: restarted.Add(5 * time.Second), Metrics: gauge})
	c = coverage()
	require.Equal(t, map[string]string{"activity": "pending", "locks": "reported", "settings": "pending"}, states(c))
	require.Equal(t, 1, c.Reported)
	require.Equal(t, 3, c.Total)
	for _, check := range c.Checks {
		if check.Check == "locks" {
			require.True(t, restarted.Add(5*time.Second).Equal(*check.FirstResultAt), "a new run starts its first result again")
		}
	}

	// An unknown instance is a 404.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+uuid.NewString()+"/coverage", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// COV-INT-002: a database-scoped check stores a row per database, and the
// coverage reads it failed when its newest result failed in one database,
// though a later result in another succeeded. Two results of one check in one
// push keep the first and the newest.
func TestCOVINT002_DatabasesAndOneEnvelope(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	inv := NewInventory(pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	h := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(h)

	instID := uuid.New()
	started := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	env := wire.Envelope{AgentID: uuid.NewString(), Instances: []wire.Instance{{
		InstanceID: instID.String(), ClusterID: pgtype.ClusterID(71321).String(), ClusterIDSource: string(pgtype.IDSourceManual),
		Addr: "10.0.13.3", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0",
		AgentStartedAt: &started, CheckIntervals: map[string]int{"activity": 15, "table_stats": 300},
		Results: []wire.Result{
			{Check: "table_stats", Database: "crm", TS: started.Add(10 * time.Second), Error: "canceling statement due to statement timeout"},
			{Check: "table_stats", Database: "app", TS: started.Add(20 * time.Second)},
			{Check: "activity", TS: started.Add(30 * time.Second)},
			{Check: "activity", TS: started.Add(15 * time.Second)},
		},
	}}}
	res, err := inv.Upsert(ctx, env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted, "%v", res.Errors)
	_, err = p.Process(ctx, env)
	require.NoError(t, err)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM check_results WHERE instance_id=$1 AND check_name='table_stats'`, instID).Scan(&rows))
	require.Equal(t, 2, rows, "one row per database")
	var first, last time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT first_result_at, last_result_at FROM check_results WHERE instance_id=$1 AND check_name='activity' AND datname=''`, instID).Scan(&first, &last))
	require.True(t, started.Add(15*time.Second).Equal(first), "the first result of the push")
	require.True(t, started.Add(30*time.Second).Equal(last), "the newest result of the push")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instID.String()+"/coverage", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out coverageJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	byCheck := coverageByCheck(out)
	require.Equal(t, coverageFailed, byCheck["table_stats"].State)
	require.True(t, started.Add(10*time.Second).Equal(*byCheck["table_stats"].FirstResultAt))
	require.True(t, started.Add(20*time.Second).Equal(*byCheck["table_stats"].LastResultAt))
	require.Equal(t, coverageReported, byCheck["activity"].State)
	require.Equal(t, 1, out.Reported)
	require.Equal(t, 2, out.Total)
}
