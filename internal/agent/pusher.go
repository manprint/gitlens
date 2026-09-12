package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
)

// maxPendingEnvelopes bounds the in-memory queue used when no disk buffer is
// configured. At the default 30s push interval this is just over eight hours
// of envelopes, comfortably longer than any outage the in-memory path is
// expected to ride out (the durable path is buffer.Buffer).
const maxPendingEnvelopes = 1024

// Health represents the pusher's health state.
type Health string

const (
	HealthOK           Health = "ok"
	HealthIncompatible Health = "incompatible"
	HealthRevoked      Health = "revoked"
	HealthUnauthorized Health = "unauthorized"
)

// Pusher sends wire.Envelopes to the server with retry backoff.
type Pusher struct {
	token              string
	serverURL          string
	clk                clock.Clock
	httpClient         *http.Client
	mu                 sync.Mutex
	health             Health
	lastSuccessfulPush *time.Time
	clockSkew          time.Duration
	lastError          string
	stopped            atomic.Bool

	pendingMu sync.Mutex
	pending   []*wire.Envelope
	nAcked    int64
	nDropped  int64

	// Drop-rate bookkeeping for the agent's own self-report metric, guarded
	// by mu.
	lastDropTotal int64
	lastDropAt    time.Time

	// buf, when set via SetBuffer, is used as the durable queue instead of
	// pending: Queue() persists to disk (so a bounded volume genuinely fills
	// and genuinely drops, I-7) and Push() drains it via Next()/AckFunc
	// instead of the in-memory slice.
	buf *buffer.Buffer
}

// SetBuffer wires a disk-backed buffer.Buffer as the pusher's durable queue.
// Must be called before the first Queue()/Push() to take effect; nil is a
// no-op (keeps the legacy in-memory pending slice).
func (p *Pusher) SetBuffer(buf *buffer.Buffer) {
	p.buf = buf
}

// NewPusher creates a new Pusher with default settings.
func NewPusher(serverURL, token string, clk clock.Clock) *Pusher {
	if clk == nil {
		clk = clock.System()
	}
	return &Pusher{
		token:     token,
		serverURL: serverURL,
		clk:       clk,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		health: HealthOK,
	}
}

