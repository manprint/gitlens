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
	"github.com/manprint/pglens/internal/store"
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

// TestPipeline_WritesCollectorSelfMetrics — check_failing,
// cardinality_budget_exceeded and clock_skew all compare series in `metrics`
// that nothing used to write, which made them unable to fire whatever
// happened. This asserts the rows actually land in the table the alert engine
// queries, with the values those rules need.
func TestPipeline_WritesCollectorSelfMetrics(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()

	received := time.Date(2026, 3, 1, 12, 0, 30, 0, time.UTC)
	sentAt := received.Add(-30 * time.Second)
	ts := sentAt.Add(-time.Second)
	p := NewPipeline(pool, clock.NewFake(received))

	instID := uuid.NewString()
	env := wire.Envelope{
		SentAt: sentAt,
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  pgtype.ClusterID(4242).String(),
			Results: []wire.Result{
				{Check: "bgwriter", TS: ts, Metrics: []wire.Metric{{Name: "pg_buffers_alloc", Value: 1, Kind: "gauge"}}},
				{Check: "conn_stats", TS: ts, Error: "permission denied for pg_stat_activity"},
				{Check: "table_stats", TS: ts, Truncated: true},
				{Check: "wal", TS: ts},
			},
		}},
	}
	res, err := p.Process(ctx, env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)

	for metric, want := range map[string]float64{
		"pglens_check_error_rate":           0.25,
		"pglens_cardinality_truncated_rate": 0.25,
		"pglens_agent_clock_skew_seconds":   30,
	} {
		var value float64
		var rowTS time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT value, ts FROM metrics WHERE metric=$1 AND instance_id=$2::uuid`, metric, instID).Scan(&value, &rowTS),
			"%s must be written for every push", metric)
		require.InDelta(t, want, value, 0.001, metric)
		require.Equal(t, ts.UTC(), rowTS.UTC(), "%s must sit on the envelope's own timeline", metric)
	}

	// The healthy push that follows must write the zero sample the rules need
	// in order to resolve.
	ts2 := ts.Add(time.Minute)
	env.SentAt = sentAt.Add(time.Minute)
	env.Instances[0].Results = []wire.Result{{Check: "bgwriter", TS: ts2, Metrics: []wire.Metric{{Name: "pg_buffers_alloc", Value: 2, Kind: "gauge"}}}}
	_, err = p.Process(ctx, env)
	require.NoError(t, err)

	var errRate float64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT value FROM metrics WHERE metric='pglens_check_error_rate' AND instance_id=$1::uuid AND ts=$2`, instID, ts2).Scan(&errRate))
	require.Zero(t, errRate)
}

// TestPipeline_ReplicationSyncStateIsStored — the streaming check labelled the
// value "sync_mode" while the pipeline read "sync_state", so
// metrics_replication.sync_state was NULL on every row ever written: the
// cluster page's Sync column read "Unknown" for every standby and
// replica.no_sync_standby had nothing to count.
func TestPipeline_ReplicationSyncStateIsStored(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	p := NewPipeline(pool, clock.NewFake(time.Now()))

	for _, label := range []string{"sync_state", "sync_mode"} {
		t.Run(label, func(t *testing.T) {
			truncateAll(t, pool) //nolint:contextcheck // the helper clears the tables with its own context, as every test does; this test's ctx is only for the pipeline call
			env := wire.Envelope{Instances: []wire.Instance{{
				InstanceID: uuid.NewString(),
				ClusterID:  pgtype.ClusterID(777).String(),
				Results: []wire.Result{{Check: "replication_streaming", TS: time.Now(), Metrics: []wire.Metric{{
					Name:   "replication_replay_lag_seconds",
					Value:  3,
					Kind:   "gauge",
					Labels: map[string]string{"standby": "pg-standby", label: "sync"},
				}}}},
			}}}
			res, err := p.Process(ctx, env)
			require.NoError(t, err)
			require.Equal(t, 1, res.Accepted)

			var syncState *string
			require.NoError(t, pool.QueryRow(ctx, `SELECT sync_state FROM metrics_replication`).Scan(&syncState))
			require.NotNil(t, syncState, "sync_state must be persisted, not left NULL")
			require.Equal(t, "sync", *syncState)
		})
	}
}

