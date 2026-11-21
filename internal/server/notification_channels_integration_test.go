//go:build integration

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
)

func cleanNotificationChannels(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `DELETE FROM notification_channels`)
	require.NoError(t, err)
}

// envAPI is the server's wiring at start: the environment's channels, then the
// addresses stored from the console over them.
func envAPI(t *testing.T, pool *pgxpool.Pool, cfg AlertConfig) *API {
	t.Helper()
	channels := []notify.Channel{}
	for _, k := range notificationKinds {
		if address := k.url(cfg); address != "" {
			channels = append(channels, NewNotificationChannel(cfg, k.Kind, address))
		}
	}
	api := NewAPI(pool)
	api.SetNotificationChannels(cfg, channels)
	require.NoError(t, api.LoadNotificationChannels(context.Background()))
	return api
}

// NOTI-INT-001: on a migrated database, an address stored through the API is
// a row with who set it; a fresh wiring of the server loads it at start, over
// the environment's address of the same kind, and delivers through it; DELETE
// removes the row and the kind goes back to the environment.
func TestNOTIINT001_StoredURLPersistsAndOverridesEnv(t *testing.T) {
	pool := getSharedPool(t)
	cleanNotificationChannels(t, pool)
	t.Cleanup(func() { cleanNotificationChannels(t, pool) })
	ctx := context.Background()

	envChat := newReceiver(t)
	storedChat := newReceiver(t)
	cfg := AlertConfig{GoogleChatURL: envChat.srv.URL + "/env"}

	api := envAPI(t, pool, cfg)
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/google_chat", urlBody(t, storedChat.srv.URL+"/stored"))
	require.Equal(t, notificationChannelJSON{Kind: "google_chat", Configured: true, Source: "ui", Host: strPtr("127.0.0.1")}, decodeChannelRow(t, rec))
	var (
		stored, by string
	)
	require.NoError(t, pool.QueryRow(ctx, `SELECT url, updated_by FROM notification_channels WHERE tenant_id = 'default' AND kind = 'google_chat'`).Scan(&stored, &by))
	require.Equal(t, storedChat.srv.URL+"/stored", stored)
	require.Contains(t, by, "ui-session")

	// A restart: the stored address wins over the environment's.
	restarted := envAPI(t, pool, cfg)
	require.Equal(t, "ui", notificationRows(t, restarted)[2].Source)
	code, result, body := testSend(t, restarted, "google_chat")
	require.Equal(t, http.StatusOK, code, body)
	require.True(t, result.Delivered, body)
	require.Len(t, storedChat.received(), 1)
	require.Empty(t, envChat.received())

	rec = retentionRequest(t, restarted, http.MethodDelete, "/api/v1/notification-channels/google_chat", "")
	require.Equal(t, "env", decodeChannelRow(t, rec).Source)
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM notification_channels`).Scan(&rows))
	require.Zero(t, rows)
	code, result, body = testSend(t, restarted, "google_chat")
	require.Equal(t, http.StatusOK, code, body)
	require.True(t, result.Delivered, body)
	require.Len(t, envChat.received(), 1)

	// And after the next restart the environment is all there is.
	require.Equal(t, "env", notificationRows(t, envAPI(t, pool, cfg))[2].Source)
}

// NOTI-INT-002 (plan 006 F7): a channel language set through the API is a row
// with who set it; a fresh wiring of the server puts it back in the slot, and
// null removes the row.
func TestNOTIINT002_ChannelLanguagePersists(t *testing.T) {
	pool := getSharedPool(t)
	clean := func() {
		_, err := pool.Exec(context.Background(), `DELETE FROM notification_channel_languages`)
		require.NoError(t, err)
	}
	clean()
	t.Cleanup(clean)
	ctx := context.Background()
	cfg := AlertConfig{SlackURL: "https://hooks.slack.example/services/X"}

	api := envAPI(t, pool, cfg)
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/slack/language", `{"language":"it"}`)
	require.Equal(t, strPtr("it"), decodeChannelRow(t, rec).Language)
	var language, by string
	require.NoError(t, pool.QueryRow(ctx, `SELECT language, updated_by FROM notification_channel_languages WHERE tenant_id = 'default' AND kind = 'slack'`).Scan(&language, &by))
	require.Equal(t, "it", language)
	require.Contains(t, by, "ui-session")

	restarted := envAPI(t, pool, cfg)
	require.Equal(t, "it", restarted.notificationState().slots["slack"].Locale())
	require.Equal(t, strPtr("it"), notificationRows(t, restarted)[0].Language)

	rec = retentionRequest(t, restarted, http.MethodPut, "/api/v1/notification-channels/slack/language", `{"language":null}`)
	require.Nil(t, decodeChannelRow(t, rec).Language)
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM notification_channel_languages`).Scan(&rows))
	require.Zero(t, rows)
	require.Equal(t, "", envAPI(t, pool, cfg).notificationState().slots["slack"].Locale())
}

// NCH-INT-001: a Teams URL stored through the API on a migrated database is
// loaded by a fresh wiring of the server, and a firing alert reaches it as an
// Adaptive Card.
func TestINTNCH001_StoredTeamsChannelDelivers(t *testing.T) {
	pool := getSharedPool(t)
	cleanNotificationChannels(t, pool)
	t.Cleanup(func() { cleanNotificationChannels(t, pool) })

	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)

	api := envAPI(t, pool, AlertConfig{})
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/teams", urlBody(t, srv.URL+"/workflows/x"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)

	restarted := envAPI(t, pool, AlertConfig{})
	slot := restarted.notificationState().slots["teams"]
	require.True(t, notify.Configured(slot))
	require.NoError(t, slot.Send(context.Background(), notify.Message{
		DedupID: "d1", RuleID: "replication_lag", Severity: "critical", Phase: "firing",
		Summary: "replication lag high", Cluster: "c1", Instance: "i1", At: time.Now(),
	}))
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	require.Contains(t, bodies[0], `"AdaptiveCard"`)
	require.Contains(t, bodies[0], "replication lag high")
}
