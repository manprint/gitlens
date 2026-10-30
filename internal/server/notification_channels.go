package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/manprint/pglens/internal/alert/notify"
)

// Notification channels (11.5a, D18). The console reads which delivery
// channels the server environment configures, by kind and host only, and can
// send a test message through each one. The webhook address is never
// returned, rendered or logged (I-10). Storing an address from the console
// (11.5b) waits on Q2, so source is always "env".

// notificationKind is one delivery channel the console lists.
type notificationKind struct {
	// Kind is the channel's notify.Channel Name().
	Kind string
	// url reads the channel's webhook address from the alert configuration.
	url func(AlertConfig) string
}

// notificationKinds is the whitelist of the kinds, in the order the API lists
// them (N5).
var notificationKinds = []notificationKind{
	{Kind: "slack", url: func(c AlertConfig) string { return c.SlackURL }},
	{Kind: "webhook", url: func(c AlertConfig) string { return c.WebhookURL }},
	{Kind: "google_chat", url: func(c AlertConfig) string { return c.GoogleChatURL }},
}

func lookupNotificationKind(kind string) (notificationKind, bool) {
	for _, k := range notificationKinds {
		if k.Kind == kind {
			return k, true
		}
	}
	return notificationKind{}, false
}

const (
	// notificationTestTimeout bounds a test send. It cuts the retries of the
	// channel's own send (45 s, 3 attempts), so the console answers within it
	// (N2); a fast 5xx may still be retried inside it.
	notificationTestTimeout = 10 * time.Second
	// notificationTestSummary is the text of the test message, labelled so
	// nobody takes it for an alert.
	notificationTestSummary = "pglens test notification"
)

// notificationTestMessage is the frozen test message (C9): it names no rule,
// cluster, instance or database.
func notificationTestMessage(at time.Time) notify.Message {
	return notify.Message{
		RuleID:   "pglens.test",
		Phase:    "test",
		Severity: "info",
		Summary:  notificationTestSummary,
		At:       at,
	}
}

// notificationChannels is the delivery configuration the console reads: the
// alert configuration for the status, and the channel objects of the alert
// path for the test send.
type notificationChannels struct {
	cfg      AlertConfig
	channels map[string]notify.Channel
	// timeout replaces notificationTestTimeout in unit tests; zero means the
	// default.
	timeout time.Duration
	// running holds one flag per kind: a second test send of a kind while
	// one is in flight is refused (429), so a burst of clicks or requests
	// cannot flood a chat space.
	running map[string]*atomic.Bool
}

// SetNotificationChannels gives the API the alert configuration and the
// channel objects the alert notifier delivers through, so a test send from the
// console goes through the same instances (N3). Without it every channel reads
// as not configured. It is called before the server starts serving.
func (a *API) SetNotificationChannels(cfg AlertConfig, channels []notify.Channel) {
	byKind := make(map[string]notify.Channel, len(channels))
	for _, c := range channels {
		byKind[c.Name()] = c
	}
	running := make(map[string]*atomic.Bool, len(notificationKinds))
	for _, k := range notificationKinds {
		running[k.Kind] = &atomic.Bool{}
	}
	a.notifications = notificationChannels{cfg: cfg, channels: byKind, running: running}
}

