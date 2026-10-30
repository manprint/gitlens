//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

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
			channels = append(channels, NewNotificationChannel(k.Kind, address))
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
