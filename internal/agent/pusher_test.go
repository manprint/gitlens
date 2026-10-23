package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestPusher_BackoffCapped(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))

	for attempt := 1; attempt <= 20; attempt++ {
		delay := backoffDelay(attempt, clk)
		require.LessOrEqual(t, delay, 120*time.Second, "attempt %d exceeded cap", attempt)
	}
}

func TestPusher_NeverBlocksCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server never responds - would timeout
		select {}
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())

	envelopes := make([]*wire.Envelope, 100)
	for i := 0; i < 100; i++ {
		envelopes[i] = &wire.Envelope{
			ProtocolVersion: wire.ProtocolVersion,
			AgentID:         "test",
		}
	}

	start := time.Now()
	for i := 0; i < 100; i++ {
		p.Queue(envelopes[i])
	}
	elapsed := time.Since(start)
	require.Less(t, elapsed, 100*time.Millisecond, "queuing should be non-blocking")

	p.pendingMu.Lock()
	require.Equal(t, 100, len(p.pending), "expected all 100 queued")
	p.pendingMu.Unlock()
}

func TestPusher_AuthorizationHeader(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "secret-token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, "Bearer secret-token", authHeader)
}

func TestPusher_GzipEncoding(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "test-agent"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)

	gz, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = gz.Close() }()

	var decoded wire.Envelope
	err = json.NewDecoder(gz).Decode(&decoded)
	require.NoError(t, err)
	require.Equal(t, "test-agent", decoded.AgentID)
}

func TestPusher_Accepts202(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, HealthOK, p.HealthState())
	require.NotNil(t, p.LastSuccessfulPush())
}

func TestPusher_StopsOn400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"unsupported protocol_version"}`))
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, HealthIncompatible, p.HealthState())
	// LastError surfaces the server's rejection reason — before this, a
	// 400/401 left no trace anywhere of why (see internal/agent/pusher.go).
	require.Contains(t, p.LastError(), "unsupported protocol_version")
}

func TestPusher_StopsOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, HealthUnauthorized, p.HealthState())
}

func TestPusher_StopsOn401Revoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"revoked"}`))
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	require.Equal(t, HealthRevoked, p.HealthState())
}

func TestPusher_Drops413(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(1), dropped)
	require.Equal(t, int64(0), acked)
}

func TestPusher_ClockSkew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverTime := time.Now().Add(1 * time.Hour)
		w.Header().Set("Date", serverTime.Format(http.TimeFormat))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	env := &wire.Envelope{ProtocolVersion: wire.ProtocolVersion}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.Queue(env)
	err := p.Push(ctx)
	require.NoError(t, err)

	skew := p.ClockSkew()
	require.Greater(t, skew, 50*time.Minute, "expected large positive skew (server ahead)")
}

func TestPusher_HealthzHandler(t *testing.T) {
	p := NewPusher("http://localhost", "token", clock.System())
	now := clock.System().Now()
	p.lastSuccessfulPush = &now

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	p.HealthzHandler()(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	require.Equal(t, "ok", result["state"])
	require.NotNil(t, result["last_successful_push"])
}

func TestPusher_HealthzHandlerUnhealthy(t *testing.T) {
	p := NewPusher("http://localhost", "token", clock.System())
	p.mu.Lock()
	p.health = HealthRevoked
	p.mu.Unlock()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	p.HealthzHandler()(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestPusher_SetBuffer_QueueAndPushRoundTrip(t *testing.T) {
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf, err := buffer.Open(t.TempDir(), buffer.Options{})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	p := NewPusher(srv.URL, "token", clock.System())
	p.SetBuffer(buf)

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "a"})
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "b"})

	// SetBuffer routes Queue through the disk buffer, not the legacy
	// in-memory slice.
	p.pendingMu.Lock()
	require.Empty(t, p.pending)
	p.pendingMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, 2, received)
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(2), acked)
	require.Equal(t, int64(0), dropped)
}

