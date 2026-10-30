package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/manprint/pglens/internal/alert/notify"
)

// Notification channels (11.5a, 11.5b, D18). The console reads which delivery
// channels are configured, by kind and host only, and can send a test message
// through each one. An address comes from the server environment or is stored
// from the console in notification_channels (Q2); a stored address overrides
// the environment, and removing it falls back to the environment again. The
// address is write-only: it is never returned, rendered or logged (I-10).
//
// The alert notifier and the test send deliver through one notify.Slot per
// kind. A change from the console is written to the database first and then
// swapped into the slot, so the next delivery uses it without a restart. Only
// this process swaps its slots: a second server replica on the same database
// picks the change up at its next start.

// notificationKind is one delivery channel the console lists.
type notificationKind struct {
	// Kind is the channel's notify.Channel Name().
	Kind string
	// url reads the channel's webhook address from the alert configuration.
	url func(AlertConfig) string
	// build makes the channel of an address, as the alert path does.
	build func(address string) notify.Channel
}

// notificationKinds is the whitelist of the kinds, in the order the API lists
// them (N5).
var notificationKinds = []notificationKind{
	{
		Kind:  "slack",
		url:   func(c AlertConfig) string { return c.SlackURL },
		build: func(address string) notify.Channel { return notify.NewSlack(address, notificationHTTPClient) },
	},
	{
		Kind:  "webhook",
		url:   func(c AlertConfig) string { return c.WebhookURL },
		build: func(address string) notify.Channel { return notify.NewWebhook(address, notificationHTTPClient) },
	},
	{
		Kind:  "google_chat",
		url:   func(c AlertConfig) string { return c.GoogleChatURL },
		build: func(address string) notify.Channel { return notify.NewGoogleChat(address, notificationHTTPClient) },
	},
}

