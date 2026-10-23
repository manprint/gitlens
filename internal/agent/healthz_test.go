package agent

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/manprint/pglens/internal/agent/buffer"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

// TestPusher_HealthzReportsBufferRetentionDrops — the disk buffer rolls records
// off under its size policy, and those are samples the agent has lost. The
// drop-rate metric counted them, but /healthz read only the Queue fallback's
// counter, so buffer_stats.dropped stayed at 0 while the buffer discarded data.
func TestPusher_HealthzReportsBufferRetentionDrops(t *testing.T) {
	buf, err := buffer.Open(t.TempDir(), buffer.Options{MaxSize: 64})
	require.NoError(t, err)
	defer func() { _ = buf.Close() }()

	p := NewPusher("http://unused.invalid", "token", clock.System())
	p.SetBuffer(buf)
	for i := 0; i < 20; i++ {
		p.Queue(&wire.Envelope{ProtocolVersion: wire.ProtocolVersion, AgentID: "0123456789012345"})
	}
	require.Positive(t, p.DroppedSamples(), "the bounded buffer must have rolled records off")

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	p.HealthzHandler()(w, req)

	var result struct {
		BufferStats struct {
			Dropped int64 `json:"dropped"`
		} `json:"buffer_stats"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, p.DroppedSamples(), result.BufferStats.Dropped)
}