func TestPusher_SetBuffer_AppendFailureDropsAndCounts(t *testing.T) {
	dir := t.TempDir()
	buf, err := buffer.Open(dir, buffer.Options{})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	// Real ENOSPC on a real tmpfs is exercised live by SYS-AGENT-003; here
	// only Pusher's reaction to Append() failing (for any reason) is under
	// test, so a read-only directory (blocking the first segment's
	// os.Create) is a simpler, deterministic stand-in.
	require.NoError(t, os.Chmod(dir, 0o555))
	defer func() { _ = os.Chmod(dir, 0o755) }() // let t.TempDir() clean up

	p := NewPusher("http://unused.invalid", "token", clock.System())
	p.SetBuffer(buf)

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})

	_, dropped := p.BufferStats()
	require.Equal(t, int64(1), dropped, "Queue must count a buffer Append failure as dropped, not silently lose it")
}

func TestPusher_SetBuffer_SkipsUndecodableRecord(t *testing.T) {
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf, err := buffer.Open(t.TempDir(), buffer.Options{})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	require.NoError(t, buf.Append([]byte("not valid json")))
	require.NoError(t, buf.Append([]byte(`{"protocol_version":1,"agent_id":"a"}`)))

	p := NewPusher(srv.URL, "token", clock.System())
	p.SetBuffer(buf)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, 1, received, "the corrupt record must be skipped, not block the valid one behind it")
}

// TestPusher_FirstAttemptIsNotDelayed pins the fix for a backoff that was
// applied *before* the first attempt: every push, against a healthy server,
// waited backoffDelay(1) = 1-2s before even opening the connection.
func TestPusher_FirstAttemptIsNotDelayed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	require.NoError(t, p.Push(ctx))
	require.Less(t, time.Since(start), 500*time.Millisecond, "the first attempt must go out immediately")
	require.Equal(t, HealthOK, p.HealthState())
}

// TestPusher_SetBuffer_401KeepsBufferedEnvelopes covers the destructive
// failure mode of an auth outage: the drain used to ack every record the
// server answered 401 to, so a rotated token or an agent awaiting
// re-approval permanently deleted the entire disk buffer — exactly the data
// the buffer exists to protect. The drain must stop at the first 401, keep
// the records, and deliver them once the token is accepted again.
func TestPusher_SetBuffer_401KeepsBufferedEnvelopes(t *testing.T) {
	var mu sync.Mutex
	unauthorized := true
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gz, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		var env wire.Envelope
		require.NoError(t, json.NewDecoder(gz).Decode(&env))

		mu.Lock()
		defer mu.Unlock()
		received = append(received, env.AgentID)
		if unauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf, err := buffer.Open(t.TempDir(), buffer.Options{})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	p := NewPusher(srv.URL, "token", clock.System())
	p.SetBuffer(buf)
	for _, id := range []string{"a", "b", "c"} {
		p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: id})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, p.Push(ctx))
	require.Equal(t, HealthUnauthorized, p.HealthState())
	mu.Lock()
	require.Equal(t, []string{"a"}, received, "the drain must stop at the first 401 instead of walking the buffer")
	mu.Unlock()
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(0), acked)
	require.Equal(t, int64(0), dropped, "a 401 is not a drop")

	// The token is accepted again: nothing may have been lost.
	mu.Lock()
	unauthorized = false
	mu.Unlock()
	require.NoError(t, p.Push(ctx))

	mu.Lock()
	require.Equal(t, []string{"a", "a", "b", "c"}, received, "every buffered envelope must survive the auth outage")
	mu.Unlock()
	acked, dropped = p.BufferStats()
	require.Equal(t, int64(3), acked)
	require.Equal(t, int64(0), dropped)
	require.Equal(t, HealthOK, p.HealthState(), "a successful push must clear the unauthorized state")
}

// TestPusher_SetBuffer_400DropsOnlyTheRejectedEnvelope is the counterpart:
// a permanently rejected record must be acked so it cannot wedge the queue,
// and the records behind it must still go out.
func TestPusher_SetBuffer_400DropsOnlyTheRejectedEnvelope(t *testing.T) {
	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gz, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		var env wire.Envelope
		require.NoError(t, json.NewDecoder(gz).Decode(&env))

		mu.Lock()
		received = append(received, env.AgentID)
		mu.Unlock()

		if env.AgentID == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unsupported protocol_version"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf, err := buffer.Open(t.TempDir(), buffer.Options{})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	p := NewPusher(srv.URL, "token", clock.System())
	p.SetBuffer(buf)
	for _, id := range []string{"bad", "good"} {
		p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: id})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	mu.Lock()
	require.Equal(t, []string{"bad", "good"}, received)
	mu.Unlock()
	acked, _ := p.BufferStats()
	require.Equal(t, int64(1), acked)
}

