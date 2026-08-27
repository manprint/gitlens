//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/stretchr/testify/require"
)

// INT-API-010: After ingesting a primary-standby pair, /clusters reports one
// primary, one standby and one edge.
func TestAPI_Topology_INT_API_010_PrimaryStandbyCluster(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := pgtype.ClusterID(111)
	cidDB := store.ToDB(cid)

	// Insert cluster
	_, err := pool.Exec(context.Background(),
		`INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`,
		cidDB)
	require.NoError(t, err)

	// Insert primary instance
	primaryID := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'primary.local',5432,170000,'primary','T0', now())`,
		primaryID, cidDB, agent1)
	require.NoError(t, err)

	// Insert standby instance
	standbyID := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'standby.local',5432,170000,'standby','T0', now())`,
		standbyID, cidDB, agent2)
	require.NoError(t, err)

	// Insert one replication edge
	_, err = pool.Exec(context.Background(),
		`INSERT INTO topology_edges (tenant_id, cluster_id, from_instance, to_instance, edge_type, sync_state, confidence)
		 VALUES ('default',$1,$2,$3,'streaming','async','high')`,
		cidDB, standbyID, primaryID)
	require.NoError(t, err)

	// Query /api/v1/clusters
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)

	c := resp[0]
	require.Equal(t, cid.String(), c.ClusterID)
	require.NotNil(t, c.Primary)
	require.Equal(t, primaryID.String(), *c.Primary)
	require.Equal(t, 2, c.InstanceCount)
	require.Equal(t, 1, c.StandbyCount)
	require.Len(t, c.Topology, 1)
	require.Equal(t, standbyID.String(), c.Topology[0].From)
	require.Equal(t, primaryID.String(), c.Topology[0].To)
	require.Equal(t, "streaming", c.Topology[0].Type)
	require.NotNil(t, c.Topology[0].SyncState)
	require.Equal(t, "async", *c.Topology[0].SyncState)
	require.Equal(t, "high", c.Topology[0].Confidence)
}

