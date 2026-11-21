//go:build integration

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
)

// TestINTMCP001: over real HTTP against the real store, an MCP client session
// (initialize, initialized, tools/list, two tools/call) returns the instance
// and the same top list as the REST call.
func TestINTMCP001_SessionOverRealStore(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	ctx := context.Background()

	instID := uuid.New()
	cid := pgtype.ClusterID(7301)
	_, err := NewInventory(pool).Upsert(ctx, wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{{
			InstanceID: instID.String(), ClusterID: cid.String(), ClusterIDSource: string(pgtype.IDSourceManual),
			Addr: "10.0.0.31", Port: 5432, Role: "primary", PGVersion: 180000, PermTier: "T0",
		}},
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	for i, q := range []int64{11, 12} {
		_, err := pool.Exec(ctx, `INSERT INTO metrics_ash (ts, cluster_id, instance_id, datname, wait_event_type, wait_event, state, queryid, samples, window_seconds, window_ticks)
			VALUES ($1, $2, $3, 'app', 'IO', 'DataFileRead', 'active', $4, $5, 10, 10)`, now.Add(-time.Duration(i+1)*time.Minute), store.ToDB(cid), instID, q, 3+i)
		require.NoError(t, err)
	}

	const token = "int-mcp-token"
	cfg := UIConfig{MCP: MCPConfig{Enabled: true, Version: "int"}}
	router := NewRouter(cfg, nil, nil, NewAuth(token), NewInventory(pool), NewPipeline(pool, nil),
		NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool), NewAlertAPI(pool, nil))
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	send := func(body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/mcp", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		out, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, out
	}
	type callResult struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}

	status, body := send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"it","version":"1"}}}`)
	require.Equal(t, http.StatusOK, status, string(body))
	require.Contains(t, string(body), `"protocolVersion":"2025-06-18"`)

	status, body = send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Equal(t, http.StatusAccepted, status)
	require.Empty(t, body)

	status, body = send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, status)
	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Result.Tools, len(mcpToolSpecs))

	status, body = send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pglens_list_instances","arguments":{}}}`)
	require.Equal(t, http.StatusOK, status)
	var instances callResult
	require.NoError(t, json.Unmarshal(body, &instances))
	require.False(t, instances.Result.IsError, string(body))
	require.Contains(t, instances.Result.Content[0].Text, instID.String())

	status, body = send(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"pglens_activity_top","arguments":{"instance_id":"` + instID.String() + `","limit":5}}}`)
	require.Equal(t, http.StatusOK, status)
	var top callResult
	require.NoError(t, json.Unmarshal(body, &top))
	require.False(t, top.Result.IsError, string(body))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/ash/top?instance_id="+instID.String()+"&limit=5", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	rest, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var viaMCP, viaREST ashTopResponse
	require.NoError(t, json.Unmarshal([]byte(top.Result.Content[0].Text), &viaMCP))
	require.NoError(t, json.Unmarshal(rest, &viaREST))
	require.Len(t, viaREST.Entries, 2)
	require.Equal(t, viaREST.Entries, viaMCP.Entries)
}
