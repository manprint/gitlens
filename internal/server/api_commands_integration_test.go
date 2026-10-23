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
	for _, index := range []string{"commands_queue_idx", "commands_instance_idx", "commands_expiry_idx", "command_audit_instance_idx", "query_plans_dedup_idx", "query_plans_lookup_idx"} {
		var exists bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1)`, index).Scan(&exists))
		require.True(t, exists, index)
	}
}

// TestINTCMD016_ExpirySweepCanUseAnIndex: the sweep runs on every claim and
// every GET, and filters on tenant, state and expiry. commands_queue_idx covers
// only pending rows, so without an index on the sweep the whole table was read
// on each run. With sequential scans off, the planner must be able to use the
// index for the sweep's predicate.
func TestINTCMD016_ExpirySweepCanUseAnIndex(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`)
	require.NoError(t, err)
	rows, err := tx.Query(ctx, `EXPLAIN SELECT command_id FROM commands WHERE tenant_id=$1 AND state IN ('pending','claimed') AND expires_at <= now()`, commandTenant)
	require.NoError(t, err)
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan.WriteString(line + "\n")
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan.String(), "commands_expiry_idx", plan.String())
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
	completeCommand(t, router, instanceID, agentID, `{"kind":"pgstattuple","args":{"datname":"postgres","schema":"public","relation":"items"}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"datname":"postgres","table_len":100,"tuple_len":55,"dead_tuple_len":15,"free_space":25,"free_percent":25}}`)
	var method, objectKind, datname string
	var bloat, realBytes, expected int64
	var ratio float64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT method, object_kind, datname, real_bytes, expected_bytes, bloat_bytes, bloat_ratio FROM metrics_bloat WHERE instance_id=$1`, instanceID).Scan(&method, &objectKind, &datname, &realBytes, &expected, &bloat, &ratio))
	require.Equal(t, "pgstattuple", method)
	require.Equal(t, "table", objectKind)
	require.Equal(t, "postgres", datname)
	require.Equal(t, int64(100), realBytes)
	require.Equal(t, int64(40), bloat, "dead tuples plus free space")
	require.Equal(t, int64(60), expected, "real minus bloat, as for the estimate")
	require.InDelta(t, 0.4, ratio, 1e-9, "bloat over real size, not free space alone")
}

// TestINTCMD013_PgstattupleWithoutDatnameUsesTheScannedDatabase: a command
// that names no database runs in the target's own, and its measurement was
// filed under an empty database name.
func TestINTCMD013_PgstattupleWithoutDatnameUsesTheScannedDatabase(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	completeCommand(t, router, instanceID, agentID, `{"kind":"pgstattuple","args":{"schema":"public","relation":"items"}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"datname":"appdb","table_len":8192,"dead_tuple_len":0,"free_space":0}}`)
	var datname string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT datname FROM metrics_bloat WHERE instance_id=$1`, instanceID).Scan(&datname))
	require.Equal(t, "appdb", datname)
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

// TestINTCMD011_AuditRecordsRequestsAndTerminalOutcomes: one row per request
// (with the credential that made it) and one per terminal outcome.
func TestINTCMD011_AuditRecordsRequestsAndTerminalOutcomes(t *testing.T) {
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
	require.Equal(t, map[string]int{"requested": 3, "ok": 1, "error": 1, "rejected": 2}, counts)
	var requesters []string
	requesterRows, err := pool.Query(context.Background(), `SELECT DISTINCT requested_by FROM command_audit WHERE instance_id=$1 AND outcome='requested'`, instanceID)
	require.NoError(t, err)
	defer requesterRows.Close()
	for requesterRows.Next() {
		var who string
		require.NoError(t, requesterRows.Scan(&who))
		requesters = append(requesters, who)
	}
	require.NoError(t, requesterRows.Err())
	require.Equal(t, []string{"api-token from 192.0.2.1"}, requesters)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instanceID.String()+"/command-audit?limit=500", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var audit []auditItem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &audit))
	require.Len(t, audit, 7)
}