// TestPusher_SamplesDroppedRate — the metric behind the agent_buffer_full
// alert rule, which had no producer at all: the buffer's drop counters never
// left the agent's own /healthz body.
func TestPusher_SamplesDroppedRate(t *testing.T) {
	dir := t.TempDir()
	// A tiny volume so the retention policy genuinely rolls records off.
	buf, err := buffer.Open(dir, buffer.Options{MaxSize: 64})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	p := NewPusher("http://unused.invalid", "token", clock.System())
	p.SetBuffer(buf)

	start := time.Unix(1000, 0)
	require.Zero(t, p.SamplesDroppedRate(start), "the first call only establishes the baseline")
	require.Zero(t, p.SamplesDroppedRate(start.Add(10*time.Second)), "nothing dropped yet")

	for i := 0; i < 3; i++ {
		p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "0123456789012345"})
	}
	dropped := p.DroppedSamples()
	require.Positive(t, dropped, "the bounded buffer must have rolled records off")

	rate := p.SamplesDroppedRate(start.Add(20 * time.Second))
	require.InDelta(t, float64(dropped)/10.0, rate, 0.001, "drops per second over the interval since the last call")

	require.Zero(t, p.SamplesDroppedRate(start.Add(30*time.Second)), "a quiet interval reports zero, not the running total")
}

// TestPusher_UnencodableEnvelopeIsDroppedNotRetriedForever — a NaN metric
// value cannot be JSON-encoded. Keeping such an envelope queued would wedge
// every later push behind a record that can never be sent.
func TestPusher_UnencodableEnvelopeIsDroppedNotRetriedForever(t *testing.T) {
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, Instances: []wire.Instance{{
		InstanceID: "a",
		Results:    []wire.Result{{Check: "bgwriter", Metrics: []wire.Metric{{Name: "x", Value: math.NaN(), Kind: "gauge"}}}},
	}}})
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "good"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.ErrorContains(t, p.Push(ctx), "encode envelope")
	_, dropped := p.BufferStats()
	require.Equal(t, int64(1), dropped)

	p.pendingMu.Lock()
	remaining := len(p.pending)
	p.pendingMu.Unlock()
	require.Equal(t, 1, remaining, "only the good envelope may stay queued")

	// The next push delivers the envelope that was stuck behind it.
	require.NoError(t, p.Push(ctx))
	require.Equal(t, 1, received)
}

