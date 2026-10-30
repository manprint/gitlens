package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
)

// Canary addresses stored from the console (11.5b): a test fails if any of
// them, or their secret parts, reaches a response body or the log (I-10).
const (
	canaryStoredChatURL    = "https://chat.googleapis.com/v1/spaces/STOREDSPACE0101/messages?key=storedChatKey0102&token=storedChatToken0103"
	canaryStoredWebhookURL = "https://storedUser0104:storedPassword0105@hooks.stored.example/pglens/storedWebhookPath0106?token=storedWebhookToken0107"
)

var canaryStoredSecrets = []string{
	canaryStoredChatURL, canaryStoredWebhookURL,
	"STOREDSPACE0101", "storedChatKey0102", "storedChatToken0103",
	"storedUser0104", "storedPassword0105", "storedWebhookPath0106", "storedWebhookToken0107",
}

// stubNotificationStore is a notificationChannelStore in memory. err fails
// every call.
type stubNotificationStore struct {
	mu   sync.Mutex
	rows map[string]string
	by   map[string]string
	sets int
	err  error
}

func newStubNotificationStore() *stubNotificationStore {
	return &stubNotificationStore{rows: map[string]string{}, by: map[string]string{}}
}

func (s *stubNotificationStore) NotificationChannels(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]string{}
	for k, v := range s.rows {
		out[k] = v
	}
	return out, nil
}

func (s *stubNotificationStore) SetNotificationChannel(_ context.Context, kind, address, updatedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.err != nil {
		return s.err
	}
	s.rows[kind] = address
	s.by[kind] = updatedBy
	return nil
}

func (s *stubNotificationStore) DeleteNotificationChannel(_ context.Context, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	delete(s.rows, kind)
	return nil
}

func (s *stubNotificationStore) stored(kind string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	address, ok := s.rows[kind]
	return address, ok
}

// storedAPI is an API over the environment cfg, whose channels are built as
// the server builds them, with a store in memory.
func storedAPI(cfg AlertConfig) (*API, *stubNotificationStore) {
	channels := []notify.Channel{}
	for _, k := range notificationKinds {
		if address := k.url(cfg); address != "" {
			channels = append(channels, NewNotificationChannel(k.Kind, address))
		}
	}
	api := NewAPI(nil)
	api.SetNotificationChannels(cfg, channels)
	st := newStubNotificationStore()
	api.notificationStore = st
	return api, st
}

func urlBody(t *testing.T, address string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"url": address})
	require.NoError(t, err)
	return string(b)
}

func decodeChannelRow(t *testing.T, rec *httptest.ResponseRecorder) notificationChannelJSON {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var row notificationChannelJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))
	return row
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &logged
}

func requireNoSecret(t *testing.T, secrets []string, texts ...string) {
	t.Helper()
	for _, secret := range secrets {
		for i, text := range texts {
			require.NotContains(t, text, secret, "text %d", i)
		}
	}
}

