//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/store"
)

func authed(t *testing.T, router http.Handler, method, path, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// answerClaim claims the agent's next command and posts result as its answer.
func answerClaim(t *testing.T, router http.Handler, agentID, want uuid.UUID, outcome, result, errText string) {
	t.Helper()
	rec := authed(t, router, http.MethodGet, "/api/v1/agents/"+agentID.String()+"/commands?wait=0s", "", commandAgentIDHeader, agentID.String())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var claimed pollResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claimed))
	require.Equal(t, want, claimed.CommandID)
	body, err := json.Marshal(resultRequest{ClaimToken: claimed.ClaimToken, Outcome: outcome, Result: json.RawMessage(result), Error: errText})
	require.NoError(t, err)
	rec = authed(t, router, http.MethodPost, "/api/v1/commands/"+claimed.CommandID.String()+"/result", string(body))
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// TestINTWBK004: a workbook lives through create, variant, run and the
// agent's answer; the variant reads back done with its cost, the plan stays
// off the plan history, and deleting the workbook removes its variants.
func TestINTWBK004_WorkbookRunsThroughTheCommandQueue(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	instanceID, agentID := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workbooks WHERE instance_id=$1`, instanceID)
	})

	rec := authed(t, router, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+instanceID.String()+`","database":"app","queryid":"-4242","name":"lookup"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var book workbookJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &book))
	require.Len(t, book.Variants, 1)
	require.Equal(t, store.BaselineVariantName, book.Variants[0].Name)
	wbPath := "/api/v1/workbooks/" + book.WorkbookID.String()

	rec = authed(t, router, http.MethodPost, wbPath+"/variants", `{"name":"no index","settings":{"enable_indexscan":"off"},"params":["42"],"analyze":true}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var variant variantJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &variant))
	require.Equal(t, 1, variant.Position)
	vPath := wbPath + "/variants/" + variant.VariantID.String()

	rec = authed(t, router, http.MethodPost, vPath+"/run", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	var run struct {
		CommandID uuid.UUID `json:"command_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	require.Equal(t, http.StatusConflict, authed(t, router, http.MethodPost, vPath+"/run", "").Code)

	var args []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT args FROM commands WHERE command_id=$1`, run.CommandID).Scan(&args))
	require.JSONEq(t, `{"queryid":-4242,"datname":"app","analyze":true,"params":["42"],"settings":{"enable_indexscan":"off"}}`, string(args))

	answerClaim(t, router, agentID, run.CommandID, "ok", `{"plan":[{"Plan":{"Node Type":"Seq Scan","Total Cost":1693.5},"Planning Time":0.2,"Execution Time":3.1}],"plan_hash":"wbk-hash","datname":"app","settings":{"enable_indexscan":"off"}}`, "")

	rec = authed(t, router, http.MethodGet, wbPath, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &book))
	done := book.Variants[1]
	require.Equal(t, "done", done.Status)
	require.InDelta(t, 1693.5, *done.TotalCost, 1e-9)
	require.InDelta(t, 3.1, *done.ExecutionMS, 1e-9)
	require.Equal(t, "wbk-hash", *done.PlanHash)
	require.NotNil(t, done.RanAt)
	require.NotEmpty(t, done.Plan)
	var plans int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM query_plans WHERE instance_id=$1 AND queryid=-4242`, instanceID).Scan(&plans))
	require.Zero(t, plans, "a workbook variant's plan stays off the plan history")

	// A refused run leaves the variant failed with the agent's error.
	basePath := wbPath + "/variants/" + book.Variants[0].VariantID.String()
	rec = authed(t, router, http.MethodPost, basePath+"/run", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	answerClaim(t, router, agentID, run.CommandID, "rejected", "", "statement not found")
	got, err := store.GetVariant(ctx, pool, commandTenant, book.WorkbookID, book.Variants[0].VariantID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, "statement not found", got.Error)

	// Editing resets status and result.
	rec = authed(t, router, http.MethodPut, vPath, `{"name":"no index","settings":{"enable_indexscan":"off"},"analyze":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &variant))
	require.Equal(t, "never", variant.Status)
	require.Nil(t, variant.TotalCost)
	require.Nil(t, variant.CommandID)

	// The list leaves plans out.
	rec = authed(t, router, http.MethodGet, "/api/v1/workbooks?instance_id="+instanceID.String()+"&queryid=-4242", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), `"plan":`)

	// The variant limit holds in SQL.
	for i := len(book.Variants); i < store.MaxVariantsPerWorkbook; i++ {
		require.Equal(t, http.StatusCreated, authed(t, router, http.MethodPost, wbPath+"/variants", `{"name":"v"}`).Code)
	}
	rec = authed(t, router, http.MethodPost, wbPath+"/variants", `{"name":"v21"}`)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "limit_reached")

	require.Equal(t, http.StatusNotFound, authed(t, router, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+uuid.NewString()+`","database":"app","queryid":"1","name":"x"}`).Code)

	require.Equal(t, http.StatusNoContent, authed(t, router, http.MethodDelete, wbPath, "").Code)
	var left int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM workbook_variants WHERE workbook_id=$1`, book.WorkbookID).Scan(&left))
	require.Zero(t, left)
	require.Equal(t, http.StatusNotFound, authed(t, router, http.MethodGet, wbPath, "").Code)
}

// TestINTWBK004_ExpiryFailsThePendingVariant: a command that expires before
// the agent answers leaves its variant failed.
func TestINTWBK004_ExpiryFailsThePendingVariant(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	instanceID, _ := seedCommandInstance(t, pool)
	router := commandsIntegrationRouter(pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workbooks WHERE instance_id=$1`, instanceID)
	})
	rec := authed(t, router, http.MethodPost, "/api/v1/workbooks", `{"instance_id":"`+instanceID.String()+`","database":"app","queryid":"9","name":"expiry"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var book workbookJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &book))
	vid := book.Variants[0].VariantID
	rec = authed(t, router, http.MethodPost, "/api/v1/workbooks/"+book.WorkbookID.String()+"/variants/"+vid.String()+"/run", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	_, err := pool.Exec(ctx, `UPDATE commands SET expires_at = now() - interval '1 second' WHERE instance_id=$1`, instanceID)
	require.NoError(t, err)
	require.NoError(t, expireCommands(ctx, pool))
	got, err := store.GetVariant(ctx, pool, commandTenant, book.WorkbookID, vid)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.NotEmpty(t, got.Error)
}