// Queue queues an envelope for delivery. Does not block on network. When a
// buffer.Buffer is set (SetBuffer), a full disk buffer drops the envelope
// (counted in nDropped, I-7) instead of growing without bound.
func (p *Pusher) Queue(env *wire.Envelope) {
	if p.buf != nil {
		data, err := json.Marshal(env)
		if err != nil {
			atomic.AddInt64(&p.nDropped, 1)
			return
		}
		if err := p.buf.Append(data); err != nil {
			atomic.AddInt64(&p.nDropped, 1)
			return
		}
		return
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	// The in-memory queue is the fallback used when no disk buffer is
	// configured (tests, and the `check` subcommand). It had no bound at all:
	// a server answering 401 — a rotated token, an agent awaiting
	// re-approval — makes Push re-queue the head envelope and stop, forever,
	// while the collection loop keeps appending one envelope per push
	// interval. That is an unbounded heap growth ending in an OOM kill, on
	// exactly the outage the buffer exists to survive. Dropping the oldest
	// matches the disk buffer's own size-limit policy, and is counted the
	// same way so the drop is visible in pglens_samples_dropped_rate.
	if len(p.pending) >= maxPendingEnvelopes {
		drop := len(p.pending) - maxPendingEnvelopes + 1
		p.pending = append(p.pending[:0], p.pending[drop:]...)
		atomic.AddInt64(&p.nDropped, int64(drop))
	}
	p.pending = append(p.pending, env)
}

// Push attempts to deliver all queued envelopes. Called by scheduler or test.
// Returns error only if context is cancelled; delivery failures are logged internally.
func (p *Pusher) Push(ctx context.Context) error {
	if p.stopped.Load() {
		return nil
	}
	if p.buf != nil {
		return p.pushFromBuffer(ctx)
	}

	for {
		p.pendingMu.Lock()
		if len(p.pending) == 0 {
			p.pendingMu.Unlock()
			return nil
		}
		env := p.pending[0]
		p.pending = p.pending[1:]
		p.pendingMu.Unlock()

		outcome, err := p.pushOne(ctx, env)
		if outcome == pushRetryLater {
			// Context cancelled, or a failure a later push may resolve
			// (401): re-queue at the head so ordering is preserved and
			// nothing is lost, then stop draining.
			p.pendingMu.Lock()
			p.pending = append([]*wire.Envelope{env}, p.pending...)
			p.pendingMu.Unlock()
			return err
		}
		if err != nil {
			// Consumed and unusable: dropping it is the point, so it must not
			// go back on the queue to be retried forever.
			return err
		}
	}
}

// pushFromBuffer drains the disk buffer. An envelope is only acked once it is
// consumed — accepted by the server, or rejected in a way no retry can fix:
// pushOne itself retries with backoff until one of those happens or ctx is
// cancelled, so on cancellation the unacked record is simply re-read from the
// last acked checkpoint on the next Push() (or after a restart).
//
// A pushRetryLater outcome (401) stops the drain and leaves the record
// unacked. Acking it, as this used to, made an authentication outage
// destructive: a rotated token or an agent awaiting re-approval answers 401
// to every push, and the loop walked the entire buffer acking each record in
// turn — permanently deleting hours of buffered metrics that the agent had
// written to disk precisely so they would survive an outage.
func (p *Pusher) pushFromBuffer(ctx context.Context) error {
	for {
		data, ack, err := p.buf.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		var env wire.Envelope
		if jsonErr := json.Unmarshal(data, &env); jsonErr != nil {
			// Corrupt/undecodable record: nothing to retry, skip it.
			_ = ack(ctx)
			continue
		}
		outcome, err := p.pushOne(ctx, &env)
		if outcome == pushRetryLater {
			// Hand the unacked record back to the buffer: Next() has already
			// advanced its read cursor past it, so without the rewind it
			// would never be offered again in this process.
			p.buf.Rewind()
			return err
		}
		_ = ack(ctx)
		if err != nil {
			return err
		}
	}
}

// pushOutcome tells the caller what to do with the envelope it handed to
// pushOne.
type pushOutcome int

const (
	// pushConsumed: the envelope must not be presented again. Either the
	// server accepted it (202), or it rejected it in a way no retry can fix
	// (400 protocol mismatch, 413 too large, an unexpected 4xx) — keeping
	// those queued would wedge the whole queue behind a record the server is
	// never going to take.
	pushConsumed pushOutcome = iota
	// pushRetryLater: delivery failed for a reason a later push may resolve
	// (401 — token rotated, agent pending re-approval). The envelope must
	// stay queued and the drain must stop.
	pushRetryLater
)

// pushOne attempts to deliver one envelope, retrying transient failures with
// exponential backoff + jitter until the envelope reaches a terminal outcome
// or ctx is cancelled.
func (p *Pusher) pushOne(ctx context.Context, env *wire.Envelope) (pushOutcome, error) {
	for attempt := 0; ; attempt++ {
		// Backoff applies to retries only. Sleeping before the first attempt
		// delayed every push by backoffDelay(1) — 1 to 2 seconds — even
		// against a perfectly healthy server, and made the flush at shutdown
		// (and any test with a short deadline) race its own timeout.
		if attempt > 0 {
			if err := p.clk.Sleep(ctx, backoffDelay(attempt, p.clk)); err != nil {
				return pushRetryLater, err // context cancelled
			}
		}

		req, err := p.buildRequest(ctx, env)
		if err != nil {
			// An envelope that cannot be encoded will never encode: keeping
			// it queued would wedge the queue behind it forever (the disk
			// buffer would re-offer the same record on every push), so it is
			// dropped and counted.
			atomic.AddInt64(&p.nDropped, 1)
			return pushConsumed, fmt.Errorf("encode envelope: %w", err)
		}

		status, skew, body, err := p.doRequest(req)
		if err != nil {
			// Transient: network error, connection refused, timeout
			// Continue retry loop
			continue
		}

		// Handle status code
		switch status {
		case http.StatusAccepted: // 202
			p.mu.Lock()
			now := p.clk.Now()
			p.lastSuccessfulPush = &now
			p.health = HealthOK
			p.mu.Unlock()
			atomic.AddInt64(&p.nAcked, 1)
			if skew != nil {
				p.mu.Lock()
				p.clockSkew = *skew
				p.mu.Unlock()
			}
			return pushConsumed, nil

		case http.StatusBadRequest: // 400
			p.mu.Lock()
			p.health = HealthIncompatible
			p.lastError = body
			p.mu.Unlock()
			return pushConsumed, nil // stop retry

		case http.StatusUnauthorized: // 401
			p.mu.Lock()
			// internal/server/ingest.go's revocation check returns a body
			// containing "revoked" specifically so this can tell "the
			// server has flagged this agent" apart from "this token was
			// never valid" — otherwise indistinguishable at the HTTP layer,
			// both being a bare 401.
			if strings.Contains(body, "revoked") {
				p.health = HealthRevoked
			} else {
				p.health = HealthUnauthorized
			}
			p.lastError = body
			p.mu.Unlock()
			return pushRetryLater, nil // stop retry, process stays alive

		case http.StatusRequestEntityTooLarge: // 413
			// Drop this envelope
			atomic.AddInt64(&p.nDropped, 1)
			return pushConsumed, nil

		default:
			if status >= 500 {
				// 5xx: retry
				continue
			}
			// Unknown status: treat as error, don't retry forever
			return pushConsumed, nil
		}
	}
}

// backoffDelay returns exponential backoff + jitter, capped at 60s.
func backoffDelay(attempt int, clk clock.Clock) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// Exponential: 1, 2, 4, 8, 16, 32, 60, 60, 60, ...
	base := time.Duration(math.Min(math.Pow(2, float64(attempt-1)), 60)) * time.Second
	// Jitter: add random [0, base]
	jitterMax := int64(base)
	jitter := time.Duration(rand.Int63n(jitterMax + 1))
	return base + jitter
}