// NOTI-006: a valid https address stored from the console answers the kind's
// status row from the console, with the host only; the listing shows it; the
// store gets the address and who set it; no response or log line holds it. A
// store that fails is a 503 whose detail does not quote the address either.
func TestNOTI006_SetValidURL(t *testing.T) {
	logged := captureLog(t)
	api, st := storedAPI(AlertConfig{GoogleChatURL: canaryGoogleChatURL})
	bodies := []string{}

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/google_chat", urlBody(t, canaryStoredChatURL))
	bodies = append(bodies, rec.Body.String())
	row := decodeChannelRow(t, rec)
	require.Equal(t, notificationChannelJSON{Kind: "google_chat", Configured: true, Source: "ui", Host: strPtr("chat.googleapis.com")}, row)
	stored, ok := st.stored("google_chat")
	require.True(t, ok)
	require.Equal(t, canaryStoredChatURL, stored)
	require.Contains(t, st.by["google_chat"], "ui-session")

	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, canaryStoredWebhookURL))
	bodies = append(bodies, rec.Body.String())
	require.Equal(t, "hooks.stored.example", *decodeChannelRow(t, rec).Host)

	listing := retentionRequest(t, api, http.MethodGet, "/api/v1/notification-channels", "")
	bodies = append(bodies, listing.Body.String())
	require.JSONEq(t, `[
		{"kind":"slack","configured":false,"source":"env","host":null},
		{"kind":"webhook","configured":true,"source":"ui","host":"hooks.stored.example"},
		{"kind":"google_chat","configured":true,"source":"ui","host":"chat.googleapis.com"}
	]`, listing.Body.String())

	// The store is down: nothing changes, and the error that quotes the
	// address is redacted.
	st.err = fmt.Errorf("write %s failed: %s", canaryStoredWebhookURL, "storedWebhookToken0107")
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/slack", urlBody(t, canaryStoredWebhookURL))
	bodies = append(bodies, rec.Body.String())
	requireAPIError(t, rec, http.StatusServiceUnavailable, "store unavailable")
	require.False(t, notificationRows(t, api)[0].Configured)
	rec = retentionRequest(t, api, http.MethodDelete, "/api/v1/notification-channels/webhook", "")
	bodies = append(bodies, rec.Body.String())
	requireAPIError(t, rec, http.StatusServiceUnavailable, "store unavailable")
	require.Equal(t, "ui", notificationRows(t, api)[1].Source, "a failed removal keeps the stored address")

	output := logged.String()
	require.Contains(t, output, "notification channel google_chat set from the console by ui-session")
	require.Contains(t, output, "host chat.googleapis.com")
	require.Contains(t, output, "notification channel webhook set from the console by ui-session")
	requireNoSecret(t, append(append([]string{}, canaryStoredSecrets...), canarySecrets...), append(bodies, output)...)
}

