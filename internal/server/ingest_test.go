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
	env := wire.Envelope{ProtocolVersion: 3, AgentID: "a", SentAt: time.Now()}
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
