//go:build integration

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/readiness"
	"github.com/manprint/pglens/internal/wire"
)

// RDY-INT-001: facts stored by the pipeline become a report, and removing the
// pg_stat_statements extension fact flips RDY-EXT-01 within one request (the
// cache is bypassed with a TTL that has always expired).
func TestRDYINT001_SeededFactsBecomeReport(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	inv := NewInventory(pool)
	p := NewPipeline(pool, clock.NewFake(time.Now()))
	api := NewAPI(pool)
	api.readinessCache.ttl = time.Nanosecond
	h := chi.NewRouter()
	api.RegisterRoutes(h)

	agentID, instID := uuid.NewString(), uuid.New()
	started := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	now := time.Now().UTC().Truncate(time.Second)
	push := func(results ...wire.Result) {
		t.Helper()
		env := wire.Envelope{AgentID: agentID, Instances: []wire.Instance{{
			InstanceID: instID.String(), ClusterID: pgtype.ClusterID(71321).String(), ClusterIDSource: string(pgtype.IDSourceManual),
			Addr: "10.0.14.2", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0",
			AgentStartedAt: &started, Results: results,
		}}}
		res, err := inv.Upsert(ctx, env)
		require.NoError(t, err)
		require.Equal(t, 1, res.Accepted, "%v", res.Errors)
		_, err = p.Process(ctx, env)
		require.NoError(t, err)
	}
	report := func() readiness.Report {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+instID.String()+"/readiness", nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out readiness.Report
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}
	ids := func(r readiness.Report) map[string]bool {
		out := map[string]bool{}
		for _, it := range r.Items {
			out[it.ID] = true
		}
		return out
	}
	extensions := func(names ...string) wire.Result {
		facts := make([]wire.Fact, 0, len(names))
		for _, n := range names {
			facts = append(facts, wire.Fact{Kind: "extension", Key: n, ValueText: "1.0"})
		}
		return wire.Result{Check: "extensions", TS: now, Database: "app", Facts: facts}
	}
	flags := json.RawMessage(`{"commands_enabled":true,"allow_signal":true,"by_application":true,"session_query_text":true,"host_local":false}`)

	push(
		wire.Result{Check: "settings", TS: now, Facts: []wire.Fact{
			{Kind: "setting", Key: "shared_preload_libraries", ValueText: "pg_stat_statements", Labels: map[string]string{"source": "configuration file"}},
			{Kind: "setting", Key: "track_io_timing", ValueText: "on", Labels: map[string]string{"source": "configuration file"}},
			{Kind: "setting", Key: "track_activity_query_size", ValueText: "4096", Labels: map[string]string{"source": "configuration file"}},
		}},
		extensions("plpgsql", "pg_stat_statements", "pgstattuple"),
		wire.Result{Check: "monitoring_role", TS: now, Facts: []wire.Fact{{
			Kind: "monitoring_role", Key: "posture", ValueText: "ok",
			Labels: map[string]string{"role": "pglens", "read_only_default": "true", "has_pg_monitor": "true", "has_read_all_stats": "true"},
		}}},
		wire.Result{Check: wire.AgentFlagsCheck, TS: now, Facts: []wire.Fact{{Kind: wire.AgentFlagsFact, Key: wire.AgentFlagsKey, ValueJSON: flags}}},
	)
	first := report()
	require.Equal(t, instID.String(), first.InstanceID)
	got := ids(first)
	require.True(t, got["RDY-NOTIF-01"], "no channel is configured in the test API")
	require.True(t, got["RDY-T2-01"], "tier T0")
	require.False(t, got["RDY-EXT-01"], "pg_stat_statements is preloaded and installed")
	require.False(t, got["RDY-SET-02"])
	require.False(t, got["RDY-ROLE-01"])
	require.Equal(t, readiness.LevelPartial, first.Level)

	// The prune keeps facts last seen at or after the result's time, so the
	// second result is one second later.
	now = now.Add(time.Second)
	push(extensions("plpgsql", "pgstattuple"))
	require.True(t, ids(report())["RDY-EXT-01"], "removing the extension fact flips RDY-EXT-01 within one request")
}