// redact passes err through notify.RedactError for every configured address,
// not only the channel's own, and for the other forms an address is quoted in:
// net/http quotes it as URL.Redacted (a password replaced), and a part of it
// identifies the webhook too: its path and each path segment of 8 characters
// or more, its query and each query value, raw and decoded, its user
// information (I-10). The longest forms go first, so a shorter part never
// breaks the match of a longer one.
func (n notificationChannels) redact(err error) error {
	forms := []string{}
	for _, k := range notificationKinds {
		address := k.url(n.cfg)
		if address == "" {
			continue
		}
		forms = append(forms, address)
		u, parseErr := url.Parse(address)
		if parseErr != nil {
			continue
		}
		forms = append(forms, u.String(), u.Redacted(), u.RawQuery, u.EscapedPath(), u.Path, u.Fragment)
		for _, path := range []string{u.Path, u.EscapedPath()} {
			for _, segment := range strings.Split(path, "/") {
				if len(segment) >= 8 {
					forms = append(forms, segment)
				}
			}
		}
		for _, pair := range strings.Split(u.RawQuery, "&") {
			if _, value, ok := strings.Cut(pair, "="); ok {
				forms = append(forms, value)
			}
		}
		for _, values := range u.Query() {
			forms = append(forms, values...)
		}
		if u.User != nil {
			forms = append(forms, u.User.String(), u.User.Username())
			if password, ok := u.User.Password(); ok {
				forms = append(forms, password)
			}
		}
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, form := range forms {
		// A form of one character, such as the path "/", names nothing.
		if len(form) > 1 {
			err = notify.RedactError(err, form)
		}
	}
	return err
}

type notificationChannelJSON struct {
	Kind       string  `json:"kind"`
	Configured bool    `json:"configured"`
	Source     string  `json:"source"`
	Host       *string `json:"host"`
}

type notificationTestJSON struct {
	Delivered bool   `json:"delivered"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (a *API) registerNotificationChannelRoutes(r chi.Router) {
	r.Get("/api/v1/notification-channels", a.handleGetNotificationChannels)
	r.Post("/api/v1/notification-channels/{kind}/test", a.handleTestNotificationChannel)
}

// webhookHost is the host name of a webhook address, nil when there is no
// address or it does not parse to one. The parse error is dropped: it quotes
// the address.
func webhookHost(raw string) *string {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	if host == "" {
		return nil
	}
	return &host
}

func (a *API) handleGetNotificationChannels(w http.ResponseWriter, _ *http.Request) {
	out := make([]notificationChannelJSON, 0, len(notificationKinds))
	for _, k := range notificationKinds {
		address := k.url(a.notifications.cfg)
		out = append(out, notificationChannelJSON{
			Kind:       k.Kind,
			Configured: address != "",
			Source:     "env",
			Host:       webhookHost(address),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleTestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	k, ok := lookupNotificationKind(chi.URLParam(r, "kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown channel", "the channel kinds are slack, webhook and google_chat")
		return
	}
	address := k.url(a.notifications.cfg)
	channel := a.notifications.channels[k.Kind]
	// The server builds a channel for every configured address, so a missing
	// channel object only happens without SetNotificationChannels.
	if address == "" || channel == nil {
		writeError(w, http.StatusConflict, "channel not configured", "configure the channel in the server environment, then restart the server")
		return
	}
	running := a.notifications.running[k.Kind]
	if !running.CompareAndSwap(false, true) {
		writeError(w, http.StatusTooManyRequests, "test already running", "a test message of this channel is being sent; wait for its result")
		return
	}
	defer running.Store(false)
	timeout := a.notifications.timeout
	if timeout <= 0 {
		timeout = notificationTestTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	err := channel.Send(ctx, notificationTestMessage(time.Now().UTC()))
	result := notificationTestJSON{Delivered: true, Status: "delivered"}
	if err != nil {
		result.Delivered = false
		result.Status = "failed"
		// The channel's send reports a timeout of its last attempt as a
		// redacted text, without context.DeadlineExceeded in its chain, so
		// the expired test deadline counts too (N1).
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Status = "timeout"
		}
		result.Error = a.notifications.redact(err).Error()
	}
	if result.Error == "" {
		log.Printf("notifications: test of %s by %s: %s", k.Kind, requesterOf(r, a.auth), result.Status)
	} else {
		// Quoted: the error text comes from the remote end and must not
		// forge a log line.
		log.Printf("notifications: test of %s by %s: %s: %q", k.Kind, requesterOf(r, a.auth), result.Status, result.Error)
	}
	writeJSON(w, http.StatusOK, result)
}
