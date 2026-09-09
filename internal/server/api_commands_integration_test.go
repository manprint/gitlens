//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
)

func commandsIntegrationRouter(pool *pgxpool.Pool) http.Handler {
	return NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, clock.NewFake(time.Now())), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))
}

func TestINTCMD001_MigrationCreatesCommandTablesAndIndexes(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	for _, table := range []string{"commands", "command_audit", "query_plans"} {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, table).Scan(&exists))
		require.True(t, exists, table)
	}
	for _, index := range []string{"commands_queue_idx", "commands_instance_idx", "command_audit_instance_idx", "query_plans_dedup_idx", "query_plans_lookup_idx"} {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1)`, index).Scan(&exists))
		require.True(t, exists, index)
	}
}

func seedCommandInstance(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	instanceID, agentID := uuid.New(), uuid.New()
	clusterID := store.ToDB(pgtype.ClusterID(uint64(time.Now().UnixNano())))
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, clusterID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'127.0.0.1',5432,170000,'primary','T1',now())`, instanceID, clusterID, agentID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM command_audit WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM query_plans WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM metrics_bloat WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM commands WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM instances WHERE instance_id=$1`, instanceID)
		_, _ = pool.Exec(ctx, `DELETE FROM clusters WHERE tenant_id='default' AND cluster_id=$1`, clusterID)
		_, _ = pool.Exec(ctx, `DELETE FROM agents WHERE agent_id=$1`, agentID)
	})
	return instanceID, agentID
}

func completeCommand(t *testing.T, router http.Handler, instanceID, agentID uuid.UUID, body, result string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+instanceID.String()+"/commands", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var enqueued struct {
		CommandID uuid.UUID `json:"command_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &enqueued))

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID.String()+"/commands?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var claimed pollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	require.Equal(t, enqueued.CommandID, claimed.CommandID)

	result = strings.ReplaceAll(result, "00000000-0000-0000-0000-000000000000", claimed.ClaimToken.String())
	req = httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+claimed.CommandID.String()+"/result", bytes.NewBufferString(result))
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

func TestINTCMD002_EnqueuePollResultAndReadBack(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)

	body := bytes.NewBufferString(`{"kind":"explain","args":{"queryid":7}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+instanceID.String()+"/commands", body)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	var enqueued struct {
		CommandID uuid.UUID `json:"command_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &enqueued))

	req = httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID.String()+"/commands?wait=0s", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var claimed pollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	require.Equal(t, enqueued.CommandID, claimed.CommandID)

	result, err := json.Marshal(resultRequest{ClaimToken: claimed.ClaimToken, Outcome: "ok", Result: json.RawMessage(`{"Plan":"Seq Scan"}`)})
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+claimed.CommandID.String()+"/result", bytes.NewReader(result))
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/commands/"+claimed.CommandID.String(), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var got commandResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "done", got.State)
	require.JSONEq(t, `{"Plan":"Seq Scan"}`, string(got.Result))
}

func TestINTCMD003_ForeignAgentCannotPollQueue(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, ownerID := seedCommandInstance(t, pool)
	foreignID := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":42}', 'pending', now()+interval '5 minutes' FROM instances WHERE instance_id=$2`, uuid.New(), instanceID)
	require.NoError(t, err)
	router := commandsIntegrationRouter(pool)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/agents/%s/commands?wait=0s", ownerID), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set(commandAgentIDHeader, foreignID.String())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	var state string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT state FROM commands WHERE instance_id=$1 ORDER BY created_at DESC LIMIT 1`, instanceID).Scan(&state))
	require.Equal(t, "pending", state)
}

func TestINTCMD009_PgstattupleResultPersistsBloat(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	completeCommand(t, router, instanceID, agentID, `{"kind":"pgstattuple","args":{"datname":"postgres","schema":"public","relation":"items"}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"table_len":100,"tuple_len":60,"free_percent":40}}`)
	var method, objectKind string
	var bloat, realBytes int64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT method, object_kind, real_bytes, bloat_bytes FROM metrics_bloat WHERE instance_id=$1`, instanceID).Scan(&method, &objectKind, &realBytes, &bloat))
	require.Equal(t, "pgstattuple", method)
	require.Equal(t, "table", objectKind)
	require.Equal(t, int64(100), realBytes)
	require.Equal(t, int64(40), bloat)
}

func TestINTPLAN001_SamePlanIsDeduplicated(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	result := `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"plan":[{"Plan":{"Node Type":"Seq Scan"}}],"plan_hash":"same-shape"}}`
	completeCommand(t, router, instanceID, agentID, `{"kind":"explain","args":{"queryid":7,"datname":"postgres"}}`, result)
	completeCommand(t, router, instanceID, agentID, `{"kind":"explain","args":{"queryid":7,"datname":"postgres"}}`, result)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM query_plans WHERE instance_id=$1 AND queryid=7`, instanceID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestINTPLAN002_DifferentPlanCreatesHistoryShape(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	for _, item := range []struct{ node, hash string }{{"Seq Scan", "seq-shape"}, {"Index Scan", "index-shape"}} {
		result := fmt.Sprintf(`{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"plan":[{"Plan":{"Node Type":"%s"}}],"plan_hash":"%s"}}`, item.node, item.hash)
		completeCommand(t, router, instanceID, agentID, `{"kind":"explain","args":{"queryid":8,"datname":"postgres"}}`, result)
	}
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM query_plans WHERE instance_id=$1 AND queryid=8`, instanceID).Scan(&count))
	require.Equal(t, 2, count)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans?instance_id="+instanceID.String()+"&queryid=8", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response planHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 2, response.TotalShapes)
	require.True(t, response.Plans[0].Changed)
	require.False(t, response.Plans[1].Changed)
}

func TestINTCMD011_AuditHasOneRowPerTerminalOutcome(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	completeCommand(t, router, instanceID, agentID, `{"kind":"cancel","args":{"pid":42}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"signalled":false}}`)
	completeCommand(t, router, instanceID, agentID, `{"kind":"terminate","args":{"pid":43}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"error","error":"backend disappeared"}`)
	completeCommand(t, router, instanceID, agentID, `{"kind":"cancel","args":{"pid":44}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"rejected","error":"not a client backend"}`)

	expiredID := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":45}', 'expired', now()-interval '1 second' FROM instances WHERE instance_id=$2`, expiredID, instanceID)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+expiredID.String()+"/result", bytes.NewBufferString(`{"claim_token":"11111111-1111-1111-1111-111111111111","outcome":"error","error":"expired"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	rows, err := pool.Query(context.Background(), `SELECT outcome FROM command_audit WHERE instance_id=$1 ORDER BY audit_id`, instanceID)
	require.NoError(t, err)
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var outcome string
		require.NoError(t, rows.Scan(&outcome))
		counts[outcome]++
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]int{"ok": 1, "error": 1, "rejected": 2}, counts)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/command-audit?limit=500", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var audit []auditItem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &audit))
	require.Len(t, audit, 4)
}
