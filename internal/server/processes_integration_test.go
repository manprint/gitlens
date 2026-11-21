//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/manprint/pglens/internal/store"
)

// TestINTPROC002: process rows at two sample times; the snapshot shows the
// newer one by CPU, and the grouped view averages over both sample times.
func TestINTPROC002_SnapshotAndGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "timescale/timescaledb:2.31.0-pg17",
		postgres.WithDatabase("test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, store.Migrate(ctx, pool))

	inst := uuid.New()
	t1 := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Minute)
	t2 := t1.Add(10 * time.Second)
	f := func(v float64) *float64 { return &v }
	qid := int64(-99)
	row := func(ts time.Time, pid int32, bt string, cpu float64, rss int64, read *float64) store.ProcessRow {
		return store.ProcessRow{TS: ts, TenantID: "default", InstanceID: inst, ClusterID: 1, PID: pid, BackendType: bt,
			Datname: "app", CPURatio: cpu, RSSBytes: rss, ReadBytesPerS: read}
	}
	rows := []store.ProcessRow{
		row(t1, 100, "postmaster", 0.01, 1000, nil),
		row(t1, 101, "client backend", 0.9, 5000, f(100)),
		row(t2, 100, "postmaster", 0.01, 1000, nil),
		row(t2, 101, "client backend", 0.3, 6000, f(300)),
		row(t2, 102, "client backend", 0.6, 2000, f(0)),
	}
	rows[4].QueryID = &qid
	for i := 0; i < 2; i++ { // the second write is a replay: ON CONFLICT DO NOTHING
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, store.WriteProcesses(ctx, tx, rows))
		require.NoError(t, tx.Commit(ctx))
	}
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_processes WHERE instance_id = $1`, inst).Scan(&n))
	require.Equal(t, 5, n)

	router := hostTestRouter(NewAPI(pool))
	get := func(url string, out any) {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), out))
	}
	base := "/api/v1/instances/" + inst.String() + "/host/processes"

	var snap processSnapshotJSON
	get(base, &snap)
	require.NotNil(t, snap.TS)
	require.True(t, snap.TS.Equal(t2))
	require.Len(t, snap.Processes, 3)
	require.Equal(t, []int32{102, 101, 100}, []int32{snap.Processes[0].PID, snap.Processes[1].PID, snap.Processes[2].PID})
	require.Equal(t, "-99", *snap.Processes[0].QueryID)
	require.Nil(t, snap.Processes[2].ReadBytesPerS)

	get(base+"?order_by=rss&limit=1", &snap)
	require.Len(t, snap.Processes, 1)
	require.Equal(t, int32(101), snap.Processes[0].PID)

	get(base+"?at="+t1.Add(time.Second).Format(time.RFC3339), &snap)
	require.True(t, snap.TS.Equal(t1))
	require.Len(t, snap.Processes, 2)

	get(base+"?at="+t1.Add(-time.Second).Format(time.RFC3339), &snap)
	require.Nil(t, snap.TS)
	require.Empty(t, snap.Processes)

	var top processTopJSON
	from, to := t1.Add(-time.Minute).Format(time.RFC3339), t2.Add(time.Minute).Format(time.RFC3339)
	get(base+"/top?group_by=backend_type&from="+from+"&to="+to, &top)
	require.Equal(t, int64(2), top.SampleTimes)
	require.Len(t, top.Groups, 2)
	cb := top.Groups[0]
	require.Equal(t, "client backend", cb.Group)
	require.InDelta(t, 0.9, cb.AvgCPURatio, 1e-9) // (0.9 + 0.3 + 0.6) / 2
	require.Equal(t, int64(6000), cb.MaxRSSBytes)
	require.InDelta(t, 200, *cb.AvgReadBytesPerS, 1e-9) // (100 + 300 + 0) / 2
	require.Nil(t, cb.AvgWriteBytesPerS)
	pm := top.Groups[1]
	require.Equal(t, "postmaster", pm.Group)
	require.InDelta(t, 0.01, pm.AvgCPURatio, 1e-9)
	require.Nil(t, pm.AvgReadBytesPerS)
}
