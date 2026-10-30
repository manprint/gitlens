//go:build integration

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/advisor"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// advisorFinding is one finding row as the test reads it.
type advisorFinding struct {
	ID, RuleID, State string
	Reason            *string
	Resolved          bool
}

func instanceFindings(t *testing.T, pool *pgxpool.Pool, instanceID uuid.UUID) map[string]advisorFinding {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT finding_id, rule_id, state, evidence->>'resolved_reason', resolved_at IS NOT NULL
		FROM findings WHERE tenant_id = 'default' AND instance_id = $1`, instanceID)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]advisorFinding{}
	for rows.Next() {
		var f advisorFinding
		require.NoError(t, rows.Scan(&f.ID, &f.RuleID, &f.State, &f.Reason, &f.Resolved))
		out[f.ID] = f
	}
	require.NoError(t, rows.Err())
	return out
}

func putAdvisorJSON(t *testing.T, h http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func cleanAdvisorSettings(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `DELETE FROM findings; DELETE FROM advisor_rule_overrides; DELETE FROM server_settings`)
	require.NoError(t, err)
}

// ADVO-INT-001: on a migrated TimescaleDB, the real engine with the server's
// overrides source. A rule disabled through the API is skipped and its open,
// degraded and muted findings are resolved at the next evaluation with
// resolved_reason rule_disabled; the other rules' findings stay. Enabled again,
// the next evaluation reopens its findings and the marker is gone.
func TestADVOINT001_DisableResolvesEnableReopens(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cleanAdvisorSettings(t, pool)
	t.Cleanup(func() { truncateAll(t, pool); cleanAdvisorSettings(t, pool) })
	ctx := context.Background()

	// One live instance, registered as an agent would.
	instanceID := uuid.New()
	_, err := NewInventory(pool).Upsert(ctx, wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{{
			InstanceID: instanceID.String(), ClusterID: pgtype.ClusterID(7114).String(), ClusterIDSource: string(pgtype.IDSourceManual),
			Addr: "10.0.0.14", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0",
		}},
	})
	require.NoError(t, err)

	// A transaction open for an hour: txn.long_running (above 900 s) opens a
	// finding, the rule under test.
	const rule = "txn.long_running"
	_, err = pool.Exec(ctx, `INSERT INTO metrics (ts, cluster_id, instance_id, metric, series_id, value)
		VALUES (now(), $1, $2, 'pg_max_xact_age_seconds', 71140001, 3600)`, int64(7114), instanceID)
	require.NoError(t, err)

	engine := advisor.NewEngine(pool, nil, time.Hour)
	engine.SetOverridesSource(AdvisorOverridesSource(pool))
	t.Cleanup(engine.Stop)
	h := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(h)

	require.NoError(t, engine.Run(ctx))
	before := instanceFindings(t, pool, instanceID)
	var target advisorFinding
	for _, f := range before {
		if f.RuleID == rule {
			target = f
		}
	}
	require.Equal(t, "open", target.State, "the seeded metric opens a %s finding", rule)
	others := 0
	for _, f := range before {
		if f.RuleID != rule && !f.Resolved {
			others++
		}
	}
	require.Positive(t, others, "another rule keeps its findings")

	// A muted finding of the same rule is resolved as well (R-11.0 A1).
	mutedID := rule + "/muted-object"
	_, err = pool.Exec(ctx, `INSERT INTO findings (finding_id, rule_id, severity, state, scope, instance_id, title, detail, first_seen, last_seen, muted_until, mute_reason)
		VALUES ($1, $2, 'warning', 'muted', 'instance', $3, 't', 'd', now(), now(), now() + interval '1 day', 'test')`, mutedID, rule, instanceID)
	require.NoError(t, err)

	rec := putAdvisorJSON(t, h, "/api/v1/advisor/rules/"+rule+"/override", `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var enabled bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT enabled FROM advisor_rule_overrides WHERE tenant_id = 'default' AND rule_id = $1`, rule).Scan(&enabled))
	require.False(t, enabled)

	require.NoError(t, engine.Run(ctx))
	disabled := instanceFindings(t, pool, instanceID)
	for id, f := range disabled {
		if f.RuleID == rule {
			require.Equal(t, "resolved", f.State, id)
			require.True(t, f.Resolved, id)
			require.NotNil(t, f.Reason, id)
			require.Equal(t, "rule_disabled", *f.Reason, id)
		} else if !before[id].Resolved {
			require.NotEqual(t, "resolved", f.State, "%s of another rule stays", id)
			require.Nil(t, f.Reason, id)
		}
	}
	require.Equal(t, "resolved", disabled[mutedID].State)
	// The Findings page's default listing no longer shows them.
	var listed []findingJSON
	getJSON(t, h, "/api/v1/findings?rule_id="+rule, &listed)
	require.Empty(t, listed)

	rec = putAdvisorJSON(t, h, "/api/v1/advisor/rules/"+rule+"/override", `{"enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM advisor_rule_overrides`).Scan(&rows))
	require.Zero(t, rows, "enabling deletes the override")

	require.NoError(t, engine.Run(ctx))
	reopened := instanceFindings(t, pool, instanceID)
	f := reopened[target.ID]
	require.Equal(t, target.State, f.State, "the condition still holds, so the finding reopens")
	require.False(t, f.Resolved)
	require.Nil(t, f.Reason, "the reopening replaces the evidence, marker included")
	getJSON(t, h, "/api/v1/findings?rule_id="+rule, &listed)
	require.NotEmpty(t, listed)
}

// ADVO-INT-001 (store): the API's reads and writes of both tables on a
// migrated database: an override round trip, the windows' set, read-back and
// per-key reset, all or nothing for a refused body.
func TestADVOINT001_StoreRoundTrip(t *testing.T) {
	pool := getSharedPool(t)
	cleanAdvisorSettings(t, pool)
	t.Cleanup(func() { cleanAdvisorSettings(t, pool) })
	h := chi.NewRouter()
	NewAPI(pool).RegisterRoutes(h)

	var rules []advisorRuleJSON
	getJSON(t, h, "/api/v1/advisor/rules", &rules)
	for _, r := range rules {
		require.True(t, r.Enabled, r.ID)
		require.False(t, r.Overridden, r.ID)
	}
	require.Equal(t, http.StatusOK, putAdvisorJSON(t, h, "/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`).Code)
	require.Equal(t, http.StatusOK, putAdvisorJSON(t, h, "/api/v1/advisor/rules/index.unused/override", `{"enabled":false}`).Code, "disabling twice is idempotent")
	getJSON(t, h, "/api/v1/advisor/rules", &rules)
	for _, r := range rules {
		require.Equal(t, r.ID != "index.unused", r.Enabled, r.ID)
		require.Equal(t, r.ID == "index.unused", r.Overridden, r.ID)
	}
	src := AdvisorOverridesSource(pool)
	got, err := src(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"index.unused": false}, got)
	require.Equal(t, http.StatusOK, putAdvisorJSON(t, h, "/api/v1/advisor/rules/index.unused/override", `{"enabled":true}`).Code)
	got, err = src(context.Background())
	require.NoError(t, err)
	require.Empty(t, got)

	var windows []advisorWindowJSON
	getJSON(t, h, "/api/v1/settings/advisor-windows", &windows)
	require.Equal(t, renderAdvisorWindows(nil), windows)

	rec := putAdvisorJSON(t, h, "/api/v1/settings/advisor-windows", `{"window.unused_index":3600,"window.backup_history":172800}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	getJSON(t, h, "/api/v1/settings/advisor-windows", &windows)
	require.Equal(t, int64(3600), windows[0].Seconds)
	require.Equal(t, int64(172800), windows[1].Seconds)
	var stored string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT jsonb_typeof(value) || ':' || value::text FROM server_settings WHERE tenant_id = 'default' AND key = 'window.unused_index'`).Scan(&stored))
	require.Equal(t, "number:3600", stored, "a jsonb integer of seconds")

	require.Equal(t, http.StatusBadRequest, putAdvisorJSON(t, h, "/api/v1/settings/advisor-windows", `{"window.unused_index":7200,"window.pgstattuple":3600}`).Code)
	require.Equal(t, http.StatusOK, putAdvisorJSON(t, h, "/api/v1/settings/advisor-windows", `{"window.unused_index":null}`).Code)
	getJSON(t, h, "/api/v1/settings/advisor-windows", &windows)
	require.Equal(t, "default", windows[0].Source)
	require.Equal(t, int64(604800), windows[0].Seconds)
	require.Equal(t, "custom", windows[1].Source)
	require.Equal(t, int64(172800), windows[1].Seconds)
}
