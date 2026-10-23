//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// INT-FACT-007: a setting put back to the empty string replaces its earlier
// value. The empty fact used to be refused, so synchronous_standby_names kept
// its old standby list and the alert went on requiring a synchronous standby.
func TestINTFACT007_EmptySettingReplacesValue(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	instance := uuid.NewString()
	push := func(ts time.Time, value string) {
		env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent, AgentID: uuid.NewString(), SentAt: ts, Instances: []wire.Instance{{InstanceID: instance, ClusterID: "27", Role: "primary", Results: []wire.Result{{Check: "settings", TS: ts, Facts: []wire.Fact{{Kind: "setting", Key: "synchronous_standby_names", ValueText: value}}}}}}}
		assertPush(t, factsRouter(pool), env, http.StatusAccepted)
	}
	first := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
	push(first, "standby1")
	push(first.Add(30*time.Second), "")
	var value *string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT value_text FROM object_facts WHERE instance_id=$1 AND kind='setting' AND key='synchronous_standby_names'`, instance).Scan(&value))
	require.NotNil(t, value)
	require.Equal(t, "", *value)
}

// INT-FACT-008: a complete result replaces the facts of the kinds its check
// owns, for its database only. A fact it no longer reports is removed (an
// index dropped, a setting put back to its default); one it still reports
// keeps its first_seen. A partial, truncated or failed result removes
// nothing, and neither does a result of another check.
func TestINTFACT008_CompleteResultReplacesItsFacts(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	instance := uuid.NewString()
	push := func(results ...wire.Result) {
		env := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent, AgentID: uuid.NewString(), SentAt: results[0].TS, Instances: []wire.Instance{{InstanceID: instance, ClusterID: "28", Role: "primary", Results: results}}}
		assertPush(t, factsRouter(pool), env, http.StatusAccepted)
	}
	def := func(key string) wire.Fact { return wire.Fact{Kind: "index_def", Key: key, ValueText: "CREATE INDEX " + key} }
	facts := func() []string {
		rows, err := pool.Query(ctx, `SELECT datname || ':' || kind || ':' || key FROM object_facts WHERE instance_id=$1 AND kind IN ('index_def', 'setting') ORDER BY 1`, instance)
		require.NoError(t, err)
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		return got
	}
	t1 := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	push(
		wire.Result{Check: "index_stats", TS: t1, Database: "app", Facts: []wire.Fact{def("public.a"), def("public.b")}},
		wire.Result{Check: "index_stats", TS: t1, Database: "other", Facts: []wire.Fact{def("public.c")}},
		wire.Result{Check: "settings", TS: t1, Facts: []wire.Fact{{Kind: "setting", Key: "work_mem", ValueText: "8MB"}, {Kind: "setting", Key: "wal_level", ValueText: "logical"}}},
	)
	require.Equal(t, []string{":setting:wal_level", ":setting:work_mem", "app:index_def:public.a", "app:index_def:public.b", "other:index_def:public.c"}, facts())

	// Nothing that is not complete replaces anything.
	t2 := t1.Add(time.Minute)
	push(
		wire.Result{Check: "index_stats", TS: t2, Database: "app", Partial: true},
		wire.Result{Check: "index_stats", TS: t2, Database: "other", Truncated: true},
		wire.Result{Check: "settings", TS: t2, Error: "timeout"},
		wire.Result{Check: "table_stats", TS: t2, Database: "app"},
	)
	require.Len(t, facts(), 5)

	t3 := t2.Add(time.Minute)
	push(
		wire.Result{Check: "index_stats", TS: t3, Database: "app", Facts: []wire.Fact{def("public.a")}},
		wire.Result{Check: "settings", TS: t3, Facts: []wire.Fact{{Kind: "setting", Key: "work_mem", ValueText: "8MB"}}},
	)
	require.Equal(t, []string{":setting:work_mem", "app:index_def:public.a", "other:index_def:public.c"}, facts())
	var firstSeen time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT first_seen FROM object_facts WHERE instance_id=$1 AND kind='index_def' AND key='public.a'`, instance).Scan(&firstSeen))
	require.True(t, firstSeen.Equal(t1), "first_seen %s, want %s", firstSeen, t1)

	// A complete result that reports nothing removes everything of its kind.
	push(wire.Result{Check: "index_stats", TS: t3.Add(time.Minute), Database: "other"})
	require.Equal(t, []string{":setting:work_mem", "app:index_def:public.a"}, facts())
}

// INT-FACT-006: unknown future protocol versions are rejected with the
// supported range in the response.
func TestINTFACT006_RejectsUnknownProtocol(t *testing.T) {
	pool := getSharedPool(t)
	reqEnv := wire.Envelope{ProtocolVersion: wire.ProtocolVersionCurrent + 1, AgentID: uuid.NewString(), SentAt: time.Now()}
	body, err := json.Marshal(reqEnv)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	factsRouter(pool).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), fmt.Sprintf("supported range %d-%d", wire.ProtocolVersionMin, wire.ProtocolVersionCurrent))
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
