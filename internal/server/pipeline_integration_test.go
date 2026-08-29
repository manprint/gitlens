//go:build integration

package server

import (
	"context"
	"encoding/json"
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

func TestINTLOCK004_LockSnapshotNewestWins(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	iid := uuid.NewString()
	cid := pgtype.ClusterID(70001).String()
	oldTS := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newTS := oldTS.Add(time.Minute)
	push := func(ts time.Time, tree string) {
		_, err := p.Process(context.Background(), wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent, Instances: []wire.Instance{{InstanceID: iid, ClusterID: cid, Results: []wire.Result{{Check: "locks", TS: ts, Facts: []wire.Fact{{Kind: "lock_tree", Key: "current", ValueJSON: []byte(tree)}}}}}}})
		require.NoError(t, err)
	}
	push(newTS, `{"nodes":[{"pid":2}]}`)
	push(oldTS, `{"nodes":[{"pid":1}]}`)
	var ts time.Time
	var tree []byte
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT ts, tree FROM lock_snapshots WHERE instance_id=$1`, uuid.MustParse(iid)).Scan(&ts, &tree))
	require.True(t, newTS.Equal(ts), "stored timestamp %s does not match newest timestamp %s", ts, newTS)
	var got struct {
		Nodes []struct {
			PID int `json:"pid"`
		} `json:"nodes"`
	}
	require.NoError(t, json.Unmarshal(tree, &got))
	require.Len(t, got.Nodes, 1)
	require.Equal(t, 2, got.Nodes[0].PID)
}

// INT-PIPE-003: a `stats_reset` change writes a `counter_reset_detected` event and no metric row for that interval;
// a query over the range shows a gap rather than a zero.
func TestPipeline_INT_PIPE_003_ResetDetection(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()

	instID := uuid.NewString()
	cid := pgtype.ClusterID(55555).String()

	// Use fake clock to control timestamps precisely
	p := NewPipeline(pool, clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(60 * time.Second)
	ts3 := ts2.Add(60 * time.Second)

	// First counter value: 1000
	env1 := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid,
			Results: []wire.Result{{
				Check: "bgwriter",
				TS:    ts1,
				Metrics: []wire.Metric{{
					Name:  "pg_xact_commit",
					Value: 1000,
					Kind:  "counter",
				}},
			}},
		}},
	}
	_, _ = p.Process(ctx, env1)

	// Second counter value: 1100 (normal increment, rate computed)
	env2 := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid,
			Results: []wire.Result{{
				Check: "bgwriter",
				TS:    ts2,
				Metrics: []wire.Metric{{
					Name:  "pg_xact_commit",
					Value: 1100,
					Kind:  "counter",
				}},
			}},
		}},
	}
	_, _ = p.Process(ctx, env2)

	// Verify metric was written for the normal interval
	var cnt1 int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit' AND ts=$1`, ts2).Scan(&cnt1))
	require.Equal(t, 1, cnt1, "should have metric for normal increment at ts2")

	// Third counter value: 50 (RESET - value went backwards)
	env3 := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid,
			Results: []wire.Result{{
				Check: "bgwriter",
				TS:    ts3,
				Metrics: []wire.Metric{{
					Name:  "pg_xact_commit",
					Value: 50,
					Kind:  "counter",
				}},
			}},
		}},
	}
	_, _ = p.Process(ctx, env3)

	// Verify counter_reset_detected event was emitted
	var eventCnt int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='counter_reset_detected'`).Scan(&eventCnt))
	require.Greater(t, eventCnt, 0, "should emit counter_reset_detected event")

	// Verify NO metric row was written for the reset interval (gap, not zero)
	var cntReset int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics WHERE metric='pg_xact_commit' AND ts=$1`, ts3).Scan(&cntReset))
	require.Equal(t, 0, cntReset, "should NOT write metric row for reset interval (creates a gap)")
}
