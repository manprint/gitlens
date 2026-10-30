package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert/notify"
)

// Canary webhook addresses: a test fails if any of them, or their secret
// parts, reaches a response body or the log (I-10).
const (
	canarySlackURL      = "https://hooks.slack.com/services/T0CANARY/B0CANARY/canarySlackSecret0001"
	canaryWebhookURL    = "https://canaryUser0007:canaryPassword0008@alerts.canary.example/pglens/canaryWebhookPath0002?token=canaryWebhookToken0003"
	canaryGoogleChatURL = "https://chat.googleapis.com/v1/spaces/CANARYSPACE0004/messages?key=canaryChatKey0005&token=canaryChatToken0006"
)

// canarySecrets are the parts of the canary addresses that must never leave
// the server: each address and every path or query part that identifies it.
var canarySecrets = []string{
	canarySlackURL, canaryWebhookURL, canaryGoogleChatURL,
	"canarySlackSecret0001", "canaryWebhookPath0002", "canaryWebhookToken0003",
	"CANARYSPACE0004", "canaryChatKey0005", "canaryChatToken0006",
	"T0CANARY", "B0CANARY", "canaryUser0007", "canaryPassword0008",
}

func canaryConfig() AlertConfig {
	return AlertConfig{SlackURL: canarySlackURL, WebhookURL: canaryWebhookURL, GoogleChatURL: canaryGoogleChatURL}
}

// stubChannel is a notify.Channel that records what it is sent. send decides
// the outcome; nil delivers.
type stubChannel struct {
	name string
	send func(ctx context.Context, m notify.Message) error

	mu        sync.Mutex
	messages  []notify.Message
	deadlines []time.Time
}

func (c *stubChannel) Name() string { return c.name }

func (c *stubChannel) Send(ctx context.Context, m notify.Message) error {
	c.mu.Lock()
	c.messages = append(c.messages, m)
	deadline, _ := ctx.Deadline()
	c.deadlines = append(c.deadlines, deadline)
	c.mu.Unlock()
	if c.send == nil {
		return nil
	}
	return c.send(ctx, m)
}

// stubChannels is one stubChannel per kind, configured with the canary
// addresses.
func stubChannels(send func(ctx context.Context, m notify.Message) error) (*API, map[string]*stubChannel) {
	stubs := map[string]*stubChannel{}
	channels := []notify.Channel{}
	for _, k := range notificationKinds {
		stub := &stubChannel{name: k.Kind, send: send}
		stubs[k.Kind] = stub
		channels = append(channels, stub)
	}
	api := NewAPI(nil)
	api.SetNotificationChannels(canaryConfig(), channels)
	return api, stubs
}

func notificationRows(t *testing.T, api *API) []notificationChannelJSON {
	t.Helper()
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []notificationChannelJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	return rows
}

func testSend(t *testing.T, api *API, kind string) (int, notificationTestJSON, string) {
	t.Helper()
	rec := retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/"+kind+"/test", "")
	var result notificationTestJSON
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	}
	return rec.Code, result, rec.Body.String()
}

// NOTI-001: channels configured in the environment are listed configured, from
// the environment, with the host name of their address and nothing else of it.
func TestNOTI001_EnvConfiguredChannelsShowHostOnly(t *testing.T) {
	t.Parallel()
	api, _ := stubChannels(nil)
	rec := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[
		{"kind":"slack","configured":true,"source":"env","host":"hooks.slack.com"},
		{"kind":"webhook","configured":true,"source":"env","host":"alerts.canary.example"},
		{"kind":"google_chat","configured":true,"source":"env","host":"chat.googleapis.com"}
	]`, rec.Body.String())

	// The host is the host name only: no port, user information or path.
	api = NewAPI(nil)
	api.SetNotificationChannels(AlertConfig{WebhookURL: "https://user:pass@hooks.canary.example:8443/path?q=1"}, nil)
	rows := notificationRows(t, api)
	require.NotNil(t, rows[1].Host)
	require.Equal(t, "hooks.canary.example", *rows[1].Host)
}

// NOTI-002: a channel without an address is configured false with host null;
// the three kinds are always listed, in order, with source env. An address
// that does not parse, or names no host, is configured with host null.
func TestNOTI002_UnconfiguredChannels(t *testing.T) {
	t.Parallel()
	rec := retentionRequest(t, NewAPI(nil), http.MethodGet, "/api/v1/notification-channels", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[
		{"kind":"slack","configured":false,"source":"env","host":null},
		{"kind":"webhook","configured":false,"source":"env","host":null},
		{"kind":"google_chat","configured":false,"source":"env","host":null}
	]`, rec.Body.String())

	api := NewAPI(nil)
	api.SetNotificationChannels(AlertConfig{GoogleChatURL: canaryGoogleChatURL}, nil)
	rows := notificationRows(t, api)
	require.Equal(t, []string{"slack", "webhook", "google_chat"}, []string{rows[0].Kind, rows[1].Kind, rows[2].Kind})
	require.False(t, rows[0].Configured)
	require.False(t, rows[1].Configured)
	require.True(t, rows[2].Configured)

	api = NewAPI(nil)
	api.SetNotificationChannels(AlertConfig{SlackURL: "://canary-not-a-url", WebhookURL: "canary-no-host"}, nil)
	rows = notificationRows(t, api)
	require.True(t, rows[0].Configured)
	require.Nil(t, rows[0].Host)
	require.True(t, rows[1].Configured)
	require.Nil(t, rows[1].Host)
}

