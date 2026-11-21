package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/wire"
)

func TestIncidentEvent(t *testing.T) {
	inst := uuid.New()
	ts := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	raw := []byte(`{"id":"agent-side","type":"deadlock","relation":"public.orders","ctid":"(0,1)","wait_seconds":1.25,
		"sessions":[{"pid":10,"query":"ALTER ROLE app PASSWORD 'x'"},{"pid":20,"query":"UPDATE orders SET v = 1"}]}`)
	f := wire.Fact{Kind: incidentFactKind, Key: "deadlock-1-10", Labels: map[string]string{"type": "deadlock"}, ValueJSON: raw}

	ev, ok := incidentEvent(f, ts, "default", 7, inst)
	require.True(t, ok)
	require.Equal(t, "deadlock_captured", ev.Type)
	require.Equal(t, ts, ev.TS)
	require.Equal(t, int64(7), *ev.ClusterID)
	require.Equal(t, inst, *ev.InstanceID)
	require.Equal(t, "deadlock-1-10", ev.Payload["id"], "the fact key names the incident")
	require.Equal(t, "public.orders", ev.Payload["relation"])
	require.Equal(t, json.Number("1.25"), ev.Payload["wait_seconds"])
	sessions := ev.Payload["sessions"].([]any)
	require.Equal(t, redact.Withheld, sessions[0].(map[string]any)["query"])
	require.Equal(t, "UPDATE orders SET v = 1", sessions[1].(map[string]any)["query"])

	for typ, want := range incidentEventTypes {
		f.Labels = map[string]string{"type": typ}
		ev, ok := incidentEvent(f, ts, "default", 7, inst)
		require.True(t, ok)
		require.Equal(t, want, ev.Type)
	}

	f.Labels = map[string]string{"type": "unknown"}
	_, ok = incidentEvent(f, ts, "default", 7, inst)
	require.False(t, ok)
	f.Labels = map[string]string{"type": "deadlock"}
	f.ValueJSON = []byte(`[1]`)
	_, ok = incidentEvent(f, ts, "default", 7, inst)
	require.False(t, ok)
	f.ValueJSON = nil
	_, ok = incidentEvent(f, ts, "default", 7, inst)
	require.False(t, ok)
}
