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
	"time"

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

	var outcomes string
	require.NoError(t, pool.QueryRow(ctx, `SELECT string_agg(outcome, ',' ORDER BY audit_id) FROM command_audit WHERE command_id=$1`, commandID).Scan(&outcomes))
	require.Equal(t, "expired,rejected", outcomes, "the expiry is audited, then the late result")
}

// TestINTCMD015_ClaimedCommandKeepsItsLease: the enqueue TTL bounds how long a
// command waits to be claimed. The agent that claimed it runs it next, so the
// TTL must not expire the claim while the agent works. A result that arrives
// after the lease ran out is still refused, but the audit records what the
// agent reported, not a rejection it never received.
func TestINTCMD015_ClaimedCommandKeepsItsLease(t *testing.T) {
	pool := getSharedPool(t)
	instanceID, agentID := seedCommandInstance(t, pool)
	ctx := context.Background()
	commandID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO commands (command_id, tenant_id, agent_id, instance_id, cluster_id, kind, args, state, expires_at) SELECT $1, tenant_id, agent_id, instance_id, cluster_id, 'cancel', '{"pid":42}', 'pending', now()+interval '2 seconds' FROM instances WHERE instance_id=$2`, commandID, instanceID)
	require.NoError(t, err)

	router := commandsIntegrationRouter(pool)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/agents/%s/commands?wait=0s", agentID), nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set(commandAgentIDHeader, agentID.String())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var claimed pollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	require.Equal(t, commandID, claimed.CommandID)

	var leased bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at > now() + interval '1 minute' FROM commands WHERE command_id=$1`, commandID).Scan(&leased))
	require.True(t, leased, "a claim outlasts the enqueue TTL, which expires the command before it is claimed")

	// The TTL passes while the agent is still running the command.
	time.Sleep(2500 * time.Millisecond)
	readCommand := func() string {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/commands/"+commandID.String(), nil)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got commandResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		return got.State
	}
	require.Equal(t, "claimed", readCommand())

	// The lease runs out before the agent reports: the sweep expires the claim.
	_, err = pool.Exec(ctx, `UPDATE commands SET expires_at = now() - interval '1 second' WHERE command_id=$1`, commandID)
	require.NoError(t, err)
	require.Equal(t, "expired", readCommand())

	payload := fmt.Sprintf(`{"claim_token":"%s","outcome":"ok","result":{"signalled":true}}`, claimed.ClaimToken)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/commands/"+commandID.String()+"/result", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var state string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM commands WHERE command_id=$1`, commandID).Scan(&state))
	require.Equal(t, "expired", state, "a late result is still refused")
	var outcomes string
	require.NoError(t, pool.QueryRow(ctx, `SELECT string_agg(outcome, ',' ORDER BY audit_id) FROM command_audit WHERE command_id=$1`, commandID).Scan(&outcomes))
	require.Equal(t, "expired,ok", outcomes, "the agent ran the command, so the late result is audited as the agent reported it")
}