// NOTI-003: a test send goes through the channel object of its kind with the
// frozen test message and a 10 s deadline; a delivery answers delivered, an
// error failed with the redacted error, an expired deadline timeout. A kind
// that is not configured is a 409 and sends nothing.
func TestNOTI003_TestSendOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("delivered", func(t *testing.T) {
		t.Parallel()
		api, stubs := stubChannels(nil)
		before := time.Now()
		code, result, _ := testSend(t, api, "google_chat")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, notificationTestJSON{Delivered: true, Status: "delivered"}, result)
		stub := stubs["google_chat"]
		require.Len(t, stub.messages, 1)
		require.Empty(t, stubs["slack"].messages)
		require.Empty(t, stubs["webhook"].messages)
		m := stub.messages[0]
		require.Equal(t, "pglens.test", m.RuleID)
		require.Equal(t, "test", m.Phase)
		require.Equal(t, "info", m.Severity)
		require.Equal(t, "pglens test notification", m.Summary)
		require.Empty(t, m.DedupID)
		require.Empty(t, m.Cluster)
		require.Empty(t, m.Instance)
		require.Empty(t, m.Database)
		require.Zero(t, m.Value)
		require.Empty(t, m.Labels)
		require.WithinDuration(t, time.Now(), m.At, 5*time.Second)
		// The deadline is 10 s after the request.
		deadline := stub.deadlines[0]
		require.False(t, deadline.IsZero())
		require.True(t, deadline.After(before.Add(9*time.Second)), deadline)
		require.False(t, deadline.After(time.Now().Add(10*time.Second)), deadline)
	})

	t.Run("failed", func(t *testing.T) {
		t.Parallel()
		api, _ := stubChannels(func(context.Context, notify.Message) error {
			return errors.New("notification to [redacted webhook] returned HTTP 400: bad payload")
		})
		code, result, _ := testSend(t, api, "slack")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, notificationTestJSON{
			Delivered: false,
			Status:    "failed",
			Error:     "notification to [redacted webhook] returned HTTP 400: bad payload",
		}, result)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		api, _ := stubChannels(func(ctx context.Context, _ notify.Message) error {
			<-ctx.Done()
			return ctx.Err()
		})
		api.notifications.timeout = 20 * time.Millisecond
		code, result, _ := testSend(t, api, "webhook")
		require.Equal(t, http.StatusOK, code)
		require.False(t, result.Delivered)
		require.Equal(t, "timeout", result.Status)
		require.Equal(t, "context deadline exceeded", result.Error)
	})

	t.Run("timeout reported as text", func(t *testing.T) {
		t.Parallel()
		// The channel's last attempt times out and reports a plain text error.
		api, _ := stubChannels(func(ctx context.Context, _ notify.Message) error {
			<-ctx.Done()
			return errors.New("Post \"[redacted webhook]\": context deadline exceeded")
		})
		api.notifications.timeout = 20 * time.Millisecond
		_, result, _ := testSend(t, api, "webhook")
		require.Equal(t, "timeout", result.Status)
	})

	t.Run("not configured", func(t *testing.T) {
		t.Parallel()
		stub := &stubChannel{name: "slack"}
		api := NewAPI(nil)
		api.SetNotificationChannels(AlertConfig{SlackURL: canarySlackURL}, []notify.Channel{stub})
		for _, kind := range []string{"webhook", "google_chat"} {
			rec := retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/"+kind+"/test", "")
			requireAPIError(t, rec, http.StatusConflict, "channel not configured")
		}
		// Without SetNotificationChannels nothing is configured.
		requireAPIError(t, retentionRequest(t, NewAPI(nil), http.MethodPost, "/api/v1/notification-channels/slack/test", ""), http.StatusConflict, "channel not configured")
		require.Empty(t, stub.messages)
	})
}