// TestPipeline_RetryAfterRolledBackEnvelopeKeepsRate — XR-08: the agent sends an
// envelope again when its push fails with a 5xx. When the first attempt's write
// failed, its counter baseline must not have moved, so the retry still stores
// the rate of that interval. Before the fix the baseline had already advanced,
// and the retry was dropped as a replay.
func TestPipeline_RetryAfterRolledBackEnvelopeKeepsRate(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()

	// A trigger that fails metrics writes while the switch row is set. It stands
	// in for a transient failure of the transaction after the observations.
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS pglens_test_fail_writes (id int PRIMARY KEY, fail boolean NOT NULL)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO pglens_test_fail_writes (id, fail) VALUES (1, false) ON CONFLICT (id) DO UPDATE SET fail = false`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE OR REPLACE FUNCTION pglens_test_fail_writes_fn() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF EXISTS (SELECT 1 FROM pglens_test_fail_writes WHERE fail) THEN
				RAISE EXCEPTION 'injected write failure';
			END IF;
			RETURN NEW;
		END $$`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TRIGGER pglens_test_fail_writes BEFORE INSERT ON metrics FOR EACH ROW EXECUTE FUNCTION pglens_test_fail_writes_fn()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS pglens_test_fail_writes ON metrics`)
		_, _ = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS pglens_test_fail_writes_fn()`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS pglens_test_fail_writes`)
	})
	setFail := func(fail bool) {
		t.Helper()
		_, err := pool.Exec(ctx, `UPDATE pglens_test_fail_writes SET fail = $1 WHERE id = 1`, fail)
		require.NoError(t, err)
	}

	instID := uuid.NewString()
	cid := pgtype.ClusterID(55556).String()
	p := NewPipeline(pool, clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))

	ts1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(60 * time.Second)
	envAt := func(ts time.Time, value float64) wire.Envelope {
		return wire.Envelope{
			Instances: []wire.Instance{{
				InstanceID: instID,
				ClusterID:  cid,
				Results: []wire.Result{{
					Check: "bgwriter",
					TS:    ts,
					Metrics: []wire.Metric{{
						Name:  "pg_xact_commit",
						Value: value,
						Kind:  "counter",
					}},
				}},
			}},
		}
	}

	_, err = p.Process(ctx, envAt(ts1, 1000))
	require.NoError(t, err)

	// First attempt: the write fails after the counter was observed.
	setFail(true)
	_, err = p.Process(ctx, envAt(ts2, 1100))
	require.ErrorContains(t, err, "injected write failure")

	// The agent re-sends the same envelope once the store is healthy again.
	setFail(false)
	_, err = p.Process(ctx, envAt(ts2, 1100))
	require.NoError(t, err)

	var stored int
	var rate float64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*), coalesce(max(value), 0) FROM metrics WHERE metric='pg_xact_commit' AND ts=$1`, ts2).Scan(&stored, &rate))
	require.Equal(t, 1, stored, "the retry must store the rate of the interval the failed attempt observed")
	require.InDelta(t, 100.0/60.0, rate, 1e-9)
}

