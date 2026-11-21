//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
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

// TestINTPGL005_LogsAPI: seeded rows of three classes; a class filter returns
// only that class, the class counts match, and a text filter shaped like an
// injection matches nothing.
func TestINTPGL005_LogsAPI(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	ctx := context.Background()
	instance := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	seed := map[string]int{pglog.ClassDeadlock: 1, pglog.ClassErrorStatement: 3, pglog.ClassAuthFailed: 2}
	line := 0
	for class, n := range seed {
		for range n {
			line++
			_, err := pool.Exec(ctx, `INSERT INTO log_events (ts, cluster_id, instance_id, session_id, line_num, severity, class, message, queryid)
VALUES ($1, 1, $2, 's', $3, 'ERROR', $4, $5, 42)`, now.Add(-time.Duration(line)*time.Minute), instance, line, class, "message of "+class)
			require.NoError(t, err)
		}
	}
	h := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(h)
	get := func(path string, out any) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
	}
	window := "instance_id=" + instance.String() + "&from=" + url.QueryEscape(now.Add(-time.Hour).Format(time.RFC3339)) + "&to=" + url.QueryEscape(now.Add(time.Minute).Format(time.RFC3339))

	var rows []logEventResponse
	get("/api/v1/logs?"+window+"&class=lock.deadlock", &rows)
	require.Len(t, rows, 1)
	require.Equal(t, pglog.ClassDeadlock, rows[0].Class)
	require.Equal(t, "42", *rows[0].QueryID)

	get("/api/v1/logs?"+window+"&limit=2", &rows)
	require.Len(t, rows, 2)
	require.True(t, rows[0].TS.After(rows[1].TS))
	get("/api/v1/logs?"+window+"&before="+url.QueryEscape(rows[1].TS.Format(time.RFC3339Nano)), &rows)
	require.Len(t, rows, 4)

	get("/api/v1/logs?"+window+"&q="+url.QueryEscape("%' OR 1=1 --"), &rows)
	require.Empty(t, rows)
	get("/api/v1/logs?"+window+"&q=OF+LOCK", &rows)
	require.Len(t, rows, 1)

	var classes []logClassResponse
	get("/api/v1/logs/classes?"+window, &classes)
	require.Len(t, classes, 3)
	require.Equal(t, pglog.ClassErrorStatement, classes[0].Class)
	for _, c := range classes {
		require.Equal(t, int64(seed[c.Class]), c.Count, c.Class)
	}
}