// NOTI-003 (in flight): a second test send of a kind while one is running is
// a 429 and sends nothing; another kind is not held back, and once the first
// send ends the kind can be tested again.
func TestNOTI003_OneTestInFlightPerKind(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	started := make(chan string, 4)
	api, stubs := stubChannels(func(_ context.Context, m notify.Message) error {
		started <- "sent"
		<-release
		return nil
	})
	first := make(chan int, 1)
	go func() {
		first <- retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/google_chat/test", "").Code
	}()
	<-started

	requireAPIError(t, retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/google_chat/test", ""), http.StatusTooManyRequests, "test already running")
	stubs["google_chat"].mu.Lock()
	require.Len(t, stubs["google_chat"].messages, 1)
	stubs["google_chat"].mu.Unlock()

	other := make(chan int, 1)
	go func() {
		other <- retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/slack/test", "").Code
	}()
	<-started
	close(release)
	require.Equal(t, http.StatusOK, <-first)
	require.Equal(t, http.StatusOK, <-other)

	code, result, _ := testSend(t, api, "google_chat")
	require.Equal(t, http.StatusOK, code)
	require.True(t, result.Delivered)
}

// NOTI-004: no response body and no log line carries a webhook address or a
// secret part of it, whatever the outcome; a channel error that quotes its
// address is redacted. Not parallel: it captures the process log.
func TestNOTI004_NoAddressInResponsesOrLogs(t *testing.T) {
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	bodies := []string{}
	record := func(api *API, method, target string) {
		rec := retentionRequest(t, api, method, target, "")
		bodies = append(bodies, rec.Body.String())
	}

	ok, _ := stubChannels(nil)
	record(ok, http.MethodGet, "/api/v1/notification-channels")
	for _, k := range notificationKinds {
		record(ok, http.MethodPost, "/api/v1/notification-channels/"+k.Kind+"/test")
	}

	// Errors that quote the address, as a transport error does, and one that
	// quotes only the secret path of the Slack and Google Chat addresses.
	leaky, _ := stubChannels(func(_ context.Context, m notify.Message) error {
		return fmt.Errorf("Post %q: dial tcp: lookup failed for %s %s; then %s",
			canaryGoogleChatURL, canarySlackURL, canaryWebhookURL,
			"https://chat.googleapis.com/v1/spaces/CANARYSPACE0004/messages?key=canaryChatKey0005&token=canaryChatToken0006 and hooks.slack.com/services/T0CANARY/B0CANARY/canarySlackSecret0001")
	})
	for _, k := range notificationKinds {
		record(leaky, http.MethodPost, "/api/v1/notification-channels/"+k.Kind+"/test")
	}

	// net/http quotes an address with its password replaced (URL.Redacted).
	quoted, err := url.Parse(canaryWebhookURL)
	require.NoError(t, err)
	slow, _ := stubChannels(func(ctx context.Context, _ notify.Message) error {
		<-ctx.Done()
		return fmt.Errorf("Post %q: %w", quoted.Redacted(), ctx.Err())
	})
	slow.notifications.timeout = 20 * time.Millisecond
	record(slow, http.MethodPost, "/api/v1/notification-channels/webhook/test")

	// An error that quotes the secret parts alone: query values and path
	// segments, without the rest of their address.
	bare, _ := stubChannels(func(context.Context, notify.Message) error {
		return errors.New("refused: key canaryChatKey0005 token canaryChatToken0006 space CANARYSPACE0004; slack canarySlackSecret0001; webhook canaryWebhookPath0002 canaryWebhookToken0003 canaryPassword0008")
	})
	record(bare, http.MethodPost, "/api/v1/notification-channels/google_chat/test")

	// Unconfigured and unknown kinds.
	record(NewAPI(nil), http.MethodPost, "/api/v1/notification-channels/google_chat/test")
	record(ok, http.MethodPost, "/api/v1/notification-channels/email/test")

	// The real channels on an unreachable address: net/http reports a
	// *url.Error that quotes the address with its password replaced, after
	// the three attempts of the channel's send (about 5 s of backoff). Both
	// kinds run at once to keep the test short.
	const (
		transportWebhookURL = "http://canaryUser0013:canaryPw0014@127.0.0.1:1/canaryPath0015?token=canaryTok0016"
		transportChatURL    = "http://127.0.0.1:1/v1/spaces/CANARYSPACE0017/messages?key=canaryKey0018&token=canaryTok0019"
	)
	transportSecrets := []string{transportWebhookURL, transportChatURL, "canaryUser0013", "canaryPw0014", "canaryPath0015", "canaryTok0016", "CANARYSPACE0017", "canaryKey0018", "canaryTok0019"}
	direct := &http.Client{Transport: &http.Transport{}} // no proxy: the dial itself fails
	real := NewAPI(nil)
	real.SetNotificationChannels(
		AlertConfig{WebhookURL: transportWebhookURL, GoogleChatURL: transportChatURL},
		[]notify.Channel{notify.NewWebhook(transportWebhookURL, direct), notify.NewGoogleChat(transportChatURL, direct)},
	)
	real.notifications.timeout = 9 * time.Second
	transportBodies := make([]string, 2)
	var wg sync.WaitGroup
	for i, kind := range []string{"webhook", "google_chat"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transportBodies[i] = retentionRequest(t, real, http.MethodPost, "/api/v1/notification-channels/"+kind+"/test", "").Body.String()
		}()
	}
	wg.Wait()
	for _, body := range transportBodies {
		require.Contains(t, body, `"status":"failed"`)
		require.Contains(t, body, "connect")
	}
	bodies = append(bodies, transportBodies...)

	require.Len(t, bodies, 13)
	output := logged.String()
	require.Contains(t, output, "notifications: test of google_chat by ui-session")
	require.Contains(t, output, "[redacted webhook]")
	for _, secret := range append(append([]string{}, canarySecrets...), transportSecrets...) {
		for i, body := range bodies {
			require.NotContains(t, body, secret, "response %d", i)
		}
		require.NotContains(t, output, secret, "log output")
	}
	// The redacted error still tells what failed.
	require.Contains(t, strings.Join(bodies, "\n"), `"status":"timeout"`)
	require.Contains(t, strings.Join(bodies, "\n"), "dial tcp")
}

