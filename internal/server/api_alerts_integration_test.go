//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
)

func TestAlertAPI_INT_ALERTAPI_001_SilenceLifecycle(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	r := alertRouter(NewAlertAPI(pool, alert.NewPgStore(pool)))
	now := time.Now().UTC()
	body := fmt.Sprintf(`{"matchers":[{"name":"rule_id","value":"agent_down"}],"reason":"maintenance","starts_at":%q,"ends_at":%q}`,
		now.Add(-time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/silences", strings.NewReader(body)))
	require.Equal(t, http.StatusCreated, w.Code)
	var created silenceJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/silences", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), created.ID.String())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/silences/"+created.ID.String(), nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	var ended time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT ends_at FROM silences WHERE silence_id=$1`, created.ID).Scan(&ended))
	require.WithinDuration(t, time.Now(), ended, 5*time.Second)
}

func TestAlertAPI_INT_ALERTAPI_002_Tier0Immutable(t *testing.T) {
	pool := getSharedPool(t)
	r := alertRouter(NewAlertAPI(pool, alert.NewPgStore(pool)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/agent_down", strings.NewReader(`{"enabled":false}`)))
	require.Equal(t, http.StatusConflict, w.Code)
}

// TestAlertAPI_INT_ALERTAPI_003_DetailFindsSlashedKey: the detail route must
// return a stored alert whose key holds '/' separators, the way the UI asks for it.
func TestAlertAPI_INT_ALERTAPI_003_DetailFindsSlashedKey(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	st := alert.NewPgStore(pool)
	now := time.Now().UTC()
	key := "agent_down/11111111-1111-1111-1111-111111111111/"
	require.NoError(t, st.Upsert(contextBackground(), alert.Alert{Key: key, RuleID: "agent_down", Severity: alert.SeverityCritical, State: alert.StateFiring, Summary: "agent silent", StartedAt: now, LastEvalAt: now, Labels: map[string]string{}}))
	r := alertRouter(NewAlertAPI(pool, st))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+encodeURIComponent(key), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"alert_key":"`+key+`"`)
}

// TestAlertAPI_INT_ALERTAPI_004_StoredNullLabelsRenderAsObject: a row stored
// with a nil label map holds JSON null, and the API must still answer labels
// as an object and resolved_at as null.
func TestAlertAPI_INT_ALERTAPI_004_StoredNullLabelsRenderAsObject(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	st := alert.NewPgStore(pool)
	now := time.Now().UTC()
	key := "agent_down/22222222-2222-2222-2222-222222222222/"
	require.NoError(t, st.Upsert(contextBackground(), alert.Alert{Key: key, RuleID: "agent_down", Severity: alert.SeverityCritical, State: alert.StateFiring, Summary: "agent silent", StartedAt: now, LastEvalAt: now}))
	r := alertRouter(NewAlertAPI(pool, st))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+encodeURIComponent(key), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, map[string]any{}, got["labels"])
	require.Contains(t, got, "resolved_at")
	require.Nil(t, got["resolved_at"])
}
