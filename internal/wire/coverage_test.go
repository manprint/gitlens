package wire_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/wire"
)

// WIRE-COV-001: the agent start and the check intervals survive a JSON round
// trip under their wire names, and are left out of the JSON when unset, so an
// envelope without them is byte-identical to one from an older agent.
func TestWIRECOV001_InstanceRoundTrip(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, 10, 30, 8, 15, 0, 0, time.UTC)
	in := wire.Instance{InstanceID: "i", ClusterID: "1", AgentStartedAt: &started, CheckIntervals: map[string]int{"settings": 3600, "activity": 15}}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"agent_started_at":"2026-10-30T08:15:00Z"`)
	require.Contains(t, string(raw), `"check_intervals":{"activity":15,"settings":3600}`)
	var out wire.Instance
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, in, out)

	bare, err := json.Marshal(wire.Instance{InstanceID: "i", ClusterID: "1"})
	require.NoError(t, err)
	require.NotContains(t, string(bare), "agent_started_at")
	require.NotContains(t, string(bare), "check_intervals")
}
