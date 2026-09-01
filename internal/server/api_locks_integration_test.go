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
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestINTLOCK005_LockSnapshotRoundTrip(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	id := uuid.New()
	tree := []byte(`{"nodes":[{"pid":123,"blocked_by":[456]}]}`)
	_, err := pool.Exec(context.Background(), `INSERT INTO lock_snapshots (tenant_id,instance_id,cluster_id,ts,tree) VALUES ('default',$1,$2,now(),$3::jsonb)`, id, int64(pgtype.ClusterID(90001)), tree)
	require.NoError(t, err)

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/locks?instance_id="+id.String(), http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	var got lockAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.Stale)
	require.Len(t, got.Nodes, 1)
	var node struct {
		PID       int   `json:"pid"`
		BlockedBy []int `json:"blocked_by"`
	}
	require.NoError(t, json.Unmarshal(got.Nodes[0], &node))
	require.Equal(t, 123, node.PID)
	require.Equal(t, []int{456}, node.BlockedBy)
}

func TestINTACT003_ActivityEndpointReportsDatabase(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	id := uuid.New()
	cid := int64(pgtype.ClusterID(90002))
	ts := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `INSERT INTO metrics (ts,tenant_id,cluster_id,instance_id,datname,metric,labels,series_id,value) VALUES ($1,'default',$2,$3,'','pg_connections_by_database','{"datname":"app"}',$4,3)`, ts, cid, id, int64(91001))
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO metrics (ts,tenant_id,cluster_id,instance_id,datname,metric,labels,series_id,value) VALUES ($1,'default',$2,$3,'','pg_deadlocks_total','{"database":"app"}',$4,2)`, ts, cid, id, int64(91002))
	require.NoError(t, err)

	r := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+id.String()+"/activity", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code)
	var got activityAPIResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.False(t, got.Stale)
	require.Len(t, got.Metrics["pg_connections_by_database"], 1)
	require.Equal(t, float64(3), got.Metrics["pg_connections_by_database"][0].Value)
	require.Len(t, got.Metrics["pg_deadlocks_total"], 1)
	require.Equal(t, float64(2), got.Metrics["pg_deadlocks_total"][0].Value)
}