// NOTI-005: an unknown kind is a 404 and sends nothing; the test send is a
// write behind the session and the cross-origin gate.
func TestNOTI005_UnknownKind(t *testing.T) {
	t.Parallel()
	api, stubs := stubChannels(nil)
	for _, kind := range []string{"email", "Slack", "google-chat", "googlechat", "webhooks"} {
		rec := retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels/"+kind+"/test", "")
		requireAPIError(t, rec, http.StatusNotFound, "unknown channel")
	}
	for _, stub := range stubs {
		require.Empty(t, stub.messages, stub.name)
	}
	// Only GET lists and only POST tests.
	require.Equal(t, http.StatusMethodNotAllowed, retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels/slack/test", "").Code)
	require.Equal(t, http.StatusMethodNotAllowed, retentionRequest(t, api, http.MethodPost, "/api/v1/notification-channels", "").Code)
}

// NOTI-005 (gate): without a session the routes are a 401, and a test send
// from another origin a 403; a same-origin send reaches the handler (409:
// nothing configured on the security router).
func TestNOTI005_GatedTestSend(t *testing.T) {
	t.Parallel()
	router, token := securityRouter(t)
	send := func(method, target string, cookie bool, headers map[string]string) int {
		req := httptest.NewRequest(method, target, http.NoBody)
		if cookie {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	target := "/api/v1/notification-channels/google_chat/test"
	require.Equal(t, http.StatusUnauthorized, send(http.MethodPost, target, false, nil))
	require.Equal(t, http.StatusForbidden, send(http.MethodPost, target, true, map[string]string{"Sec-Fetch-Site": "cross-site"}))
	require.Equal(t, http.StatusForbidden, send(http.MethodPost, target, true, map[string]string{"Origin": "https://evil.example"}))
	require.Equal(t, http.StatusConflict, send(http.MethodPost, target, true, map[string]string{"Sec-Fetch-Site": "same-origin"}))
	require.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/api/v1/notification-channels", false, nil))
	require.Equal(t, http.StatusOK, send(http.MethodGet, "/api/v1/notification-channels", true, nil))
}
