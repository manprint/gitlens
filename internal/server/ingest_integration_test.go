//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"testing"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// TestIngest_EndToEnd_PushThenClusters closes V002-F01's "Test" requirement:
// it proves the wired router (auth -> inventory.Upsert -> pipeline.Process ->
// API) actually persists a pushed envelope and serves it back, end to end,
// against a real database. Without the http.go/main.go wiring this endpoint
// would never be reachable at all.
func TestIngest_EndToEnd_PushThenClusters(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	auth := NewAuth("test-token")
	inv := NewInventory(pool)
	pipeline := NewPipeline(pool, nil)
	api := NewAPI(pool)
	topoAPI := NewTopologyAPI(pool)
	ashAPI := NewAshAPI(pool)
	router := NewRouter(UIConfig{}, nil, nil, auth, inv, pipeline, api, topoAPI, ashAPI)

	instID := uuid.NewString()
	cid := pgtype.ClusterID(987654321).String()
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          time.Now(),
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid,
			Role:       "primary",
			Results: []wire.Result{{
				Check: "bgwriter",
				TS:    time.Now(),
				Metrics: []wire.Metric{{
					Name:  "pg_buffers_checkpoint",
					Value: 7,
					Kind:  "gauge",
				}},
			}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)

	pushReq := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	pushReq.Header.Set("Authorization", "Bearer test-token")
	pushRec := httptest.NewRecorder()
	router.ServeHTTP(pushRec, pushReq)
	require.Equal(t, http.StatusAccepted, pushRec.Code, pushRec.Body.String())

	var pushResp struct {
		Accepted int `json:"accepted"`
		Rejected int `json:"rejected"`
	}
	require.NoError(t, json.Unmarshal(pushRec.Body.Bytes(), &pushResp))
	require.Equal(t, 1, pushResp.Accepted)
	require.Equal(t, 0, pushResp.Rejected)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", http.NoBody)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	var clusters []clusterResp
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &clusters))
	require.Len(t, clusters, 1)
	require.Equal(t, cid, clusters[0].ClusterID)
	require.Equal(t, 1, clusters[0].InstanceCount)
}

// INT-INGEST-002: the identical envelope posted twice leaves the row count unchanged (idempotency, I-3).
func TestIngest_INT_INGEST_002_IdempotentPush(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	auth := NewAuth("test-token")
	inv := NewInventory(pool)
	pipeline := NewPipeline(pool, nil)
	api := NewAPI(pool)
	topoAPI := NewTopologyAPI(pool)
	ashAPI := NewAshAPI(pool)
	router := NewRouter(UIConfig{}, nil, nil, auth, inv, pipeline, api, topoAPI, ashAPI)

	instID := uuid.NewString()
	cid := pgtype.ClusterID(111222333).String()
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          time.Now(),
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid,
			Role:       "primary",
			Results: []wire.Result{{
				Check: "bgwriter",
				TS:    time.Now(),
				Metrics: []wire.Metric{{
					Name:  "pg_buffers_checkpoint",
					Value: 42,
					Kind:  "gauge",
				}},
			}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)

	// First push
	pushReq1 := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	pushReq1.Header.Set("Authorization", "Bearer test-token")
	pushRec1 := httptest.NewRecorder()
	router.ServeHTTP(pushRec1, pushReq1)
	require.Equal(t, http.StatusAccepted, pushRec1.Code, pushRec1.Body.String())

	var pushResp1 struct {
		Accepted int `json:"accepted"`
		Rejected int `json:"rejected"`
	}
	require.NoError(t, json.Unmarshal(pushRec1.Body.Bytes(), &pushResp1))
	require.Equal(t, 1, pushResp1.Accepted)

	// Count instances (upsert semantics alone already guarantee this stays
	// at 1 regardless of metric-row idempotency) AND metrics rows (the
	// actual I-3 signal: metrics(series_id, ts) is uniquely indexed with
	// ON CONFLICT DO NOTHING — re-posting the identical envelope carries
	// the identical ts, so a real dedup failure would show up here, not
	// in the instances count).
	var count1, metricsCount1 int
	ctx := context.Background()
	err = pool.QueryRow(ctx, "SELECT count(*) FROM instances").Scan(&count1)
	require.NoError(t, err)
	require.Equal(t, 1, count1, "should have 1 instance after first push")
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics").Scan(&metricsCount1)
	require.NoError(t, err)
	require.Equal(t, 1, metricsCount1, "should have 1 metric row after first push")

	// Second identical push
	pushReq2 := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	pushReq2.Header.Set("Authorization", "Bearer test-token")
	pushRec2 := httptest.NewRecorder()
	router.ServeHTTP(pushRec2, pushReq2)
	require.Equal(t, http.StatusAccepted, pushRec2.Code, pushRec2.Body.String())

	var pushResp2 struct {
		Accepted int `json:"accepted"`
		Rejected int `json:"rejected"`
	}
	require.NoError(t, json.Unmarshal(pushRec2.Body.Bytes(), &pushResp2))
	require.Equal(t, 1, pushResp2.Accepted)

	// Counts after the second, identical push should be unchanged — the
	// metrics count is the real I-3 assertion (see comment above).
	var count2, metricsCount2 int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM instances").Scan(&count2)
	require.NoError(t, err)
	require.Equal(t, count1, count2, "instance count should be unchanged after identical second push (idempotent)")
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics").Scan(&metricsCount2)
	require.NoError(t, err)
	require.Equal(t, metricsCount1, metricsCount2, "metrics row count must remain the same after resubmitting an identical envelope (I-3)")
}
