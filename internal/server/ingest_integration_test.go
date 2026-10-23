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
	// The read API is credential-gated like the ingest endpoint: an
	// unconfigured UI no longer leaves it anonymous.
	getReq.Header.Set("Authorization", "Bearer test-token")
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
	// in the instances count). The pglens_% exclusion skips the collector's
	// own self-monitoring gauges, which every push writes as well and which
	// are covered by their own test.
	var count1, metricsCount1 int
	ctx := context.Background()
	err = pool.QueryRow(ctx, "SELECT count(*) FROM instances").Scan(&count1)
	require.NoError(t, err)
	require.Equal(t, 1, count1, "should have 1 instance after first push")
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics WHERE metric NOT LIKE 'pglens_%'").Scan(&metricsCount1)
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
	err = pool.QueryRow(ctx, "SELECT count(*) FROM metrics WHERE metric NOT LIKE 'pglens_%'").Scan(&metricsCount2)
	require.NoError(t, err)
	require.Equal(t, metricsCount1, metricsCount2, "metrics row count must remain the same after resubmitting an identical envelope (I-3)")
}

// INT-INGEST-003: once the agent that owns an instance is revoked, a push for
// that instance is refused with 401 whatever agent_id it carries (none, or
// another agent's), while an instance owned by an agent that is not revoked
// is still accepted.
func TestIngest_INT_INGEST_003_RevokedOwnerRefusesPush(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	auth := NewAuth("test-token")
	inv := NewInventory(pool)
	pipeline := NewPipeline(pool, nil)
	api := NewAPI(pool)
	topoAPI := NewTopologyAPI(pool)
	ashAPI := NewAshAPI(pool)
	router := NewRouter(UIConfig{}, nil, nil, auth, inv, pipeline, api, topoAPI, ashAPI)

	push := func(agentID, instanceID string) *httptest.ResponseRecorder {
		env := wire.Envelope{
			ProtocolVersion: wire.ProtocolVersion,
			AgentID:         agentID,
			SentAt:          time.Now(),
			Instances: []wire.Instance{{
				InstanceID: instanceID,
				ClusterID:  pgtype.ClusterID(444555666).String(),
				Role:       "primary",
			}},
		}
		body, err := json.Marshal(env)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	owner := uuid.NewString()
	instID := uuid.NewString()
	require.Equal(t, http.StatusAccepted, push(owner, instID).Code)
	_, err := pool.Exec(context.Background(), `UPDATE agents SET revoked_at = now() WHERE agent_id = $1`, owner)
	require.NoError(t, err)

	for name, agentID := range map[string]string{"omitted": "", "other agent": uuid.NewString()} {
		rec := push(agentID, instID)
		require.Equal(t, http.StatusUnauthorized, rec.Code, name)
		require.Contains(t, rec.Body.String(), "revoked", name)
	}

	require.Equal(t, http.StatusAccepted, push(uuid.NewString(), uuid.NewString()).Code, "an unrevoked owner is not refused")
}

// TestIngest_StoreRefusalIsNotRetried: an envelope the store refuses on a
// CHECK constraint fails every time it is sent, so the answer is a 400 that
// the agent consumes. A 500 made the agent resend it forever, and every
// envelope behind it waited.
func TestIngest_StoreRefusalIsNotRetried(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	auth := NewAuth("test-token")
	h := IngestHandler(auth, NewInventory(pool), NewPipeline(pool, nil))

	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          time.Now(),
		Instances: []wire.Instance{{
			InstanceID: uuid.NewString(),
			ClusterID:  pgtype.ClusterID(111222444).String(),
			Role:       "primary",
			Results: []wire.Result{{
				Check: "bloat_estimate", TS: time.Now(), Database: "app",
				Metrics: []wire.Metric{{
					Name: "pg_bloat_real_bytes", Value: 1, Kind: "gauge",
					// A kind of object the table does not admit, as a newer agent may send.
					Labels: map[string]string{"schemaname": "public", "relname": "s", "object_kind": "sequence", "method": "estimate"},
				}},
			}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// INT-INGEST-004: an agent that predates the agent_id field sends its envelopes
// without one. Its known instances are stored, under the owner recorded for
// them, and a new instance is refused alone. Once that owner is revoked, the
// same envelope is refused as revoked, so the owner check still applies to it.
func TestIngest_INT_INGEST_004_LegacyEnvelopeWithoutAgentID(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	auth := NewAuth("test-token")
	h := IngestHandler(auth, NewInventory(pool), NewPipeline(pool, nil))
	owner := uuid.NewString()
	push := func(version int, instances ...wire.Instance) *httptest.ResponseRecorder {
		env := wire.Envelope{ProtocolVersion: version, SentAt: time.Now(), Instances: instances}
		if version == wire.ProtocolVersion {
			env.AgentID = owner
		}
		body, err := json.Marshal(env)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	known, fresh := uuid.NewString(), uuid.NewString()
	instance := func(id string) wire.Instance {
		return wire.Instance{InstanceID: id, ClusterID: pgtype.ClusterID(555666777).String(), Role: "primary"}
	}
	require.Equal(t, http.StatusAccepted, push(wire.ProtocolVersion, instance(known)).Code)

	rec := push(agentIDRequiredFrom-1, instance(known), instance(fresh))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"accepted":1,"rejected":1}`, rec.Body.String())
	var recorded uuid.UUID
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT agent_id FROM instances WHERE instance_id=$1`, known).Scan(&recorded))
	require.Equal(t, uuid.MustParse(owner), recorded, "a known instance keeps its owner")
	var freshRows int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM instances WHERE instance_id=$1`, fresh).Scan(&freshRows))
	require.Zero(t, freshRows, "a new instance without an owner is not stored")

	_, err := pool.Exec(context.Background(), `UPDATE agents SET revoked_at = now() WHERE agent_id = $1`, owner)
	require.NoError(t, err)
	rec = push(agentIDRequiredFrom-1, instance(known))
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "revoked")
}
