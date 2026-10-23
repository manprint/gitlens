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

	"github.com/google/uuid"
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/silences", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
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
	req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/agent_down", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
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

// TestAlertAPI_INT_ALERTAPI_005_DefaultListsActiveAlerts: with no state the
// list must answer the active alerts from the real store, not an empty set.
// A stored resolved alert stays out of the default.
func TestAlertAPI_INT_ALERTAPI_005_DefaultListsActiveAlerts(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	// truncateAll leaves the alerts table alone, so earlier tests' rows must go.
	_, err := pool.Exec(contextBackground(), `DELETE FROM alerts`)
	require.NoError(t, err)
	st := alert.NewPgStore(pool)
	ctx := contextBackground()
	now := time.Now().UTC()
	resolvedAt := now
	for _, a := range []alert.Alert{
		{Key: "replica_lag/firing/", RuleID: "replica_lag", Severity: alert.SeverityWarning, State: alert.StateFiring, Summary: "lag", StartedAt: now, LastEvalAt: now, Labels: map[string]string{}},
		{Key: "replica_lag/resolved/", RuleID: "replica_lag", Severity: alert.SeverityWarning, State: alert.StateResolved, Summary: "lag", StartedAt: now, LastEvalAt: now, ResolvedAt: &resolvedAt, Labels: map[string]string{}},
	} {
		require.NoError(t, st.Upsert(ctx, a))
	}
	r := alertRouter(NewAlertAPI(pool, st))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []alertJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1, w.Body.String())
	require.Equal(t, "replica_lag/firing/", got[0].Key)
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

// TestAlertAPI_INT_ALERTAPI_006_RuleEditDoesNotRevertAnother: an edit to one
// field must not write back the value of another field that a concurrent edit
// committed while the request waited on the row lock.
func TestAlertAPI_INT_ALERTAPI_006_RuleEditDoesNotRevertAnother(t *testing.T) {
	pool := getSharedPool(t)
	ctx := contextBackground()
	const rule = "replica.lag_high"
	var origEnabled bool
	var origThreshold float64
	require.NoError(t, pool.QueryRow(ctx, `SELECT enabled, threshold FROM alert_rules WHERE rule_id=$1`, rule).Scan(&origEnabled, &origThreshold))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE alert_rules SET enabled=$1, threshold=$2 WHERE rule_id=$3`, origEnabled, origThreshold, rule)
	})

	// The first edit holds the row lock until it commits, and disables the rule.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE alert_rules SET enabled=false WHERE rule_id=$1`, rule)
	require.NoError(t, err)

	// The second edit changes only the threshold, and must wait for the first.
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		r := alertRouter(NewAlertAPI(pool, alert.NewPgStore(pool)))
		req := httptest.NewRequest(http.MethodPut, "/api/v1/alert-rules/"+rule, strings.NewReader(`{"threshold":45}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		done <- w
	}()
	require.Eventually(t, func() bool {
		var waiting int
		err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'UPDATE alert_rules%'`).Scan(&waiting)
		return err == nil && waiting > 0
	}, 10*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit(ctx))

	w := <-done
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"enabled":false`)
	var enabled bool
	var threshold float64
	require.NoError(t, pool.QueryRow(ctx, `SELECT enabled, threshold FROM alert_rules WHERE rule_id=$1`, rule).Scan(&enabled, &threshold))
	require.False(t, enabled, "the first edit's enabled=false must survive the second edit")
	require.Equal(t, 45.0, threshold)
}

// TestAlertAPI_INT_ALERTAPI_007_SilenceIDAndDeleteOrder: a silence id that is
// already taken is a conflict, not a server error. Deleting a silence that has
// not started ends it at its own start, so the row stays well ordered and
// inactive. Deleting an expired silence keeps the end it had.
func TestAlertAPI_INT_ALERTAPI_007_SilenceIDAndDeleteOrder(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := contextBackground()
	r := alertRouter(NewAlertAPI(pool, alert.NewPgStore(pool)))
	now := time.Now().UTC()
	id := uuid.NewString()
	body := fmt.Sprintf(`{"silence_id":%q,"matchers":[{"name":"rule_id","value":"agent_down"}],"reason":"maintenance","starts_at":%q,"ends_at":%q}`,
		id, now.Add(time.Hour).Format(time.RFC3339), now.Add(2*time.Hour).Format(time.RFC3339))
	post := func() int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/silences", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusCreated, post())
	require.Equal(t, http.StatusConflict, post(), "the same silence id again is a conflict")

	del := func(id string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/silences/"+id, nil))
		return w.Code
	}
	require.Equal(t, http.StatusNoContent, del(id))
	var startsAt, endsAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT starts_at, ends_at FROM silences WHERE silence_id=$1`, id).Scan(&startsAt, &endsAt))
	require.True(t, startsAt.Equal(endsAt), "a scheduled silence ends at its own start, starts %s ends %s", startsAt, endsAt)

	expired := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO silences (silence_id, matchers, reason, starts_at, ends_at) VALUES ($1,'[{"name":"rule_id","value":"agent_down"}]','maintenance',$2,$3)`, expired, now.Add(-2*time.Hour), now.Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, del(expired.String()))
	require.NoError(t, pool.QueryRow(ctx, `SELECT ends_at FROM silences WHERE silence_id=$1`, expired).Scan(&endsAt))
	require.WithinDuration(t, now.Add(-time.Hour), endsAt, time.Second, "an expired silence keeps the end it had")
}
