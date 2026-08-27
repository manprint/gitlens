//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestPipeline_Process_WithRealDB(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(99999).String()
	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(60 * time.Second)
	iid := uuid.MustParse(instID)
	cidDB := int64(pgtype.ClusterID(99999))
	// need to ensure cluster/instance exist? Pipeline writes metrics directly without FK? But store WriteMetrics will try to insert, FK not enforced if we haven't created cluster/instance? Our schema has FK for instances but metrics table has no FK, so we can write metrics without prior cluster row.
	// Test gauge
	envGauge := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts1, Metrics: []wire.Metric{{Name: "pg_buffers_checkpoint", Value: 42, Kind: "gauge"}}}}}}}
	res, err := p.Process(context.Background(), envGauge)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics WHERE metric='pg_buffers_checkpoint'`).Scan(&cnt))
	require.Equal(t, 1, cnt)

	// Test counter rate: first no row
	envC1 := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts1, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 100, Kind: "counter"}}}}}}}
	envC2 := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: ts2, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 160, Kind: "counter"}}}}}}}
	// reset pipeline delta by creating new pipeline for isolated test
	p2 := NewPipeline(pool, clock.NewFake(ts1))
	truncateAll(t, pool)
	// need to re-truncate after first gauge
	_, _ = p2.Process(context.Background(), envC1)
	var cnt2 int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit'`).Scan(&cnt2))
	require.Equal(t, 0, cnt2)
	_, _ = p2.Process(context.Background(), envC2)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit'`).Scan(&cnt2))
	require.Equal(t, 1, cnt2)
	var val float64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT value FROM metrics WHERE metric='pg_xact_commit'`).Scan(&val))
	require.InDelta(t, 1.0, val, 0.001)

	// Test statements routing
	truncateAll(t, pool)
	p3 := NewPipeline(pool, clock.NewFake(time.Now()))
	envStmt := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, PGVersion: 170000, Results: []wire.Result{{Check: "stat_statements", TS: time.Now(), Database: "db", QueryTexts: map[string]string{"123": "SELECT 1"}, Metrics: []wire.Metric{{Name: "calls", Value: 10, Kind: "gauge", Labels: map[string]string{"queryid": "123"}}}}}}}}
	res, err = p3.Process(context.Background(), envStmt)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics_statements WHERE queryid=123`).Scan(&cnt))
	require.Equal(t, 1, cnt)
	// check query_texts
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM query_texts WHERE queryid=123`).Scan(&cnt))
	require.Equal(t, 1, cnt)

	// Test ASH routing
	truncateAll(t, pool)
	envASH := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "ash", TS: time.Now(), Metrics: []wire.Metric{{Name: "samples", Value: 5, Kind: "gauge", Labels: map[string]string{"wait_event_type": "CPU", "wait_event": "CPU"}}}}}}}}
	res, err = p3.Process(context.Background(), envASH)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics_ash`).Scan(&cnt))
	require.Equal(t, 1, cnt)

	// Test replication routing
	truncateAll(t, pool)
	envRepl := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "replication", TS: time.Now(), Metrics: []wire.Metric{{Name: "write_lag_bytes", Value: 100, Kind: "gauge", Labels: map[string]string{"slot_name": "myslot"}}}}}}}}
	res, err = p3.Process(context.Background(), envRepl)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics_replication WHERE slot_name='myslot'`).Scan(&cnt))
	require.Equal(t, 1, cnt)

	// Test reset produces event
	truncateAll(t, pool)
	p4 := NewPipeline(pool, clock.NewFake(time.Now()))
	tsA := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tsB := tsA.Add(60 * time.Second)
	_, _ = p4.Process(context.Background(), wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: tsA, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 100, Kind: "counter"}}}}}}})
	_, _ = p4.Process(context.Background(), wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "bgwriter", TS: tsB, Metrics: []wire.Metric{{Name: "pg_xact_commit", Value: 5, Kind: "counter"}}}}}}})
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='counter_reset_detected'`).Scan(&cnt))
	require.GreaterOrEqual(t, cnt, 1)
	// also test unknown kind in ASH via real DB should reject
	res, err = p4.Process(context.Background(), wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, Results: []wire.Result{{Check: "ash", TS: time.Now(), Metrics: []wire.Metric{{Name: "x", Value: 1, Kind: "unknown"}}}}}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Rejected)
	_ = iid
	_ = cidDB
}
