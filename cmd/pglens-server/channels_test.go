package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
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
	channels := wireNotificationChannels(context.Background(), api, server.AlertConfig{WebhookURL: srv.URL + "/hook"})
	// One slot per kind; only the webhook's holds a channel.
	require.Len(t, channels, 3)
	require.Equal(t, []string{"slack", "webhook", "google_chat"}, []string{channels[0].Name(), channels[1].Name(), channels[2].Name()})
	require.Equal(t, []bool{false, true, false}, []bool{notify.Configured(channels[0]), notify.Configured(channels[1]), notify.Configured(channels[2])})

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

// When the addresses stored from the console cannot be read at start (the
// database is down), the environment's channel stays in its slot and delivers,
// and the log line of the failure holds no part of the address. The read is
// not retried: the environment's address is used until the next start.
func TestNotificationChannelWiringKeepsTheEnvironmentWhenTheStoreFails(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	delivered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delivered <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	address := srv.URL + "/canaryStartPath0501?token=canaryStartTok0502"

	// Nothing listens on port 1: every query fails to connect.
	pool, err := pgxpool.New(context.Background(), "postgres://canaryDbUser:canaryDbPw@127.0.0.1:1/pglens?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	api := server.NewAPI(pool)

	channels := wireNotificationChannels(context.Background(), api, server.AlertConfig{WebhookURL: address})
	require.True(t, notify.Configured(channels[1]))
	require.NoError(t, channels[1].Send(context.Background(), notify.Message{RuleID: "r"}))
	<-delivered

	logged := buf.String()
	require.Contains(t, logged, "read the URLs stored from the console (using the environment's)")
	for _, secret := range []string{address, "canaryStartPath0501", "canaryStartTok0502"} {
		require.False(t, strings.Contains(logged, secret), "the log holds %q", secret)
	}
}
