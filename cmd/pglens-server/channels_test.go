package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/server"
)

func channelNames(cfg server.AlertConfig) []string {
	names := []string{}
	for _, c := range buildChannels(cfg) {
		names = append(names, c.Name())
	}
	return names
}

func TestCONFIGGC002_GoogleChatChannelOnlyWhenConfigured(t *testing.T) {
	require.Empty(t, channelNames(server.AlertConfig{}))
	require.Equal(t, []string{"google_chat"}, channelNames(server.AlertConfig{GoogleChatURL: "https://chat.example/x"}))
	require.Equal(t, []string{"slack", "webhook", "google_chat"},
		channelNames(server.AlertConfig{SlackURL: "s", WebhookURL: "w", GoogleChatURL: "g"}))
	require.Equal(t, []string{"slack"}, channelNames(server.AlertConfig{SlackURL: "s"}))
}

// The console's test send (11.5a) goes through the channels the alert path
// delivers through: wireNotificationChannels builds them without a database
// and a test send of the generic webhook reaches its address with the frozen
// test message (N3, C9).
func TestNotificationChannelWiringReachesTheAlertChannel(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	api := server.NewAPI(nil)
	channels := wireNotificationChannels(api, server.AlertConfig{WebhookURL: srv.URL + "/hook"})
	require.Len(t, channels, 1)
	require.Equal(t, "webhook", channels[0].Name())

	r := chi.NewRouter()
	api.RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels/webhook/test", http.NoBody))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"delivered":true,"status":"delivered"}`, rec.Body.String())

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, bodies, 1)
	require.Equal(t, "pglens.test", bodies[0]["rule_id"])
	require.Equal(t, "test", bodies[0]["phase"])
	require.Equal(t, "info", bodies[0]["severity"])
	require.Equal(t, "pglens test notification", bodies[0]["summary"])

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/notification-channels", http.NoBody))
	require.JSONEq(t, `[
		{"kind":"slack","configured":false,"source":"env","host":null},
		{"kind":"webhook","configured":true,"source":"env","host":"127.0.0.1"},
		{"kind":"google_chat","configured":false,"source":"env","host":null}
	]`, rec.Body.String())
}
