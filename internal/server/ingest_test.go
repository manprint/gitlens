package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestIngest_RejectsUnknownProtocolVersion(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent + 1, AgentID: "a", SentAt: time.Now()}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unsupported protocol_version")
}

func TestIngest_RejectsUnknownFields(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	// Use raw JSON with unknown field
	body := []byte(`{"protocol_version":1,"agent_id":"a","sent_at":"2026-01-01T00:00:00Z","instances":[],"unknown_field":123}`)
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIngest_RejectsRevokedAgent(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	revokedAt := time.Now()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{&revokedAt}}}}
	inv := &Inventory{pool: pool}
	h := IngestHandler(auth, inv, NewPipeline(nil, nil))
	env := wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: uuid.NewString(), SentAt: time.Now()}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "revoked")
}

func TestIngest_RejectsStaleEnvelope(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	env := wire.Envelope{ProtocolVersion: 1, AgentID: "a", SentAt: time.Now().Add(-13 * time.Hour)}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIngest_AcceptsFutureSkew(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	env := wire.Envelope{ProtocolVersion: 1, AgentID: "a", SentAt: time.Now().Add(5 * time.Minute)}
	body, _ := json.Marshal(env)
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
}

// TestIngest_AcceptsGzipBody exercises exactly what agent.Pusher sends in
// production (internal/agent/pusher.go's buildRequest always gzips and sets
// Content-Encoding) — without gzip decompression on this side, every real
// agent push failed with "bad json: invalid character '\x1f'..." (gzip's
// own magic byte fed straight to the JSON decoder). No test caught this
// until a real agent binary existed to drive a real POST here.
func TestIngest_AcceptsGzipBody(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	env := wire.Envelope{ProtocolVersion: 1, AgentID: "a", SentAt: time.Now()}
	raw, _ := json.Marshal(env)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(raw)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
}

func TestIngest_RejectsInvalidGzipBody(t *testing.T) {
	t.Parallel()
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))
	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader([]byte("not gzip")))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid gzip body")
}

// TestIngest_RejectsDecompressionBomb — MaxBytesReader bounds the compressed
// body only. Envelope JSON is extremely repetitive, so a body well inside the
// 32 MiB wire limit decompresses to orders of magnitude more: reading it into
// memory unbounded is a single-request out-of-memory kill of the server.
func TestIngest_RejectsDecompressionBomb(t *testing.T) {
	// Not t.Parallel(): this test temporarily lowers a package-level limit.
	auth := NewAuth("token")
	h := IngestHandler(auth, NewInventory(nil), NewPipeline(nil, nil))

	// Lowered so the assertion is about the boundary, not about how fast this
	// machine can inflate 256 MiB of zeros.
	original := maxDecompressedBytes
	maxDecompressedBytes = 1 << 20
	t.Cleanup(func() { maxDecompressedBytes = original })

	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	// Zeros compress at roughly 1000:1, so a body of a few KiB on the wire
	// expands past the limit — far below the MaxBytesReader limit that used
	// to be the only bound.
	chunk := make([]byte, 1<<16)
	for written := int64(0); written < maxDecompressedBytes+(1<<16); written += int64(len(chunk)) {
		if _, err := gz.Write(chunk); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
	}
	require.NoError(t, gz.Close())
	require.Less(t, int64(body.Len()), int64(maxBodyBytes), "the bomb must pass the wire-size limit to exercise the decompressed one")

	req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader(body.Bytes()))
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Contains(t, w.Body.String(), "payload too large")
}

// TestIngest_TransmittedAtHeaderIsTheSkewReference covers an envelope
// replayed from the agent's buffer: it was queued an hour ago but sent now,
// so the agent's clock is fine.
func TestIngest_TransmittedAtHeaderIsTheSkewReference(t *testing.T) {
	t.Parallel()
	queued := time.Now().Add(-time.Hour)
	sent := time.Now()
	req := httptest.NewRequest("POST", "/api/v1/push", nil)
	req.Header.Set(wire.TransmittedAtHeader, sent.UTC().Format(time.RFC3339Nano))
	require.WithinDuration(t, sent, transmittedAt(req, queued), time.Millisecond)

	legacy := httptest.NewRequest("POST", "/api/v1/push", nil)
	require.Equal(t, queued, transmittedAt(legacy, queued), "agents without the header fall back to sent_at")

	env := wire.Envelope{SentAt: queued, TransmittedAt: sent}
	inst := wire.Instance{Results: []wire.Result{{Check: "activity", TS: queued}}}
	rows := selfMetricRows(env, inst, "default", 1, uuid.New(), sent.Add(time.Second))
	for _, r := range rows {
		if r.Metric == "pglens_agent_clock_skew_seconds" {
			require.InDelta(t, 1.0, r.Value, 0.01, "an hour of queueing is not skew")
			return
		}
	}
	t.Fatal("no clock skew sample")
}

// TestIngest_ErrorBodiesAreJSON: a decode error quotes the offending
// character, and pasting it unescaped into a Sprintf'd body made the error
// response itself invalid JSON (sent as text/plain).
func TestIngest_ErrorBodiesAreJSON(t *testing.T) {
	t.Parallel()
	h := IngestHandler(NewAuth("token"), NewInventory(nil), NewPipeline(nil, nil))
	for name, body := range map[string]string{
		"quote":     `{"protocol_version": "1"}`,
		"garbage":   `"}{`,
		"truncated": `{"agent_id":`,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/push", bytes.NewReader([]byte(body)))
			req.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))
			var resp apiError
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
			require.Equal(t, "bad json", resp.Error)
			require.NotEmpty(t, resp.Detail)
		})
	}
}
