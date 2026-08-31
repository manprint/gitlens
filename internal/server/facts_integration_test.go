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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func factsRouter(pool *pgxpool.Pool) http.Handler {
	return NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, clock.NewFake(time.Now())), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))
}

// INT-FACT-001: migration 0007 creates the three hypertables but leaves the
// relational object_facts table as an ordinary table.
func TestINTFACT001_MigrationCreatesTypedTables(t *testing.T) {
	pool := getSharedPool(t)
	ctx := context.Background()
	var hypertables int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_name IN ('metrics_tables','metrics_indexes','metrics_bloat')`).Scan(&hypertables))
	require.Equal(t, 3, hypertables)
	var isHypertable bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name='object_facts')`).Scan(&isHypertable))
	require.False(t, isHypertable)
}

// INT-FACT-002: replaying a typed table row is deduplicated by its identity
// and timestamp key.
func TestINTFACT002_TableStatsDeduplicate(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	row := store.TableStatRow{TS: time.Now().UTC().Truncate(time.Microsecond), TenantID: "default", ClusterID: 21, InstanceID: uuid.New(), Datname: "app", Schemaname: "public", Relname: "orders"}
	for range 2 {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, store.WriteTableStats(ctx, tx, []store.TableStatRow{row}))
		require.NoError(t, tx.Commit(ctx))
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_tables WHERE instance_id=$1`, row.InstanceID).Scan(&count))
	require.Equal(t, 1, count)
}

// INT-FACT-003: object fact history advances last_seen on replay but only
// advances changed_at when the value changes.
func TestINTFACT003_ObjectFactHistory(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	first := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	second := first.Add(5 * time.Minute)
	row := store.ObjectFactRow{TenantID: "default", ClusterID: 22, InstanceID: uuid.New(), Datname: "app", Kind: "setting", Key: "work_mem", Labels: map[string]string{}, ValueText: stringPtr("4MB"), FirstSeen: first, LastSeen: first, ChangedAt: first}
	writeFact := func(r store.ObjectFactRow) {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, store.WriteObjectFacts(ctx, tx, []store.ObjectFactRow{r}))
		require.NoError(t, tx.Commit(ctx))
	}
	writeFact(row)
	row.LastSeen, row.ChangedAt = second, second
	writeFact(row)
	var lastSeen, changedAt time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT last_seen, changed_at FROM object_facts WHERE instance_id=$1`, row.InstanceID).Scan(&lastSeen, &changedAt))
	require.WithinDuration(t, second, lastSeen, time.Microsecond)
	require.WithinDuration(t, first, changedAt, time.Microsecond)
	row.ValueText, row.LastSeen = stringPtr("8MB"), second.Add(5*time.Minute)
	writeFact(row)
	require.NoError(t, pool.QueryRow(ctx, `SELECT changed_at FROM object_facts WHERE instance_id=$1`, row.InstanceID).Scan(&changedAt))
	require.WithinDuration(t, row.LastSeen, changedAt, time.Microsecond)
}

// INT-FACT-004: a v1 envelope remains accepted by the v2 server.
func TestINTFACT004_V1EnvelopeStillPushes(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	instID := uuid.NewString()
	env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionMin, AgentID: uuid.NewString(), SentAt: time.Now(), Instances: []wire.Instance{{InstanceID: instID, ClusterID: "24", Role: "primary", Results: []wire.Result{{Check: "bgwriter", TS: time.Now(), Metrics: []wire.Metric{{Name: "pg_buffers_checkpoint", Value: 7, Kind: "gauge"}}}}}}}
	assertPush(t, factsRouter(pool), env, http.StatusAccepted)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM metrics WHERE metric='pg_buffers_checkpoint'`).Scan(&count))
	require.Equal(t, 1, count)
}

// INT-FACT-005: protocol v2 stores a typed relation metric and a setting fact
// in their respective tables in one accepted push.
func TestINTFACT005_V2StoresTypedMetricAndFact(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ts := time.Now().UTC().Truncate(time.Microsecond)
	env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent, AgentID: uuid.NewString(), SentAt: ts, Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: "25", Role: "primary", Results: []wire.Result{{Check: "table_stats", TS: ts, Database: "app", Metrics: []wire.Metric{{Name: "n_live_tup", Value: 12, Kind: "gauge", Labels: map[string]string{"schemaname": "public", "relname": "orders"}}}, Facts: []wire.Fact{{Kind: "setting", Key: "work_mem", ValueText: "4MB"}}}}}}}
	assertPush(t, factsRouter(pool), env, http.StatusAccepted)
	ctx := context.Background()
	var tables, facts int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metrics_tables WHERE relname='orders'`).Scan(&tables))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM object_facts WHERE key='work_mem'`).Scan(&facts))
	require.Equal(t, 1, tables)
	require.Equal(t, 1, facts)
}

// INT-FACT-006: unknown future protocol versions are rejected with the
// supported range in the response.
func TestINTFACT006_RejectsUnknownProtocol(t *testing.T) {
	pool := getSharedPool(t)
	reqEnv := wire.Envelope{ProtocolVersion: 3, AgentID: uuid.NewString(), SentAt: time.Now()}
	body, err := json.Marshal(reqEnv)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	factsRouter(pool).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "supported range 1-2")
}

func assertPush(t *testing.T, handler http.Handler, env wire.Envelope, status int) {
	t.Helper()
	body, err := json.Marshal(env)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, status, rec.Code, rec.Body.String())
}

func stringPtr(s string) *string { return &s }