// INT-API-011: After a promote, `primary` points at the new instance and
// `/clusters/{id}/topology` lists the `failover_detected` event.
func TestAPI_Topology_INT_API_011_PromoteUpdatesTopology(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := pgtype.ClusterID(222)
	cidDB := store.ToDB(cid)

	// Insert cluster
	_, err := pool.Exec(context.Background(),
		`INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`,
		cidDB)
	require.NoError(t, err)

	// Insert old primary
	oldPrimaryID := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'old-primary.local',5432,170000,'standby','T0', now())`,
		oldPrimaryID, cidDB, agent1)
	require.NoError(t, err)

	// Insert new primary (formerly standby)
	newPrimaryID := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'new-primary.local',5432,170000,'primary','T0', now())`,
		newPrimaryID, cidDB, agent2)
	require.NoError(t, err)

	// Insert failover_detected event
	payload := json.RawMessage(`{"old_primary":"` + oldPrimaryID.String() + `","new_primary":"` + newPrimaryID.String() + `"}`)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO events (tenant_id, cluster_id, type, instance_id, payload, ts) VALUES ('default',$1,'failover_detected',$2,$3, now())`,
		cidDB, newPrimaryID, payload)
	require.NoError(t, err)

	// Query /api/v1/clusters/{id}/topology
	topo := NewTopologyAPI(pool)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/clusters/%s/topology", cid.String()), http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp topologyResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, cid.String(), resp.ClusterID)
	require.Len(t, resp.Events, 1)
	require.Equal(t, "failover_detected", resp.Events[0].Type)
	require.NotNil(t, resp.Events[0].ClusterID)
	require.Equal(t, cid.String(), *resp.Events[0].ClusterID)
}

// INT-API-012: A lagging replica flips `health` to `degraded`.
func TestAPI_Topology_INT_API_012_LagDegradedHealth(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := pgtype.ClusterID(333)
	cidDB := store.ToDB(cid)

	// Insert cluster
	_, err := pool.Exec(context.Background(),
		`INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`,
		cidDB)
	require.NoError(t, err)

	// Insert primary
	primaryID := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'primary.local',5432,170000,'primary','T0', now())`,
		primaryID, cidDB, agent1)
	require.NoError(t, err)

	// Insert standby
	standbyID := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'standby.local',5432,170000,'standby','T0', now())`,
		standbyID, cidDB, agent2)
	require.NoError(t, err)

	// Insert replication metric with lag
	_, err = pool.Exec(context.Background(),
		`INSERT INTO metrics_replication (tenant_id, cluster_id, instance_id, upstream_id, edge_type, replay_lag_sec, ts)
		 VALUES ('default',$1,$2,$3,'streaming', 150.5, now())`,
		cidDB, standbyID, primaryID)
	require.NoError(t, err)

	// Query /api/v1/clusters
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)

	c := resp[0]
	// Should be degraded because lag > 0
	require.Equal(t, "degraded", c.Health)
	require.NotNil(t, c.MaxReplayLagSeconds)
	require.Greater(t, *c.MaxReplayLagSeconds, 0.0)
}

// INT-API-013: An unresolvable edge is returned with confidence: "low" and a note.
func TestAPI_Topology_INT_API_013_LowConfidenceEdgeWithNote(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := pgtype.ClusterID(444)
	cidDB := store.ToDB(cid)

	// Insert cluster
	_, err := pool.Exec(context.Background(),
		`INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`,
		cidDB)
	require.NoError(t, err)

	// Insert standby instance
	standbyID := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'standby.local',5432,170000,'standby','T0', now())`,
		standbyID, cidDB, agent1)
	require.NoError(t, err)

	// Insert edge with low confidence (unresolved upstream)
	unknownUpstream := uuid.New()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO topology_edges (tenant_id, cluster_id, from_instance, to_instance, edge_type, confidence)
		 VALUES ('default',$1,$2,$3,'streaming','low')`,
		cidDB, standbyID, unknownUpstream)
	require.NoError(t, err)

	// Query /api/v1/clusters
	api := NewAPI(pool)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp []clusterResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp, 1)

	c := resp[0]
	require.Len(t, c.Topology, 1)
	edge := c.Topology[0]
	require.Equal(t, "low", edge.Confidence)
	require.NotNil(t, edge.Note)
	require.Contains(t, *edge.Note, "could not be resolved")
}

// TestAPI_Topology_ReplicationSeries_WithMetrics tests the replication
// endpoint returns lag series with null gaps.
func TestAPI_Topology_ReplicationSeries_WithMetrics(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := pgtype.ClusterID(555)
	cidDB := store.ToDB(cid)

	// Insert cluster
	_, err := pool.Exec(context.Background(),
		`INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`,
		cidDB)
	require.NoError(t, err)

	// Insert standby instance
	standbyID := uuid.New()
	agent1 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent1)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'standby.local',5432,170000,'standby','T0', now())`,
		standbyID, cidDB, agent1)
	require.NoError(t, err)

	// Insert primary instance
	primaryID := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen)
		 VALUES ($1,'default',$2,$3,'primary.local',5432,170000,'primary','T0', now())`,
		primaryID, cidDB, agent2)
	require.NoError(t, err)

	// Insert replication metrics
	now := time.Now().UTC()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO metrics_replication (tenant_id, cluster_id, instance_id, upstream_id, edge_type, sync_state, ts, replay_lag_sec)
		 VALUES ('default',$1,$2,$3,'streaming','async',$4, 50.0)`,
		cidDB, standbyID, primaryID, now)
	require.NoError(t, err)

	// Query /api/v1/clusters/{id}/replication
	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)
	topo := NewTopologyAPI(pool)
	r := chi.NewRouter()
	topo.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/clusters/%s/replication?from=%s&to=%s",
			cid.String(),
			from.Format(time.RFC3339),
			to.Format(time.RFC3339)),
		http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp replicationResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, cid.String(), resp.ClusterID)
	// One row in metrics_replication expands into one edge per lag metric
	// (write/flush/replay) so a consumer can chart each independently and see
	// "null gaps" for the metrics that weren't reported — only replay_lag_sec
	// was inserted here, so write/flush come back with a nil series value.
	require.Len(t, resp.Edges, 3)

	byMetric := make(map[string]replicationSeriesResp, len(resp.Edges))
	for _, e := range resp.Edges {
		require.Equal(t, standbyID.String(), e.From)
		require.Equal(t, primaryID.String(), e.To)
		require.NotNil(t, e.SyncState)
		require.Equal(t, "async", *e.SyncState)
		require.Len(t, e.Series, 1)
		byMetric[e.Metric] = e
	}

	replay, ok := byMetric["replay_lag_sec"]
	require.True(t, ok, "expected a replay_lag_sec edge")
	require.NotNil(t, replay.Series[0].Value)
	require.Equal(t, 50.0, *replay.Series[0].Value)

	for _, metric := range []string{"write_lag_sec", "flush_lag_sec"} {
		e, ok := byMetric[metric]
		require.True(t, ok, "expected a %s edge", metric)
		require.Nil(t, e.Series[0].Value, "%s was never inserted, so its series value must be a null gap, not 0", metric)
	}
}
