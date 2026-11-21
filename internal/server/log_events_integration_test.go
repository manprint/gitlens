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

	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// TestINTPGL004_LogEventsStoredOnce: a logs result pushed twice stores each
// line once, never as an object fact, and log_events keeps 14 days.
func TestINTPGL004_LogEventsStoredOnce(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	router := NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, nil), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))

	ts := time.Now().UTC().Truncate(time.Microsecond)
	labels := map[string]string{"ts": ts.Add(-time.Second).Format(time.RFC3339Nano), "class": pglog.ClassErrorStatement, "severity": "ERROR", "sqlstate": "23505", "datname": "app", "pid": "42"}
	fact := wire.Fact{Kind: "log_event", Key: "6b01761c.2e/9", Labels: labels, ValueJSON: []byte(`{"message":"duplicate key value violates unique constraint \"u_pkey\""}`)}
	bad := wire.Fact{Kind: "log_event", Key: "6b01761c.2e/10", Labels: map[string]string{"class": "bogus", "severity": "ERROR", "ts": labels["ts"]}, ValueJSON: []byte(`{"message":"x"}`)}
	instance := uuid.NewString()
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          ts,
		Instances: []wire.Instance{{
			InstanceID: instance,
			ClusterID:  pgtype.ClusterID(555444334).String(),
			Role:       "primary",
			Results: []wire.Result{{Check: "logs", TS: ts, Facts: []wire.Fact{fact, bad}, Metrics: []wire.Metric{
				{Name: "pg_log_events", Labels: map[string]string{"class": pglog.ClassErrorStatement}, Value: 1, Kind: "gauge"},
			}}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	}

	ctx := context.Background()
	var n int
	var class, sqlstate, session string
	var line int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), max(class), max(sqlstate), max(session_id), max(line_num) FROM log_events WHERE instance_id = $1`, instance).Scan(&n, &class, &sqlstate, &session, &line))
	require.Equal(t, 1, n)
	require.Equal(t, pglog.ClassErrorStatement, class)
	require.Equal(t, "23505", sqlstate)
	require.Equal(t, "6b01761c.2e", session)
	require.Equal(t, int64(9), line)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE kind = 'log_event'`).Scan(&n))
	require.Zero(t, n)

	var dropAfter string
	require.NoError(t, pool.QueryRow(ctx, `SELECT config->>'drop_after' FROM timescaledb_information.jobs WHERE proc_name = 'policy_retention' AND hypertable_name = 'log_events'`).Scan(&dropAfter))
	require.Equal(t, "14 days", dropAfter)
}
