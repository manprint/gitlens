//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// TestPipeline_INT_PIPE_007_RetriedStandbyPushCountsOnce — a standby whose
// upstream stops resolving opens an orphan episode at t=0. Its cycle at t=90
// reaches the server twice, because the first response was lost and the agent
// sent the same envelope again. The retry is the same cycle, so the episode
// stays open and the evidence at t=180 fires with the whole episode as its
// duration. Counted as a second cycle, the retry would end the episode at t=90,
// and the evidence at t=180 would start a new one and fire nothing.
func TestPipeline_INT_PIPE_007_RetriedStandbyPushCountsOnce(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFake(start)
	p := NewPipeline(pool, clk)

	primaryID := uuid.NewString()
	standbyID := uuid.NewString()
	cid := pgtype.ClusterID(5151).String()
	const primaryAddr = "10.0.0.1"
	// envelope is one push from the agent, sent at sentAt. The standby reports
	// its upstream edge only when linked is true; without it the standby is
	// quiet, which is a cycle without orphan evidence.
	envelope := func(sentAt time.Time, linked bool) wire.Envelope {
		standby := wire.Instance{InstanceID: standbyID, ClusterID: cid, Role: "standby"}
		if linked {
			standby.TopologyEdges = []wire.Edge{{From: standbyID, To: primaryAddr, Port: 5432, Type: "streaming", Confidence: "low"}}
		}
		return wire.Envelope{SentAt: sentAt, Instances: []wire.Instance{
			{InstanceID: primaryID, ClusterID: cid, Role: "primary", Addr: primaryAddr, Port: 5432},
			standby,
		}}
	}

	_, err := p.Process(ctx, envelope(start, true)) // t=0: evidence starts the episode
	require.NoError(t, err)

	clk.Advance(90 * time.Second)
	quiet := envelope(start.Add(90*time.Second), false)
	_, err = p.Process(ctx, quiet) // t=90: a cycle without evidence
	require.NoError(t, err)
	_, err = p.Process(ctx, quiet) // the same envelope, retried after a lost response
	require.NoError(t, err)

	clk.Advance(90 * time.Second)
	_, err = p.Process(ctx, envelope(start.Add(180*time.Second), true)) // t=180: evidence again
	require.NoError(t, err)

	rows, err := pool.Query(ctx,
		`SELECT (payload->>'duration_seconds')::float8 FROM events WHERE type='orphan_standby' AND instance_id=$1::uuid ORDER BY ts`, standbyID)
	require.NoError(t, err)
	defer rows.Close()
	var durations []float64
	for rows.Next() {
		var d float64
		require.NoError(t, rows.Scan(&d))
		durations = append(durations, d)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []float64{180}, durations, "the retried cycle must not end the orphan episode")
}

// TestPipeline_INT_PIPE_008_PrimaryEdgeIsNotStored — only a standby has an
// upstream. An agent older than a382022 still reports an edge from a primary,
// and the server must not store it. The rows the primary already had are
// cleared by the same push, since the write replaces every row from it.
func TestPipeline_INT_PIPE_008_PrimaryEdgeIsNotStored(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	p := NewPipeline(pool, clock.NewFake(start))

	primaryID := uuid.NewString()
	standbyID := uuid.NewString()
	cid := pgtype.ClusterID(5152).String()
	const standbyAddr = "10.0.0.2"
	envelope := func(primaryEdges []wire.Edge) wire.Envelope {
		return wire.Envelope{SentAt: start, Instances: []wire.Instance{
			{InstanceID: primaryID, ClusterID: cid, Role: "primary", Addr: "10.0.0.1", Port: 5432, TopologyEdges: primaryEdges},
			{InstanceID: standbyID, ClusterID: cid, Role: "standby", Addr: standbyAddr, Port: 5432},
		}}
	}
	storedEdges := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT count(*) FROM topology_edges WHERE from_instance=$1::uuid`, primaryID).Scan(&n))
		return n
	}

	// A row the primary wrote before the ingest check existed.
	_, err := pool.Exec(ctx,
		`INSERT INTO topology_edges (tenant_id, cluster_id, from_instance, to_instance, edge_type, confidence)
		 VALUES ('default', $1, $2::uuid, $3::uuid, 'streaming', 'high')`,
		store.ToDB(pgtype.ClusterID(5152)), primaryID, standbyID)
	require.NoError(t, err)
	require.Equal(t, 1, storedEdges())

	_, err = p.Process(ctx, envelope([]wire.Edge{{From: primaryID, To: standbyAddr, Port: 5432, Type: "streaming", Confidence: "high"}}))
	require.NoError(t, err)
	require.Equal(t, 0, storedEdges(), "a primary edge must not be stored, and the stored row must be cleared")
}