// NOTI-007: the address is validated, and a refused one is a 400 whose detail
// never quotes it; nothing is stored.
func TestNOTI007_Validation(t *testing.T) {
	logged := captureLog(t)
	long := "https://hooks.canary.example/canaryLong0201/" + strings.Repeat("a", maxWebhookURLLength)
	secrets := []string{
		"hooks.canary.example", "canaryPath0202", "canaryTok0203", "canaryFrag0204", "canaryOpaque0205",
		"canary%zzhost0206", "canarySpace0209", "canaryLong0201", "canaryFtp0207", "canaryPublic0208",
	}
	for _, tc := range []struct {
		name string
		body string
		code int
	}{
		{"https", urlBody(t, "https://hooks.canary.example/canaryPath0202?token=canaryTok0203"), http.StatusOK},
		{"http public host", urlBody(t, "http://hooks.canary.example/canaryPath0202?token=canaryTok0203"), http.StatusBadRequest},
		{"http public address", urlBody(t, "http://8.8.8.8/canaryPublic0208"), http.StatusBadRequest},
		{"http 172.32 is public", urlBody(t, "http://172.32.0.1/canaryPublic0208"), http.StatusBadRequest},
		{"http 127.0.0.1", urlBody(t, "http://127.0.0.1:8080/canaryPath0202"), http.StatusOK},
		{"http 127.1.2.3", urlBody(t, "http://127.1.2.3/canaryPath0202"), http.StatusOK},
		{"http 10.x", urlBody(t, "http://10.20.30.40/canaryPath0202"), http.StatusOK},
		{"http 172.16.x", urlBody(t, "http://172.16.0.1/canaryPath0202"), http.StatusOK},
		{"http 192.168.x", urlBody(t, "http://192.168.1.10:9000/canaryPath0202"), http.StatusOK},
		{"http localhost", urlBody(t, "http://localhost:8080/canaryPath0202"), http.StatusOK},
		{"http ::1", urlBody(t, "http://[::1]:8080/canaryPath0202"), http.StatusOK},
		{"http fd00::", urlBody(t, "http://[fd00::1]/canaryPath0202"), http.StatusOK},
		{"http upper-case scheme and localhost", urlBody(t, "HTTP://LocalHost:8080/canaryPath0202"), http.StatusOK},
		{"http ::ffff:10.x", urlBody(t, "http://[::ffff:10.0.0.1]/canaryPath0202"), http.StatusOK},
		{"http ::ffff:8.8.8.8 is public", urlBody(t, "http://[::ffff:8.8.8.8]/canaryPublic0208"), http.StatusBadRequest},
		// Not a literal address: no WHATWG normalisation of the host.
		{"http 127.1", urlBody(t, "http://127.1/canaryPublic0208"), http.StatusBadRequest},
		{"http 2130706433", urlBody(t, "http://2130706433/canaryPublic0208"), http.StatusBadRequest},
		{"http 0x0a.1.2.3", urlBody(t, "http://0x0a.1.2.3/canaryPublic0208"), http.StatusBadRequest},
		{"http 010.0.0.1", urlBody(t, "http://010.0.0.1/canaryPublic0208"), http.StatusBadRequest},
		{"bad port", urlBody(t, "https://hooks.canary.example:8o/canaryPath0202"), http.StatusBadRequest},
		{"ftp", urlBody(t, "ftp://hooks.canary.example/canaryFtp0207"), http.StatusBadRequest},
		{"no scheme", urlBody(t, "hooks.canary.example/canaryPath0202"), http.StatusBadRequest},
		{"no host", urlBody(t, "https:///canaryPath0202"), http.StatusBadRequest},
		{"opaque", urlBody(t, "https:canaryOpaque0205"), http.StatusBadRequest},
		{"does not parse", urlBody(t, "https://canary%zzhost0206/canaryPath0202"), http.StatusBadRequest},
		{"space", urlBody(t, "https://hooks.canary.example/canary Space0209"), http.StatusBadRequest},
		{"leading space", urlBody(t, " https://hooks.canary.example/canarySpace0209"), http.StatusBadRequest},
		{"trailing space", urlBody(t, "https://hooks.canary.example/canarySpace0209 "), http.StatusBadRequest},
		{"tab", urlBody(t, "https://hooks.canary.example/canary\tSpace0209"), http.StatusBadRequest},
		{"line break", urlBody(t, "https://hooks.canary.example/canarySpace0209\r\n"), http.StatusBadRequest},
		{"longer than 2048", urlBody(t, long), http.StatusBadRequest},
		{"fragment", urlBody(t, "https://hooks.canary.example/canaryPath0202#canaryFrag0204"), http.StatusBadRequest},
		{"empty", urlBody(t, ""), http.StatusBadRequest},
		{"missing", `{}`, http.StatusBadRequest},
		{"null", `{"url":null}`, http.StatusBadRequest},
		{"not a string", `{"url":42}`, http.StatusBadRequest},
		{"extra field", `{"url":"https://hooks.canary.example/canaryPath0202","extra":"canaryTok0203"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, st := storedAPI(AlertConfig{})
			rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", tc.body)
			require.Equal(t, tc.code, rec.Code, rec.Body.String())
			if tc.code == http.StatusOK {
				require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
				return
			}
			var got apiError
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.NotEmpty(t, got.Detail)
			requireNoSecret(t, secrets, rec.Body.String())
			_, stored := st.stored("webhook")
			require.False(t, stored, "a refused address is not stored")
			require.Equal(t, "env", notificationRows(t, api)[1].Source)
		})
	}
	// The log names the host of an accepted address, never the rest of it.
	requireNoSecret(t, secrets[1:], logged.String())
}

// NOTI-008: removing the stored address goes back to the environment's
// address and channel when there is one, to not configured otherwise; removing
// again, or with nothing stored, is a 200 that changes nothing.
func TestNOTI008_DeleteFallsBackToTheEnvironment(t *testing.T) {
	logged := captureLog(t)
	api, stubs := stubChannels(nil)
	st := newStubNotificationStore()
	api.notificationStore = st

	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/google_chat", urlBody(t, "https://chat.stored.example/v1/spaces/X/messages"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
	// The stored address replaced the environment's channel.
	require.Same(t, stubs["google_chat"], api.notifications.env["google_chat"])
	require.NotEqual(t, redactingChannel{Channel: stubs["google_chat"], n: api.notifications}, api.notifications.slots["google_chat"].Channel())

	for i := 0; i < 2; i++ {
		rec = retentionRequest(t, api, http.MethodDelete, "/api/v1/notification-channels/google_chat", "")
		require.Equal(t, notificationChannelJSON{Kind: "google_chat", Configured: true, Source: "env", Host: strPtr("chat.googleapis.com")}, decodeChannelRow(t, rec))
		_, stored := st.stored("google_chat")
		require.False(t, stored)
		// The test send reaches the environment's channel again.
		code, result, _ := testSend(t, api, "google_chat")
		require.Equal(t, http.StatusOK, code)
		require.True(t, result.Delivered)
	}
	require.Len(t, stubs["google_chat"].messages, 2)

	// Without an environment address the kind is not configured afterwards.
	bare, bareStore := storedAPI(AlertConfig{})
	rec = retentionRequest(t, bare, http.MethodPut, "/api/v1/notification-channels/slack", urlBody(t, "https://hooks.slack.example/services/X"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
	for i := 0; i < 2; i++ {
		rec = retentionRequest(t, bare, http.MethodDelete, "/api/v1/notification-channels/slack", "")
		require.Equal(t, notificationChannelJSON{Kind: "slack", Configured: false, Source: "env", Host: nil}, decodeChannelRow(t, rec))
		code, _, body := testSend(t, bare, "slack")
		require.Equal(t, http.StatusConflict, code, body)
	}
	require.Empty(t, bareStore.rows)
	// A kind with nothing stored ever.
	rec = retentionRequest(t, bare, http.MethodDelete, "/api/v1/notification-channels/webhook", "")
	require.False(t, decodeChannelRow(t, rec).Configured)

	output := logged.String()
	require.Contains(t, output, "notification channel google_chat removed from the console by ui-session")
	require.Contains(t, output, "now the environment's URL")
	require.Contains(t, output, "notification channel slack removed from the console by ui-session")
	require.Contains(t, output, "now not configured")
	require.NotContains(t, output, "chat.stored.example/v1")
}

// NOTI-009: an unknown kind is a 404 for PUT and DELETE and stores nothing.
func TestNOTI009_UnknownKind(t *testing.T) {
	t.Parallel()
	api, st := storedAPI(AlertConfig{})
	for _, kind := range []string{"email", "Slack", "google-chat", "googlechat", "webhooks"} {
		rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/"+kind, urlBody(t, "https://hooks.example/x"))
		requireAPIError(t, rec, http.StatusNotFound, "unknown channel")
		rec = retentionRequest(t, api, http.MethodDelete, "/api/v1/notification-channels/"+kind, "")
		requireAPIError(t, rec, http.StatusNotFound, "unknown channel")
	}
	require.Zero(t, st.sets)
	require.Empty(t, st.rows)
}

// alertDeliveryStore is an alert.Store that claims every delivery once.
type alertDeliveryStore struct{}

func (alertDeliveryStore) Rules(context.Context) ([]alert.Rule, error) { return nil, nil }
func (alertDeliveryStore) Silences(context.Context, time.Time) ([]alert.Silence, error) {
	return nil, nil
}
func (alertDeliveryStore) Upsert(context.Context, alert.Alert) error { return nil }
func (alertDeliveryStore) ClaimNotification(context.Context, string, string, string, alert.Alert) (bool, error) {
	return true, nil
}
func (alertDeliveryStore) MarkNotification(context.Context, string, string, string, bool, int, error) error {
	return nil
}
func (alertDeliveryStore) Active(context.Context, alert.Filter) ([]alert.Alert, error) {
	return nil, nil
}

// receiver is a webhook endpoint that counts what it receives.
type receiver struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []string
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		var m notify.Message
		_ = json.Unmarshal(body, &m)
		r.mu.Lock()
		r.got = append(r.got, m.RuleID)
		r.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) received() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.got...)
}

// NOTI-010: the alert notifier built over the API's slots delivers to the
// address stored from the console on the next notification after a PUT, to
// the replacement after another PUT, back to the environment's address after
// a DELETE, and to nothing after a DELETE that leaves no address.
func TestNOTI010_AlertPathFollowsTheConsole(t *testing.T) {
	envHook, first, second := newReceiver(t), newReceiver(t), newReceiver(t)
	fire := func(n alert.Notifier, rule string) {
		t.Helper()
		at := time.Now().UTC()
		require.NoError(t, n.Notify(context.Background(), alert.Alert{Key: rule, RuleID: rule, Severity: alert.SeverityWarning, State: alert.StateFiring, Summary: rule, StartedAt: at, LastEvalAt: at}))
	}

	// No environment address: the notifier starts with nothing to deliver to.
	api, _ := storedAPI(AlertConfig{})
	n := alert.NewChannelNotifier(alertDeliveryStore{}, api.NotificationChannels()...)
	fire(n, "r0")
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, first.srv.URL+"/hook"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
	fire(n, "r1")
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, second.srv.URL+"/hook"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
	fire(n, "r2")
	rec = retentionRequest(t, api, http.MethodDelete, "/api/v1/notification-channels/webhook", "")
	require.False(t, decodeChannelRow(t, rec).Configured)
	fire(n, "r3")
	require.Equal(t, []string{"r1"}, first.received())
	require.Equal(t, []string{"r2"}, second.received())

	// With an environment address, removing the stored one goes back to it.
	api, _ = storedAPI(AlertConfig{WebhookURL: envHook.srv.URL + "/env"})
	n = alert.NewChannelNotifier(alertDeliveryStore{}, api.NotificationChannels()...)
	fire(n, "e1")
	rec = retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, first.srv.URL+"/hook"))
	require.Equal(t, "ui", decodeChannelRow(t, rec).Source)
	fire(n, "e2")
	rec = retentionRequest(t, api, http.MethodDelete, "/api/v1/notification-channels/webhook", "")
	require.Equal(t, "env", decodeChannelRow(t, rec).Source)
	fire(n, "e3")
	require.Equal(t, []string{"e1", "e3"}, envHook.received())
	require.Equal(t, []string{"r1", "e2"}, first.received())
}

// NOTI-011: a test send that fails after a PUT reports the transport error of
// the real channel with the stored address, and its secret parts, removed. An
// address replaced since stays in the redaction set.
func TestNOTI011_FailingTestSendRedactsTheStoredURL(t *testing.T) {
	logged := captureLog(t)
	const (
		replacedURL = "https://hooks.replaced.example/canaryReplaced0301?token=canaryReplacedTok0302"
		storedURL   = "http://canaryUser0303:canaryPw0304@127.0.0.1:1/canaryPath0305?token=canaryTok0306"
	)
	secrets := []string{replacedURL, storedURL, "canaryReplaced0301", "canaryReplacedTok0302", "canaryUser0303", "canaryPw0304", "canaryPath0305", "canaryTok0306"}
	api, _ := storedAPI(AlertConfig{})
	api.notifications.timeout = 9 * time.Second
	bodies := []string{}
	for _, address := range []string{replacedURL, storedURL} {
		rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, address))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		bodies = append(bodies, rec.Body.String())
	}
	code, result, body := testSend(t, api, "webhook")
	require.Equal(t, http.StatusOK, code, body)
	require.False(t, result.Delivered)
	require.NotEmpty(t, result.Error)
	bodies = append(bodies, body)

	// The alert path delivers through the same slot: its error, which the
	// engine logs and records, is redacted the same way.
	alertErr := api.NotificationChannels()[1].Send(context.Background(), notify.Message{RuleID: "r"})
	require.Error(t, alertErr)
	bodies = append(bodies, alertErr.Error())

	// The replaced address is still redacted, in every form.
	redacted := api.notifications.redact(errors.New("Post " + replacedURL + ": refused canaryReplacedTok0302")).Error()
	require.Contains(t, redacted, "[redacted webhook]")
	bodies = append(bodies, redacted)
	requireNoSecret(t, secrets, append(bodies, logged.String())...)
}

// NOTI-011: a channel never follows a redirect, from the console or the
// environment: the address was checked when it was stored, the redirect target
// never was. The redirect answer is a failed delivery and the target receives
// nothing.
func TestNOTI011_RedirectIsNotFollowed(t *testing.T) {
	target := newReceiver(t)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, target.srv.URL+"/hook", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)

	api, _ := storedAPI(AlertConfig{})
	rec := retentionRequest(t, api, http.MethodPut, "/api/v1/notification-channels/webhook", urlBody(t, redirector.URL+"/canaryRedirect0401"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	code, result, body := testSend(t, api, "webhook")
	require.Equal(t, http.StatusOK, code, body)
	require.False(t, result.Delivered)
	require.Contains(t, result.Error, "HTTP 307")

	envErr := NewNotificationChannel("webhook", redirector.URL+"/canaryRedirect0402").Send(context.Background(), notify.Message{RuleID: "r"})
	require.Error(t, envErr)
	require.Contains(t, envErr.Error(), "HTTP 307")
	require.Empty(t, target.received(), "the redirect target is never posted to")
}

// NOTI-011: the replaced addresses kept for the redaction are bounded: the
// newest maxReplacedAddresses and the current one are redacted, an older one
// is no longer kept.
func TestNOTI011_ReplacedAddressesAreBounded(t *testing.T) {
	t.Parallel()
	api, _ := storedAPI(AlertConfig{})
	k, ok := lookupNotificationKind("webhook")
	require.True(t, ok)
	address := func(i int) string { return fmt.Sprintf("https://hooks.canary.example/canaryCap%04d", i) }
	total := maxReplacedAddresses + 8
	for i := range total {
		api.notifications.apply(k, address(i))
	}
	require.Len(t, api.notifications.replaced, maxReplacedAddresses)
	redact := func(i int) string { return api.notifications.redact(errors.New("Post " + address(i))).Error() }
	require.NotContains(t, redact(total-1), "canaryCap", "the current address")
	require.NotContains(t, redact(total-2), "canaryCap", "the newest replaced address")
	require.NotContains(t, redact(total-1-maxReplacedAddresses), "canaryCap", "the oldest replaced address kept")
	require.Contains(t, redact(0), "canaryCap0000", "an address replaced before the newest 32 is no longer kept")

	// Removing keeps the removed address among the replaced ones.
	api.notifications.unapply(k)
	require.NotContains(t, redact(total-1), "canaryCap")
}

// NOTI-012: storing and removing an address are writes behind the session and
// the cross-origin gate, like the other Settings writes; a same-origin request
// reaches the handler (503: no store on the security router).
func TestNOTI012_GatedWrites(t *testing.T) {
	t.Parallel()
	router, token := securityRouter(t)
	send := func(method, target, body string, cookie bool, headers map[string]string) int {
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, target, http.NoBody)
		} else {
			req = httptest.NewRequest(method, target, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
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
	target := "/api/v1/notification-channels/google_chat"
	body := urlBody(t, "https://chat.example/v1/spaces/X/messages")
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		b := body
		if method == http.MethodDelete {
			b = ""
		}
		require.Equal(t, http.StatusUnauthorized, send(method, target, b, false, nil), method)
		require.Equal(t, http.StatusForbidden, send(method, target, b, true, map[string]string{"Sec-Fetch-Site": "cross-site"}), method)
		require.Equal(t, http.StatusForbidden, send(method, target, b, true, map[string]string{"Origin": "https://evil.example"}), method)
		require.Equal(t, http.StatusServiceUnavailable, send(method, target, b, true, map[string]string{"Sec-Fetch-Site": "same-origin"}), method)
	}
}