// buildRequest constructs an HTTP request for the envelope.
//
// The request carries ctx: http.NewRequest (no context) produced a request
// that the http.Client would only ever abandon on its own 10s timeout, so a
// cancelled push context — the agent shutting down, or the scheduler's
// deadline expiring — did not actually stop the in-flight POST. Shutdown
// stalled for up to ten seconds per queued envelope, and a cancellation that
// the caller believed had taken effect kept writing to a server that was
// about to be told the agent had stopped.
func (p *Pusher) buildRequest(ctx context.Context, env *wire.Envelope) (*http.Request, error) {
	body := bytes.NewBuffer(nil)
	gz := gzip.NewWriter(body)
	if err := json.NewEncoder(gz).Encode(env); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.serverURL+"/api/v1/push", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	return req, nil
}

// doRequest sends the request and returns (status, skew, error).
// error is non-nil only for network errors; status is parsed from response when err is nil.
func (p *Pusher) doRequest(req *http.Request) (int, *time.Duration, string, error) {
	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Network error, timeout, connection refused: return error
		return 0, nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var skew *time.Duration
	if dateStr := resp.Header.Get("Date"); dateStr != "" {
		if serverTime, err := http.ParseTime(dateStr); err == nil {
			skewVal := serverTime.Sub(p.clk.Now())
			skew = &skewVal
		}
	}

	// Consumed here (rather than discarded) so a rejection's reason is
	// available to LastError() — a 400/401 used to leave no trace anywhere
	// of WHY the agent went "incompatible"/"unauthorized", not even in logs.
	bodyBytes, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, skew, string(bodyBytes), nil
}

// Health returns current health state.
func (p *Pusher) HealthState() Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.health
}

// LastError returns the server's response body from the most recent
// rejected push (400/401), or "" if there hasn't been one.
func (p *Pusher) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastError
}

// LastSuccessfulPush returns when the last successful push occurred.
func (p *Pusher) LastSuccessfulPush() *time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSuccessfulPush
}

// ClockSkew returns the measured clock skew with the server.
func (p *Pusher) ClockSkew() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clockSkew
}

// BufferStats returns counts of acked and dropped envelopes.
func (p *Pusher) BufferStats() (acked, dropped int64) {
	return atomic.LoadInt64(&p.nAcked), atomic.LoadInt64(&p.nDropped)
}

// DroppedSamples returns the cumulative number of samples this agent has
// lost: envelopes Queue() could not persist, plus every record the disk
// buffer rolled off under its size and age retention policies.
func (p *Pusher) DroppedSamples() int64 {
	total := atomic.LoadInt64(&p.nDropped)
	if p.buf != nil {
		for _, n := range p.buf.Stats().SamplesDropped {
			total += n
		}
	}
	return total
}

// SamplesDroppedRate returns drops per second since the previous call, and is
// what makes the agent_buffer_full alert rule reachable: the rule compares the
// pglens_samples_dropped_rate metric, which no check produces and which the
// agent is the only party able to observe. The first call establishes the
// baseline and reports 0.
//
// Call it once per push cycle; each call consumes the interval since the last
// one.
func (p *Pusher) SamplesDroppedRate(now time.Time) float64 {
	total := p.DroppedSamples()

	p.mu.Lock()
	defer p.mu.Unlock()
	last, lastAt := p.lastDropTotal, p.lastDropAt
	p.lastDropTotal, p.lastDropAt = total, now
	if lastAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(lastAt).Seconds()
	if elapsed <= 0 || total <= last {
		return 0
	}
	return float64(total-last) / elapsed
}

// Stop signals the pusher to stop.
func (p *Pusher) Stop() {
	p.stopped.Store(true)
}

// HealthzHandler returns an HTTP handler for /healthz.
func (p *Pusher) HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		health := p.health
		lastPush := p.lastSuccessfulPush
		skew := p.clockSkew
		lastErr := p.lastError
		p.mu.Unlock()

		code := http.StatusOK
		if health == HealthIncompatible || health == HealthRevoked || health == HealthUnauthorized {
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)

		lastPushStr := "null"
		if lastPush != nil {
			b, _ := lastPush.MarshalJSON()
			lastPushStr = string(b)
		}

		lastErrJSON, _ := json.Marshal(lastErr)
		acked, dropped := p.BufferStats()
		_, _ = fmt.Fprintf(w, `{"state":"%s","last_successful_push":%s,"last_error":%s,"buffer_stats":{"acked":%d,"dropped":%d},"clock_skew_seconds":%d}`,
			health, lastPushStr, lastErrJSON, acked, dropped, int64(skew.Seconds()))
	}
}