// TestINTCMD012_ReadingAnExpiredCommandExpiresIt: a command whose agent
// never polled read as pending forever, and left no audit row.
func TestINTCMD012_ReadingAnExpiredCommandExpiresIt(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, _ := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	ctx := context.Background()
	commandID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at, requested_by)
		SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'terminate', '{"pid":7}', 'pending', now()-interval '1 second', 'ui-session 0badcafe from 10.0.0.9' FROM instances WHERE instance_id=$2`, commandID, instanceID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/commands/"+commandID.String(), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got commandResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "expired", got.State)

	var outcome, who, detail string
	require.NoError(t, pool.QueryRow(ctx, `SELECT outcome, requested_by, detail FROM command_audit WHERE command_id=$1`, commandID).Scan(&outcome, &who, &detail))
	require.Equal(t, "expired", outcome)
	require.Equal(t, "ui-session 0badcafe from 10.0.0.9", who)
	require.Equal(t, "expired before the agent claimed it", detail)
}

// TestINTPLAN003_PlanWithoutDatnameIsFiledUnderTheDatabaseItRanIn: a command
// that names no database explains in the target's own. The plan is filed under
// the database the agent reports, not under an empty name.
func TestINTPLAN003_PlanWithoutDatnameIsFiledUnderTheDatabaseItRanIn(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	completeCommand(t, router, instanceID, agentID, `{"kind":"explain","args":{"queryid":9}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"plan":[{"Plan":{"Node Type":"Seq Scan"}}],"plan_hash":"named-by-agent","datname":"appdb"}}`)
	var datname string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT datname FROM query_plans WHERE instance_id=$1 AND queryid=9`, instanceID).Scan(&datname))
	require.Equal(t, "appdb", datname)
}

// TestINTPLAN004_PlanWithNoDatnameIsStoredUnderNoName: a result that names no
// database, from an agent that predates the name, is still stored, under an
// empty name, so the plan is not lost in the upgrade window.
func TestINTPLAN004_PlanWithNoDatnameIsStoredUnderNoName(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	completeCommand(t, router, instanceID, agentID, `{"kind":"explain","args":{"queryid":10}}`, `{"claim_token":"00000000-0000-0000-0000-000000000000","outcome":"ok","result":{"plan":[{"Plan":{"Node Type":"Seq Scan"}}],"plan_hash":"unnamed"}}`)
	var count int
	var datname string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*), COALESCE(max(datname), '') FROM query_plans WHERE instance_id=$1 AND queryid=10`, instanceID).Scan(&count, &datname))
	require.Equal(t, 1, count)
	require.Empty(t, datname)
}

// INT-CMD-030: an agent's queue is bounded. A command over the limit is refused
// with 429 and not queued, and a command that leaves the queue frees a place.
func TestINTCMD030_EnqueueRefusesBeyondTheAgentQueueLimit(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT gen_random_uuid(), tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":42}', 'pending', now()+interval '5 minutes' FROM instances, generate_series(1, $2) WHERE instance_id=$1`, instanceID, commandQueueLimit)
	require.NoError(t, err)

	router := commandsIntegrationRouter(pool)
	enqueue := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+instanceID.String()+"/commands", bytes.NewBufferString(`{"kind":"explain","args":{"queryid":11}}`))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	rec := enqueue()
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	var pending int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE agent_id=$1 AND state='pending'`, agentID.String()).Scan(&pending))
	require.Equal(t, commandQueueLimit, pending, "the refused command is not queued")

	_, err = pool.Exec(ctx, `UPDATE commands SET state='done', finished_at=now() WHERE command_id = (SELECT command_id FROM commands WHERE agent_id=$1 AND state='pending' LIMIT 1)`, agentID.String())
	require.NoError(t, err)
	rec = enqueue()
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}
