package wire_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestEnvelope_ClusterIDIsAString(t *testing.T) {
	t.Parallel()
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         "00000000-0000-0000-0000-000000000000",
		SentAt:          time.Now(),
		Instances: []wire.Instance{
			{InstanceID: "11111111-1111-1111-1111-111111111111", ClusterID: "18446744073709551615", ClusterIDSource: "system_identifier"},
		},
	}
	data, err := json.Marshal(env)
	require.NoError(t, err)
	// Raw JSON should contain quoted string
	require.Contains(t, string(data), `"18446744073709551615"`)
	// Unmarshal should return exact value
	var decoded wire.Envelope
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, "18446744073709551615", decoded.Instances[0].ClusterID)
	// Also test MaxUint64 round trip
	var id uint64 = math.MaxUint64
	s := wire.Envelope{Instances: []wire.Instance{{ClusterID: "18446744073709551615"}}}
	_ = s
	_ = id
}

func TestEnvelope_RoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Second).UTC()
	env := wire.Envelope{
		ProtocolVersion: 1,
		AgentID:         "agent-1",
		AgentVersion:    "dev",
		SentAt:          now,
		Instances: []wire.Instance{
			{
				InstanceID:      "inst-1",
				ClusterID:       "123",
				ClusterIDSource: "system_identifier",
				Addr:            "127.0.0.1",
				Port:            5432,
				PGVersion:       150014,
				Role:            "primary",
				PermTier:        "T0",
				Capabilities:    []string{"instance_info"},
				Databases:       []wire.Database{{Name: "app", Monitored: true}},
				Results: []wire.Result{
					{Check: "instance_info", TS: now, Metrics: []wire.Metric{{Name: "pg_up", Value: 1, Kind: "gauge"}}},
				},
			},
		},
	}
	data, err := json.Marshal(env)
	require.NoError(t, err)
	var decoded wire.Envelope
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, env.ProtocolVersion, decoded.ProtocolVersion)
	require.Equal(t, env.AgentID, decoded.AgentID)
	require.Len(t, decoded.Instances, 1)
	require.Equal(t, "123", decoded.Instances[0].ClusterID)
}

func TestEnvelope_OmitEmpty(t *testing.T) {
	t.Parallel()
	env := wire.Envelope{
		ProtocolVersion: 1,
		AgentID:         "a",
		SentAt:          time.Now(),
		Instances:       []wire.Instance{{InstanceID: "i", ClusterID: "1"}},
	}
	data, err := json.Marshal(env)
	require.NoError(t, err)
	m := map[string]any{}
	require.NoError(t, json.Unmarshal(data, &m))
	// Instances should not have empty optional fields like topology_edges
	require.NotContains(t, string(data), "topology_edges")
}

func TestGolden_AssertGolden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/golden.json"
	data := map[string]string{"a": "1"}
	wire.AssertGolden(t, data, path, true)
	require.FileExists(t, path)
	wire.AssertGolden(t, data, path, false)
}
