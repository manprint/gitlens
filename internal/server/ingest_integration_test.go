//go:build integration

package server

import (
	"bytes"
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
	router := NewRouter(auth, inv, pipeline, api, topoAPI, ashAPI)

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