// TestPusher_RejectedEnvelopesAreCountedAsDropped covers every rejection that
// consumes an envelope without storing it: each one is lost data and has to
// reach pglens_samples_dropped_rate.
func TestPusher_RejectedEnvelopesAreCountedAsDropped(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"samples too old"}`))
			}))
			defer srv.Close()

			p := NewPusher(srv.URL, "token", clock.System())
			p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			require.NoError(t, p.Push(ctx))
			_, dropped := p.BufferStats()
			require.Equal(t, int64(1), dropped)
			require.Contains(t, p.LastError(), "samples too old")
		})
	}
}

// TestPusher_SendsTransmissionTime lets the server measure clock skew against
// the moment of sending rather than the moment of queueing.
func TestPusher_SendsTransmissionTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	var header string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get(wire.TransmittedAtHeader)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.NewFake(now))
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, SentAt: now.Add(-3 * time.Hour)})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))
	sent, err := time.Parse(time.RFC3339Nano, header)
	require.NoError(t, err)
	require.True(t, sent.Equal(now), "header %q, want the push time", header)
}

// TestPusher_TrailingSlashInServerURLStillPushes: server.url written with a
// trailing slash must reach the push endpoint, not //api/v1/push, which the
// server answers with 404 and every envelope was then dropped.
func TestPusher_TrailingSlashInServerURLStillPushes(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if path != "/api/v1/push" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL+"/", "token", clock.System())
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))
	require.Equal(t, "/api/v1/push", path)
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(1), acked)
	require.Equal(t, int64(0), dropped)
}

// roundTripFunc lets a test script the transport under the pusher's client.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestPusher_NetworkFailureReportsUnreachable: a push that fails at the
// network level must show in healthz while it is retried, and the first
// accepted push must clear it.
func TestPusher_NetworkFailureReportsUnreachable(t *testing.T) {
	p := NewPusher("http://pglens.invalid", "token", clock.System())
	calls := 0
	var observed Health
	var observedBody string
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("dial tcp: connection refused")
		}
		if calls == 2 {
			observed = p.HealthState()
			rec := httptest.NewRecorder()
			p.HealthzHandler()(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			observedBody = rec.Body.String()
			require.Equal(t, http.StatusOK, rec.Code)
		}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{},
			Request:    r,
		}, nil
	})

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, HealthUnreachable, observed)
	require.Contains(t, observedBody, `"state":"unreachable"`)
	require.Contains(t, observedBody, "connection refused")
	require.Equal(t, HealthOK, p.HealthState(), "an accepted push clears the unreachable state")
}

// TestPusher_NetworkFailureKeepsRejection: an agent the server has revoked
// stays revoked while the network is also down.
func TestPusher_NetworkFailureKeepsRejection(t *testing.T) {
	p := NewPusher("http://pglens.invalid", "token", clock.System())
	p.mu.Lock()
	p.health = HealthRevoked
	p.mu.Unlock()
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.Error(t, p.Push(ctx))

	require.Equal(t, HealthRevoked, p.HealthState())
}

// TestPusher_TransientRejectionsAreRetried: a 408, 425 or 429 says the push
// may succeed later. The envelope must be retried, not counted as lost.
func TestPusher_TransientRejectionsAreRetried(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.WriteHeader(status)
					return
				}
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()

			p := NewPusher(srv.URL, "token", clock.System())
			p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, p.Push(ctx))

			require.Equal(t, 2, requests)
			acked, dropped := p.BufferStats()
			require.Equal(t, int64(1), acked)
			require.Equal(t, int64(0), dropped)
		})
	}
}

// TestPusher_RetryAfterIsHonoured: a 429 that names a longer wait than the
// first backoff must not be retried before that wait has passed.
func TestPusher_RetryAfterIsHonoured(t *testing.T) {
	requests := 0
	var second time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		second = time.Now()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, 2, requests)
	require.GreaterOrEqual(t, second.Sub(start), 2*time.Second)
}

// TestPusher_NotFoundKeepsEnvelopeQueued: a 404 or 405 from the push endpoint
// means the URL or a proxy is wrong, not the envelope. It stays queued, and
// healthz reports the state as incompatible.
func TestPusher_NotFoundKeepsEnvelopeQueued(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(status)
			}))
			defer srv.Close()

			p := NewPusher(srv.URL, "token", clock.System())
			p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			require.NoError(t, p.Push(ctx))

			require.Equal(t, 1, requests)
			_, dropped := p.BufferStats()
			require.Equal(t, int64(0), dropped)
			p.pendingMu.Lock()
			require.Len(t, p.pending, 1)
			p.pendingMu.Unlock()
			require.Equal(t, HealthIncompatible, p.HealthState())
			require.Contains(t, p.LastError(), strconv.Itoa(status))
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"3":                             3 * time.Second,
		" 5 ":                           5 * time.Second,
		"0":                             0,
		"-4":                            0,
		"soon":                          0,
		"99999":                         maxRetryAfter,
		"Fri, 23 Oct 2026 12:00:30 GMT": 30 * time.Second,
		"Fri, 23 Oct 2026 11:59:00 GMT": 0,
		"Fri, 23 Oct 2026 14:00:00 GMT": maxRetryAfter,
	}
	for value, want := range cases {
		require.Equal(t, want, parseRetryAfter(value, now), "Retry-After %q", value)
	}
}

// shortAttemptTimeouts shrinks the attempt waits so a test that stalls the
// server waits milliseconds, not the production 10 seconds and 2 minutes.
func shortAttemptTimeouts(p *Pusher) {
	p.attemptTimeout = 50 * time.Millisecond
	p.maxAttemptTimeout = 200 * time.Millisecond
}

// releaseBackoffs moves a fake clock forward so every retry backoff the pusher
// sleeps on returns at once, instead of after its real exponential delay. It
// stops when done is closed.
func releaseBackoffs(clk *clock.Fake, done <-chan struct{}) {
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			clk.Advance(time.Hour)
			time.Sleep(time.Millisecond)
		}
	}()
}

// timeoutErr is a failure that net/http reports as a timeout, such as a dial
// that never completed, before the request was written.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "dial tcp: i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestPusher_StalledServerIsRetriedWithLongerWait: a server that needs longer
// than the first attempt's wait is given a longer wait on each retry, and the
// envelope is delivered once it answers. Nothing is dropped.
func TestPusher_StalledServerIsRetriedWithLongerWait(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// The body is read first: net/http reports a disconnect on the
		// request context only once the body has been consumed.
		_, _ = io.Copy(io.Discard, r.Body)
		// The answer takes 120ms, so the 50ms and 100ms waits expire and the
		// 200ms wait is the first one the answer fits in.
		select {
		case <-time.After(120 * time.Millisecond):
			w.WriteHeader(http.StatusAccepted)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	clk := clock.NewFake(time.Unix(0, 0))
	p := NewPusher(srv.URL, "token", clk)
	shortAttemptTimeouts(p)
	done := make(chan struct{})
	defer close(done)
	releaseBackoffs(clk, done)

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, int32(3), requests.Load())
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(1), acked)
	require.Equal(t, int64(0), dropped)
}

// TestPusher_ServerThatNeverAnswersDropsEnvelope: a server that takes the
// whole envelope and never answers cannot hold the queue head forever. The
// waits grow to their ceiling, and the envelope is then dropped and counted,
// so the push returns and the next envelope goes out.
func TestPusher_ServerThatNeverAnswersDropsEnvelope(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer srv.Close()

	clk := clock.NewFake(time.Unix(0, 0))
	p := NewPusher(srv.URL, "token", clk)
	shortAttemptTimeouts(p)
	done := make(chan struct{})
	defer close(done)
	releaseBackoffs(clk, done)

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, p.Push(ctx))

	// 50ms, 100ms, then the 200ms ceiling, which expires and drops the envelope.
	require.Equal(t, int32(3), requests.Load())
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(0), acked)
	require.Equal(t, int64(1), dropped)
	require.Contains(t, p.LastError(), "envelope dropped")
	p.pendingMu.Lock()
	require.Empty(t, p.pending)
	p.pendingMu.Unlock()
}

// TestPusher_UnreachableServerIsNeverDropped: a request that never reached the
// server, because the connection was refused or could not be made, is retried
// until the push context ends. The disk buffer holds the envelope through the
// outage, so it must not be counted as lost.
func TestPusher_UnreachableServerIsNeverDropped(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	p := NewPusher("http://pglens.invalid", "token", clk)
	shortAttemptTimeouts(p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	p.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 6 {
			cancel()
		}
		return nil, timeoutErr{}
	})
	done := make(chan struct{})
	defer close(done)
	releaseBackoffs(clk, done)

	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	require.Error(t, p.Push(ctx))

	require.GreaterOrEqual(t, calls, 6)
	acked, dropped := p.BufferStats()
	require.Equal(t, int64(0), acked)
	require.Equal(t, int64(0), dropped)
	p.pendingMu.Lock()
	require.Len(t, p.pending, 1)
	p.pendingMu.Unlock()
}

// TestPusher_ServerErrorHonoursRetryAfter: a 503 that names a longer wait than
// the first backoff is not retried before that wait has passed, as a 429 is
// not.
func TestPusher_ServerErrorHonoursRetryAfter(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	var second time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		second = time.Now()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := NewPusher(srv.URL, "token", clock.System())
	p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, p.Push(ctx))

	require.Equal(t, int32(2), requests.Load())
	require.GreaterOrEqual(t, second.Sub(start), 3*time.Second)
}