// TestPipeline_TruncationMarkerCoversItsWindow: a cap that holds is marked once per
// window on the instance, not on every push, and a statements read whose window
// starts after the marker still sees the cap it recorded.
func TestPipeline_TruncationMarkerCoversItsWindow(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	instID := uuid.New()
	cid := pgtype.ClusterID(1).String()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	push := func(ts time.Time) {
		_, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{
			InstanceID: instID.String(), ClusterID: cid, Role: "primary",
			Results: []wire.Result{{Check: "stat_statements", TS: ts, Database: "app", Truncated: true}},
		}}})
		require.NoError(t, err)
	}
	markers := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE instance_id=$1 AND type='cardinality_truncated'`, instID).Scan(&n))
		return n
	}

	push(base)
	push(base.Add(15 * time.Second))
	push(base.Add(30 * time.Second))
	require.Equal(t, 1, markers(), "pushes inside one window write one marker")
	// The check_partial fact of these pushes ends at the last one, before this
	// window, so only the marker can make the window truncated.
	from, to := base.Add(5*time.Minute), base.Add(6*time.Minute)
	truncated, err := NewAPI(pool).instanceTruncated(ctx, instID, &from, &to)
	require.NoError(t, err)
	require.True(t, truncated, "a window starting five minutes after the marker is still truncated")

	push(base.Add(11 * time.Minute))
	require.Equal(t, 2, markers(), "the next window writes the next marker")
}

// INT-PIPE-004: an edge that names an address with no port resolves only to an
// instance nothing else of the cluster shares the address with. An address two
// instances of one cluster report names no upstream, whether the two are in the
// database or in the envelope, so no edge is stored for it. A unique address
// still resolves. Before, the database and the envelope each attached the edge
// to whichever instance they met first.
func TestPipeline_INT_PIPE_004_AmbiguousAddressNamesNoUpstream(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE topology_edges`)
	require.NoError(t, err)
	cid := pgtype.ClusterID(404)
	agent := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual') ON CONFLICT DO NOTHING`, store.ToDB(cid))
	require.NoError(t, err)

	shared1, shared2, unique := uuid.New(), uuid.New(), uuid.New()
	for _, row := range []struct {
		id   uuid.UUID
		addr string
	}{{shared1, "10.0.4.5"}, {shared2, "10.0.4.5"}, {unique, "10.0.4.7"}} {
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,$4,0,170000,'primary','T0', now())`, row.id, store.ToDB(cid), agent, row.addr)
		require.NoError(t, err)
	}

	// The standby's edges name the shared address, which resolves to no one, and
	// the unique one, which resolves to its instance.
	standby := uuid.New()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{
		InstanceID: standby.String(), ClusterID: cid.String(), Addr: "10.0.4.9", Port: 5432, PGVersion: 170000, Role: "standby",
		TopologyEdges: []wire.Edge{
			{From: standby.String(), To: "10.0.4.5", Type: "streaming", Confidence: "high"},
			{From: standby.String(), To: "10.0.4.7", Type: "streaming", Confidence: "high"},
		},
	}}})
	require.NoError(t, err)
	require.Zero(t, res.Rejected)
	var upstreams []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT to_instance FROM topology_edges WHERE from_instance=$1`, standby)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var to uuid.UUID
		require.NoError(t, rows.Scan(&to))
		upstreams = append(upstreams, to)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []uuid.UUID{unique}, upstreams, "only the unambiguous address names an upstream")

	// The same ambiguity inside one envelope: two instances report the address
	// and none of them is in the database yet.
	_, err = pool.Exec(ctx, `TRUNCATE topology_edges`)
	require.NoError(t, err)
	standby2 := uuid.New()
	res, err = p.Process(ctx, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: uuid.NewString(), ClusterID: cid.String(), Addr: "10.0.4.8", Port: 0, PGVersion: 170000, Role: "primary"},
		{InstanceID: uuid.NewString(), ClusterID: cid.String(), Addr: "10.0.4.8", Port: 0, PGVersion: 170000, Role: "primary"},
		{InstanceID: standby2.String(), ClusterID: cid.String(), Addr: "10.0.4.9", Port: 5432, PGVersion: 170000, Role: "standby",
			TopologyEdges: []wire.Edge{{From: standby2.String(), To: "10.0.4.8", Type: "streaming", Confidence: "high"}}},
	}})
	require.NoError(t, err)
	require.Zero(t, res.Rejected)
	var edges int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM topology_edges WHERE from_instance=$1`, standby2).Scan(&edges))
	require.Zero(t, edges, "two instances of the envelope report the address, so it names no upstream")
}

// TestPipeline_RetriedEnvelopeCountsEachCheckOnce: the agent resends an
// envelope whose response it lost, so a committed envelope can arrive twice.
// Both deliveries store the same rows, and the check counters must count the
// scrape once. They used to be incremented before the commit, once per
// delivery.
func TestPipeline_RetriedEnvelopeCountsEachCheckOnce(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	instID := uuid.New()
	cid := pgtype.ClusterID(1).String()
	ts := time.Now().Add(-time.Minute).Truncate(time.Second)
	env := wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID.String(), ClusterID: cid, Role: "primary",
		Results: []wire.Result{
			{Check: "activity", TS: ts, Database: "app"},
			{Check: "stat_statements", TS: ts, Database: "app", Truncated: true},
		},
	}}}
	okBefore := pglensCheckOKTotal.Get(checkKey{instance: instID.String(), check: "activity"})

	for range 2 {
		res, err := p.Process(ctx, env)
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted)
	}

	require.Equal(t, okBefore+1, pglensCheckOKTotal.Get(checkKey{instance: instID.String(), check: "activity"}),
		"a scrape delivered twice is counted once")
	var markers int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE instance_id=$1 AND type='cardinality_truncated'`, instID).Scan(&markers))
	require.Equal(t, 1, markers, "the window marker is not written again on the retry")

	// The next scrape is new, and it counts.
	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID.String(), ClusterID: cid, Role: "primary",
		Results: []wire.Result{{Check: "activity", TS: ts.Add(time.Minute), Database: "app"}},
	}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, okBefore+2, pglensCheckOKTotal.Get(checkKey{instance: instID.String(), check: "activity"}))
}