// notificationHTTPClient is the HTTP client of every notification channel, from
// the environment or the console. It never follows a redirect: an address is
// checked when it is stored (https, or http only for a private host), and a
// redirect would post to a host that was never checked. A redirect answer is a
// failed delivery. The timeouts are the channel's own, on the request context,
// as with the default client.
var notificationHTTPClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// NewNotificationChannel builds the delivery channel of kind for address, with
// the constructors and HTTP client the alert path uses; nil for an unknown
// kind. A channel stored from the console is built the same way.
func NewNotificationChannel(kind, address string) notify.Channel {
	k, ok := lookupNotificationKind(kind)
	if !ok {
		return nil
	}
	return k.build(address)
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

// notificationChannels is the delivery configuration the console reads and
// changes: the environment's addresses and channels, the addresses stored from
// the console, and one slot per kind that the alert notifier and the test send
// both deliver through.
type notificationChannels struct {
	cfg AlertConfig
	// env is the channel of each kind the environment configures; removing a
	// stored address puts it back in the slot.
	env map[string]notify.Channel
	// slots holds the channel each kind delivers through now, nil inside the
	// slot when the kind is not configured.
	slots map[string]*notify.Slot
	// timeout replaces notificationTestTimeout in unit tests; zero means the
	// default.
	timeout time.Duration
	// running holds one flag per kind: a second test send of a kind while
	// one is in flight is refused (429), so a burst of clicks or requests
	// cannot flood a chat space.
	running map[string]*atomic.Bool

	// change serialises the changes from the console, so the slots end up in
	// the order of the database writes.
	change sync.Mutex
	// mu guards stored and replaced.
	mu sync.Mutex
	// stored is the address of each kind stored from the console.
	stored map[string]string
	// envAddresses is every address of the environment.
	envAddresses []string
	// replaced is the addresses stored from the console and since replaced or
	// removed, the newest last, at most maxReplacedAddresses. With the
	// environment's and the stored ones, each is removed from every delivery
	// error (D4).
	replaced []string
}

// maxReplacedAddresses bounds the replaced addresses kept for the redaction, so
// a long run of changes grows neither the set nor the work of each redaction
// without limit.
const maxReplacedAddresses = 32

// minRedactedPart is the shortest part of an address (its path or a path
// segment, its query or a query value) that redact removes on its own: a
// shorter one names nothing secret and would match ordinary text.
const minRedactedPart = 8

// newNotificationChannels is the configuration of the environment cfg, whose
// channel objects are channels; nothing is stored from the console yet.
func newNotificationChannels(cfg AlertConfig, channels []notify.Channel) *notificationChannels {
	byKind := make(map[string]notify.Channel, len(channels))
	for _, c := range channels {
		byKind[c.Name()] = c
	}
	n := &notificationChannels{
		cfg:     cfg,
		env:     map[string]notify.Channel{},
		slots:   make(map[string]*notify.Slot, len(notificationKinds)),
		running: make(map[string]*atomic.Bool, len(notificationKinds)),
		stored:  map[string]string{},
	}
	for _, k := range notificationKinds {
		n.running[k.Kind] = &atomic.Bool{}
		var initial notify.Channel
		if address := k.url(cfg); address != "" {
			n.envAddresses = append(n.envAddresses, address)
			if c := byKind[k.Kind]; c != nil {
				n.env[k.Kind] = c
				initial = n.redacting(c)
			}
		}
		n.slots[k.Kind] = notify.NewSlot(k.Kind, initial)
	}
	return n
}

// SetNotificationChannels gives the API the alert configuration and the
// channel objects built from it, so a test send from the console goes through
// the channels the alerts are delivered through (N3). Without it every channel
// reads as not configured. It is called before the server starts serving,
// then LoadNotificationChannels applies the addresses stored from the console,
// and NotificationChannels hands the slots to the alert notifier.
func (a *API) SetNotificationChannels(cfg AlertConfig, channels []notify.Channel) {
	a.notifications = newNotificationChannels(cfg, channels)
}

// NotificationChannels is the slot of every kind, in the order the API lists
// them. The alert notifier delivers through them, so a change from the console
// reaches the next alert; an empty slot is skipped.
func (a *API) NotificationChannels() []notify.Channel {
	n := a.notificationState()
	out := make([]notify.Channel, 0, len(notificationKinds))
	for _, k := range notificationKinds {
		out = append(out, n.slots[k.Kind])
	}
	return out
}

// LoadNotificationChannels applies the addresses stored from the console,
// which override the environment. Without a database nothing is stored. A
// stored address that is not valid any more is ignored and logged by kind.
func (a *API) LoadNotificationChannels(ctx context.Context) error {
	st := a.notificationChannelStore()
	if st == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stored, err := st.NotificationChannels(ctx)
	if err != nil {
		return err
	}
	n := a.notificationState()
	n.change.Lock()
	defer n.change.Unlock()
	for _, k := range notificationKinds {
		address, ok := stored[k.Kind]
		if !ok {
			continue
		}
		if detail := validateWebhookURL(address); detail != "" {
			log.Printf("notifications: ignoring the URL of %s stored from the console: %s", k.Kind, detail)
			continue
		}
		n.apply(k, address)
		log.Printf("notifications: notification channel %s uses the URL stored from the console; host %s", k.Kind, hostForLog(address))
	}
	return nil
}

// notificationState is the API's configuration. An API built without NewAPI
// has none: every channel reads as not configured.
func (a *API) notificationState() *notificationChannels {
	if a.notifications == nil {
		return newNotificationChannels(AlertConfig{}, nil)
	}
	return a.notifications
}

// apply stores address as the kind's address from the console and swaps its
// channel into the slot. The address joins the redaction set first, so a send
// on the new channel never reports it. The caller holds n.change.
func (n *notificationChannels) apply(k notificationKind, address string) {
	n.mu.Lock()
	if previous, ok := n.stored[k.Kind]; ok && previous != address {
		n.retire(previous)
	}
	n.stored[k.Kind] = address
	n.mu.Unlock()
	n.slots[k.Kind].Store(n.redacting(k.build(address)))
}

// unapply forgets the kind's stored address and puts the environment's channel
// back in the slot, or empties it. The caller holds n.change.
func (n *notificationChannels) unapply(k notificationKind) {
	n.mu.Lock()
	if previous, ok := n.stored[k.Kind]; ok {
		n.retire(previous)
	}
	delete(n.stored, k.Kind)
	n.mu.Unlock()
	n.slots[k.Kind].Store(n.redacting(n.env[k.Kind]))
}

// retire keeps address, no longer stored, among the replaced addresses, of
// which the newest maxReplacedAddresses stay. The caller holds n.mu.
func (n *notificationChannels) retire(address string) {
	n.replaced = slices.DeleteFunc(n.replaced, func(a string) bool { return a == address })
	n.replaced = append(n.replaced, address)
	if over := len(n.replaced) - maxReplacedAddresses; over > 0 {
		n.replaced = slices.Delete(n.replaced, 0, over)
	}
}

// redactingChannel is a delivery channel whose errors pass through the
// redaction of every address configured in this process. The alert path logs
// a failed delivery and records its error in notifications, so neither holds
// an address or its secret parts either (D4).
type redactingChannel struct {
	notify.Channel
	n *notificationChannels
}

func (c redactingChannel) Send(ctx context.Context, m notify.Message) error {
	return c.n.redact(c.Channel.Send(ctx, m))
}

// redacting wraps c in a redactingChannel; nil stays nil, an empty slot.
func (n *notificationChannels) redacting(c notify.Channel) notify.Channel {
	if c == nil {
		return nil
	}
	return redactingChannel{Channel: c, n: n}
}

// redact passes err through notify.RedactError for every address of the
// environment, every address stored from the console, the latest replaced ones
// (maxReplacedAddresses) and extra (an address being stored), not only the
// channel's own, and for the other forms an address is quoted in: net/http
// quotes it as URL.Redacted (a password replaced), and a part of it identifies
// the webhook too: its path and each path segment, its query and each query
// value, raw and decoded, of minRedactedPart characters or more, and its user
// information (I-10).
// The longest forms go first, so a shorter part never breaks the match of a
// longer one.
func (n *notificationChannels) redact(err error, extra ...string) error {
	if err == nil {
		return nil
	}
	n.mu.Lock()
	addresses := slices.Concat(n.envAddresses, n.replaced, extra)
	for _, address := range n.stored {
		addresses = append(addresses, address)
	}
	n.mu.Unlock()
	forms := []string{}
	part := func(p string) {
		if len(p) >= minRedactedPart {
			forms = append(forms, p)
		}
	}
	for _, address := range addresses {
		if address == "" {
			continue
		}
		forms = append(forms, address)
		u, parseErr := url.Parse(address)
		if parseErr != nil {
			continue
		}
		forms = append(forms, u.String(), u.Redacted())
		for _, p := range []string{u.RawQuery, u.EscapedPath(), u.Path, u.Fragment} {
			part(p)
		}
		for _, path := range []string{u.Path, u.EscapedPath()} {
			for _, segment := range strings.Split(path, "/") {
				part(segment)
			}
		}
		for _, pair := range strings.Split(u.RawQuery, "&") {
			if _, value, ok := strings.Cut(pair, "="); ok {
				part(value)
			}
		}
		for _, values := range u.Query() {
			for _, value := range values {
				part(value)
			}
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

// row is the status of a kind: the stored address when there is one, else the
// environment's.
func (n *notificationChannels) row(k notificationKind) notificationChannelJSON {
	n.mu.Lock()
	stored, ok := n.stored[k.Kind]
	n.mu.Unlock()
	if ok {
		return notificationChannelJSON{Kind: k.Kind, Configured: true, Source: "ui", Host: webhookHost(stored)}
	}
	address := k.url(n.cfg)
	return notificationChannelJSON{Kind: k.Kind, Configured: address != "", Source: "env", Host: webhookHost(address)}
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
	r.Put("/api/v1/notification-channels/{kind}", a.handleSetNotificationChannel)
	r.Delete("/api/v1/notification-channels/{kind}", a.handleDeleteNotificationChannel)
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

// hostForLog is the host of an address for a log line, "none" without one.
func hostForLog(address string) string {
	if host := webhookHost(address); host != nil {
		return *host
	}
	return "none"
}

func (a *API) handleGetNotificationChannels(w http.ResponseWriter, _ *http.Request) {
	n := a.notificationState()
	out := make([]notificationChannelJSON, 0, len(notificationKinds))
	for _, k := range notificationKinds {
		out = append(out, n.row(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleTestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	k, ok := lookupNotificationKind(chi.URLParam(r, "kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown channel", "the channel kinds are slack, webhook and google_chat")
		return
	}
	n := a.notificationState()
	// The server builds a channel for every configured address, so an empty
	// slot means the kind is not configured.
	channel := n.slots[k.Kind].Channel()
	if channel == nil {
		writeError(w, http.StatusConflict, "channel not configured", "set the channel's URL from the console, or in the server environment and restart the server")
		return
	}
	running := n.running[k.Kind]
	if !running.CompareAndSwap(false, true) {
		writeError(w, http.StatusTooManyRequests, "test already running", "a test message of this channel is being sent; wait for its result")
		return
	}
	defer running.Store(false)
	timeout := n.timeout
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
		result.Error = n.redact(err).Error()
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

// notificationChannelStore is the database side of the addresses stored from
// the console. Unit tests stub it.
type notificationChannelStore interface {
	// NotificationChannels answers the stored addresses, kind to address.
	NotificationChannels(ctx context.Context) (map[string]string, error)
	// SetNotificationChannel stores the address of kind, set by updatedBy.
	SetNotificationChannel(ctx context.Context, kind, address, updatedBy string) error
	// DeleteNotificationChannel removes the stored address of kind, if any.
	DeleteNotificationChannel(ctx context.Context, kind string) error
}

// The SQL of the notification channel store. Every value is a bound parameter.
const (
	notificationChannelsSQL      = `SELECT kind, url FROM notification_channels WHERE tenant_id = 'default'`
	notificationChannelUpsertSQL = `INSERT INTO notification_channels (tenant_id, kind, url, updated_at, updated_by) VALUES ('default', $1, $2, now(), $3)
		ON CONFLICT (tenant_id, kind) DO UPDATE SET url = EXCLUDED.url, updated_at = now(), updated_by = EXCLUDED.updated_by`
	notificationChannelDeleteSQL = `DELETE FROM notification_channels WHERE tenant_id = 'default' AND kind = $1`
)

// pgNotificationChannelStore is the notificationChannelStore of a database.
type pgNotificationChannelStore struct {
	pool dbPool
}

func (s pgNotificationChannelStore) NotificationChannels(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, notificationChannelsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var kind, address string
		if err := rows.Scan(&kind, &address); err != nil {
			return nil, err
		}
		out[kind] = address
	}
	return out, rows.Err()
}

func (s pgNotificationChannelStore) SetNotificationChannel(ctx context.Context, kind, address, updatedBy string) error {
	_, err := s.pool.Exec(ctx, notificationChannelUpsertSQL, kind, address, updatedBy)
	return err
}

func (s pgNotificationChannelStore) DeleteNotificationChannel(ctx context.Context, kind string) error {
	_, err := s.pool.Exec(ctx, notificationChannelDeleteSQL, kind)
	return err
}

// notificationChannelStore is the store of the API: the stub of a test, else
// the database, else nil (no store).
func (a *API) notificationChannelStore() notificationChannelStore {
	if a.notificationStore != nil {
		return a.notificationStore
	}
	if a.pool == nil {
		return nil
	}
	return pgNotificationChannelStore{pool: a.pool}
}

// maxWebhookURLLength bounds an address stored from the console.
const maxWebhookURLLength = 2048

// validateWebhookURL checks an address set from the console and answers why it
// is refused, or "" when it is accepted. The answer never quotes the address,
// nor the error of its parse, which does.
func validateWebhookURL(raw string) string {
	switch {
	case raw == "":
		return "url is required"
	case len(raw) > maxWebhookURLLength:
		return "the URL may not be longer than 2048 characters"
	case strings.ContainsAny(raw, " \t\r\n"):
		return "the URL may not contain spaces, tabs or line breaks"
	case strings.Contains(raw, "#"):
		return "the URL may not have a fragment (#)"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "the URL does not parse"
	}
	if u.Host == "" || u.Hostname() == "" {
		return "the URL names no host"
	}
	switch u.Scheme {
	case "https":
		return ""
	case "http":
		if privateHost(u.Hostname()) {
			return ""
		}
		return "http is accepted only for a loopback or private address; use https"
	default:
		return "the scheme must be https, or http for a loopback or private address"
	}
}

// privateHost reports whether host is localhost or a literal loopback or
// private address: 127.0.0.0/8, ::1, 10.0.0.0/8, 172.16.0.0/12,
// 192.168.0.0/16 or fc00::/7. A host name other than localhost is not
// resolved, so it is not private.
func privateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

type notificationChannelBody struct {
	URL *string `json:"url"`
}

// handleSetNotificationChannel stores the address of a kind from the console
// and swaps the kind's channel to it once the database has it. The answer is
// the kind's status row; the address is never echoed, in an error either.
func (a *API) handleSetNotificationChannel(w http.ResponseWriter, r *http.Request) {
	k, ok := lookupNotificationKind(chi.URLParam(r, "kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown channel", "the channel kinds are slack, webhook and google_chat")
		return
	}
	var in notificationChannelBody
	if !decodeStrictJSONBody(w, r, &in) {
		return
	}
	if in.URL == nil {
		writeError(w, http.StatusBadRequest, "invalid url", "url is required and must be a string")
		return
	}
	address := *in.URL
	if detail := validateWebhookURL(address); detail != "" {
		writeError(w, http.StatusBadRequest, "invalid url", detail)
		return
	}
	st := a.notificationChannelStore()
	if st == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	n := a.notificationState()
	requester := requesterOf(r, a.auth)
	// Not cancelled with the request: a client that goes away while the write
	// commits must not leave the slot behind the database.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	n.change.Lock()
	err := st.SetNotificationChannel(ctx, k.Kind, address, requester)
	if err == nil {
		n.apply(k, address)
	}
	n.change.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", n.redact(err, address).Error())
		return
	}
	log.Printf("notifications: notification channel %s set from the console by %s; host %s", k.Kind, requester, hostForLog(address))
	writeJSON(w, http.StatusOK, n.row(k))
}

// handleDeleteNotificationChannel removes the address stored from the console,
// so the kind goes back to the environment's address, or to not configured.
// Removing an address that is not stored changes nothing and is not an error.
func (a *API) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request) {
	k, ok := lookupNotificationKind(chi.URLParam(r, "kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown channel", "the channel kinds are slack, webhook and google_chat")
		return
	}
	st := a.notificationChannelStore()
	if st == nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", "")
		return
	}
	n := a.notificationState()
	// Not cancelled with the request, as in handleSetNotificationChannel.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	n.change.Lock()
	err := st.DeleteNotificationChannel(ctx, k.Kind)
	if err == nil {
		n.unapply(k)
	}
	n.change.Unlock()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store unavailable", n.redact(err).Error())
		return
	}
	row := n.row(k)
	fallback := "the environment's URL"
	if !row.Configured {
		fallback = "not configured"
	}
	log.Printf("notifications: notification channel %s removed from the console by %s; now %s", k.Kind, requesterOf(r, a.auth), fallback)
	writeJSON(w, http.StatusOK, row)
}
