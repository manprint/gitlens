package wire

import (
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RDY-WIRE-001: the agent_flags fact round-trips through the envelope with
// its unknown fields left out, and a consumer whose registry predates the kind
// refuses the fact as an unknown kind, which the server counts as an invalid
// fact and drops while it stores the rest of the push.
func TestRDYWIRE001_AgentFlagsRoundTripAndUnknownKind(t *testing.T) {
	t.Parallel()
	on, off := true, false
	flags := AgentFlags{CommandsEnabled: &on, AllowSignal: &off, ByApplication: &off, HostLocal: &on}
	raw, err := json.Marshal(flags)
	require.NoError(t, err)
	require.JSONEq(t, `{"commands_enabled":true,"allow_signal":false,"by_application":false,"host_local":true}`, string(raw),
		"an unknown flag is left out, never sent as false")

	fact := Fact{Kind: AgentFlagsFact, Key: AgentFlagsKey, ValueJSON: raw}
	require.NoError(t, fact.Validate())
	kind, ok := LookupFactKind(AgentFlagsFact)
	require.True(t, ok)
	require.Equal(t, FactKind{Check: AgentFlagsCheck, Replace: true}, kind)
	require.Equal(t, []string{AgentFlagsFact}, ReplacedFactKinds(AgentFlagsCheck))

	sent := Result{Check: AgentFlagsCheck, TS: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC), Facts: []Fact{fact}}
	body, err := json.Marshal(sent)
	require.NoError(t, err)
	var got Result
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Facts, 1)
	require.NoError(t, got.Facts[0].Validate())
	var decoded AgentFlags
	require.NoError(t, json.Unmarshal(got.Facts[0].ValueJSON, &decoded))
	require.Equal(t, flags, decoded)
	require.Nil(t, decoded.SessionQueryText)
	require.Nil(t, decoded.DataDirectoryReadable)

	// A server built before the kind: the same registry without it.
	older := maps.Clone(factKinds)
	delete(older, AgentFlagsFact)
	require.EqualError(t, got.Facts[0].validate(older), `unknown fact kind "agent_flags"`)
	// The other facts of that push still pass.
	require.NoError(t, Fact{Kind: "setting", Key: "work_mem", ValueText: "4MB"}.validate(older))
}
