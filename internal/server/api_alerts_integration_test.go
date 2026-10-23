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

// TestAlertAPI_INT_ALERTAPI_003_DeleteKeepsTheSchedule — deleting a silence ends
// an active one now. An expired silence keeps the end it had, and a future one
// ends at its start, so no silence is given a window it was never scheduled.
func TestAlertAPI_INT_ALERTAPI_003_DeleteKeepsTheSchedule(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	r := alertRouter(NewAlertAPI(pool, alert.NewPgStore(pool)))
	ctx := context.Background()
	now := time.Now().UTC()
	active, expired, future := uuid.New(), uuid.New(), uuid.New()
	starts := map[uuid.UUID]time.Time{active: now.Add(-time.Hour), expired: now.Add(-3 * time.Hour), future: now.Add(2 * time.Hour)}
	ends := map[uuid.UUID]time.Time{active: now.Add(time.Hour), expired: now.Add(-2 * time.Hour), future: now.Add(3 * time.Hour)}
	for id := range starts {
		_, err := pool.Exec(ctx, `INSERT INTO silences (silence_id, matchers, reason, starts_at, ends_at) VALUES ($1, '[]'::jsonb, 'test', $2, $3)`, id, starts[id], ends[id])
		require.NoError(t, err)
	}
	for _, id := range []uuid.UUID{active, expired, future} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v1/silences/"+id.String(), nil))
		require.Equal(t, http.StatusNoContent, w.Code)
	}
	endOf := func(id uuid.UUID) time.Time {
		var got time.Time
		require.NoError(t, pool.QueryRow(ctx, `SELECT ends_at FROM silences WHERE silence_id=$1`, id).Scan(&got))
		return got
	}
	require.WithinDuration(t, time.Now(), endOf(active), 5*time.Second, "an active silence ends now")
	require.WithinDuration(t, ends[expired], endOf(expired), time.Millisecond, "an expired silence keeps its end")
	require.WithinDuration(t, starts[future], endOf(future), time.Millisecond, "a future silence ends at its start")
}