// TestPipeline_InvalidEdgeConfidenceIsRefusedAlone: a newer agent may send an
// edge confidence the store does not admit. The edge is refused and counted,
// and the rest of the envelope is stored. The CHECK used to fail the whole
// envelope, which an agent then resends forever.
func TestPipeline_InvalidEdgeConfidenceIsRefusedAlone(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(405).String()
	standby := uuid.New()
	before := pglensIngestRejectedTotal.Get("invalid_edge")

	// The edge names the primary, which is in the envelope, so it resolves and
	// reaches the store.
	primary := uuid.New()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{
		InstanceID: primary.String(), ClusterID: cid, Addr: "10.0.5.1", Port: 5432, PGVersion: 170000, Role: "primary",
	}, {
		InstanceID: standby.String(), ClusterID: cid, Addr: "10.0.5.9", Port: 5432, PGVersion: 170000, Role: "standby",
		Results: []wire.Result{{Check: "activity", TS: time.Now().Add(-time.Minute), Database: "app"}},
		TopologyEdges: []wire.Edge{
			{From: standby.String(), To: "10.0.5.1", Port: 5432, Type: "streaming", Confidence: "medium"},
		},
	}}})
	require.NoError(t, err)
	require.Equal(t, 2, res.Accepted, "both instances are stored")
	require.Zero(t, res.Rejected)
	require.Equal(t, before+1, pglensIngestRejectedTotal.Get("invalid_edge"))
	var edges int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM topology_edges WHERE from_instance=$1`, standby).Scan(&edges))
	require.Zero(t, edges, "an edge the store would refuse is not written")
}

// TestPipeline_RejectedInstanceCountsNoCheck: an instance the pipeline refuses
// stores none of its results, so its checks must not count as run. The
// counters used to count them before the refusal.
func TestPipeline_RejectedInstanceCountsNoCheck(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	bad, good := uuid.New(), uuid.New()
	cid := pgtype.ClusterID(406).String()
	ts := time.Now().Add(-time.Minute).Truncate(time.Second)
	badOK := pglensCheckOKTotal.Get(checkKey{instance: bad.String(), check: "activity"})
	goodOK := pglensCheckOKTotal.Get(checkKey{instance: good.String(), check: "activity"})

	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{
		{InstanceID: bad.String(), ClusterID: cid, Role: "primary", Results: []wire.Result{{
			Check: "activity", TS: ts, Database: "app",
			Metrics: []wire.Metric{{Name: "pg_stat_activity_count", Value: 1, Kind: "not-a-kind"}},
		}}},
		{InstanceID: good.String(), ClusterID: cid, Role: "primary", Results: []wire.Result{{Check: "activity", TS: ts, Database: "app"}}},
	}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Equal(t, badOK, pglensCheckOKTotal.Get(checkKey{instance: bad.String(), check: "activity"}), "a refused instance counts no check")
	require.Equal(t, goodOK+1, pglensCheckOKTotal.Get(checkKey{instance: good.String(), check: "activity"}))
}

// INT-PIPE-005: a skipped result is stored as a skip fact, and once its envelope
// commits it counts neither as a success nor as an error. The plain result in
// the same envelope shows the instance was stored and the counters did move, so
// the test cannot pass because nothing was counted at all.
func TestPipeline_INT_PIPE_005_SkippedResultCountsNoCheck(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	instID := uuid.New()
	cid := pgtype.ClusterID(408).String()
	ts := time.Now().Add(-time.Minute).Truncate(time.Second)
	skipped := checkKey{instance: instID.String(), check: "wal"}
	plain := checkKey{instance: instID.String(), check: "activity"}
	okBefore := pglensCheckOKTotal.Get(skipped)
	errBefore := pglensCheckErrorTotal.Get(skipped)
	plainBefore := pglensCheckOKTotal.Get(plain)

	res, err := p.Process(ctx, wire.Envelope{Instances: []wire.Instance{{
		InstanceID: instID.String(), ClusterID: cid, Role: "primary",
		Results: []wire.Result{
			{Check: "wal", TS: ts, Database: "app", SkipReason: "needs pg_read_all_stats"},
			{Check: "activity", TS: ts, Database: "app"},
		},
	}}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Zero(t, res.Rejected)
	require.Equal(t, plainBefore+1, pglensCheckOKTotal.Get(plain), "the stored plain result counts")

	var skips int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE instance_id=$1 AND kind='check_skip' AND key='wal'`, instID).Scan(&skips))
	require.Equal(t, 1, skips, "the skip is stored as a fact")
	require.Equal(t, okBefore, pglensCheckOKTotal.Get(skipped), "a skipped result is not a success")
	require.Equal(t, errBefore, pglensCheckErrorTotal.Get(skipped), "a skipped result is not an error")
}

// TestPipeline_INT_PIPE_009_QueryTextWithACredentialSignIsWithheld: an agent
// built before the redaction fix ships a statement's text as it was typed. The
// server stores a text with a sign of a credential as withheld, and a text
// without one as it is.
func TestPipeline_INT_PIPE_009_QueryTextWithACredentialSignIsWithheld(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	instID := uuid.NewString()
	cid := pgtype.ClusterID(99998).String()
	texts := map[string]string{"41": "ALTER ROLE tmp PASSWORD 'hunter2'", "42": "SELECT 1"}
	result := wire.Result{Check: "stat_statements", TS: time.Now(), Database: "app", QueryTexts: texts}
	env := wire.Envelope{Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid, PGVersion: 170000, Results: []wire.Result{result}}}}
	res, err := p.Process(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)

	stored := map[int64]string{}
	rows, err := pool.Query(context.Background(), `SELECT queryid, query_text FROM query_texts WHERE datname='app' ORDER BY queryid`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id int64
		var text string
		require.NoError(t, rows.Scan(&id, &text))
		stored[id] = text
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[int64]string{41: "[redacted]", 42: "SELECT 1"}, stored)
}
