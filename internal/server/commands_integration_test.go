//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestINTCMD004_AtomicClaim(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	ctx := context.Background()
	for range 5 {
		_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":42}', 'pending', now()+interval '5 minutes' FROM instances WHERE instance_id=$2`, uuid.New(), instanceID)
		require.NoError(t, err)
	}

	router := commandsIntegrationRouter(pool)
	type pollResult struct {
		code int
		body []byte
	}
	results := make(chan pollResult, 10)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/agents/%s/commands?wait=0s", agentID), nil)
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set(commandAgentIDHeader, agentID.String())
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			results <- pollResult{code: rec.Code, body: append([]byte(nil), rec.Body.Bytes()...)}
		}()
	}
	wg.Wait()
	close(results)

	claimed := make(map[uuid.UUID]struct{})
	for result := range results {
		switch result.code {
		case http.StatusOK:
			var response pollResponse
			require.NoError(t, json.Unmarshal(result.body, &response))
			require.NotEqual(t, uuid.Nil, response.ClaimToken)
			claimed[response.CommandID] = struct{}{}
		case http.StatusNoContent:
		default:
			t.Fatalf("unexpected poll status %d: %s", result.code, result.body)
		}
	}
	require.Len(t, claimed, 5)
	var claimedCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM commands WHERE instance_id=$1 AND state='claimed'`, instanceID).Scan(&claimedCount))
	require.Equal(t, 5, claimedCount)
}

func TestINTCMD005_ExpiryAndLateResult(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	ctx := context.Background()
	commandID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":42}', 'pending', now()-interval '1 minute' FROM instances WHERE instance_id=$2`, commandID, instanceID)
	require.NoError(t, err)

	router := commandsIntegrationRouter(pool)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/agents/%s/commands?wait=0s", agentID), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	var state string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM commands WHERE command_id=$1`, commandID).Scan(&state))
	require.Equal(t, "expired", state)

	payload, err := json.Marshal(resultRequest{ClaimToken: uuid.New(), Outcome: "error", Error: "agent expired"})
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)

	var outcome string
	var auditCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), max(outcome) FROM command_audit WHERE command_id=$1`, commandID).Scan(&auditCount, &outcome))
	require.Equal(t, 1, auditCount)
	require.Equal(t, "rejected", outcome)
}
