//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/redact"
	"github.com/manprint/pglens/internal/wire"
)

// INT-INCIDENT-001: an incident captured by the agent's watcher is stored as
// one event, with a credential-bearing statement withheld; the same envelope
// sent again, or the same incident twice in it, adds no copy, and an incident
// of an unknown type is rejected rather than kept as an object fact.
func TestINTINCIDENT001_IncidentBecomesOneEvent(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	router := NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, nil), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))

	value := []byte(`{"id":"lock_wait-1-200","type":"lock_wait","captured_at":"2026-11-20T10:00:00Z","database":"app","relation":"public.orders","ctid":"(3,7)","lock_mode":"ShareLock","wait_seconds":2.5,
		"sessions":[{"pid":200,"query":"UPDATE orders SET v = 1 WHERE id = 42","blocked_by":[100]},{"pid":100,"state":"idle in transaction","query":"ALTER ROLE app PASSWORD 'x'"}]}`)
	fact := wire.Fact{Kind: "incident", Key: "lock_wait-1-200", Labels: map[string]string{"type": "lock_wait"}, ValueJSON: value}
	odd := wire.Fact{Kind: "incident", Key: "odd-1", Labels: map[string]string{"type": "odd"}, ValueJSON: []byte(`{}`)}
	ts := time.Now().UTC().Truncate(time.Microsecond)
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          ts,
		Instances: []wire.Instance{{
			InstanceID: uuid.NewString(),
			ClusterID:  pgtype.ClusterID(555444333).String(),
			Role:       "primary",
			Results:    []wire.Result{{Check: "ash", TS: ts, Facts: []wire.Fact{fact, fact, odd}}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	var n int
	var payload []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), max(payload::text) FROM events WHERE type = 'lock_wait_captured'`).Scan(&n, &payload))
	require.Equal(t, 1, n)
	var got struct {
		ID       string `json:"id"`
		Relation string `json:"relation"`
		CTID     string `json:"ctid"`
		Sessions []struct {
			PID   int    `json:"pid"`
			Query string `json:"query"`
		} `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(payload, &got))
	require.Equal(t, "lock_wait-1-200", got.ID)
	require.Equal(t, "public.orders", got.Relation)
	require.Equal(t, "(3,7)", got.CTID)
	require.Len(t, got.Sessions, 2)
	require.Contains(t, got.Sessions[0].Query, "WHERE id = 42")
	require.Equal(t, redact.Withheld, got.Sessions[1].Query)

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type NOT IN ('lock_wait_captured') AND payload ? 'id'`).Scan(&n))
	require.Zero(t, n)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE kind = 'incident'`).Scan(&n))
	require.Zero(t, n)
}
