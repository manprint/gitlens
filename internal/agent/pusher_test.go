package agent

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
