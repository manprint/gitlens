//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

func pushPlanFacts(t *testing.T, router http.Handler, instance string, ts time.Time, facts ...wire.Fact) {
	t.Helper()
	env := wire.Envelope{
		ProtocolVersion: wire.ProtocolVersion,
		AgentID:         uuid.NewString(),
		SentAt:          ts,
		Instances: []wire.Instance{{
			InstanceID: instance,
			ClusterID:  pgtype.ClusterID(555444335).String(),
			Role:       "primary",
			Results:    []wire.Result{{Check: "logs", TS: ts, Facts: facts}},
		}},
	}
	body, err := json.Marshal(env)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/push", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

func autoPlanFact(ts time.Time, plan string) wire.Fact {
	value := `{}`
	if plan != "" {
		value = `{"plan":` + plan + `}`
	}
	return wire.Fact{
		Kind: "plan", Key: "77:h1:" + ts.Format(time.RFC3339Nano),
		Labels: map[string]string{
			"source": "auto_explain", "ts": ts.Format(time.RFC3339Nano), "datname": "app",
			"queryid": "77", "plan_hash": "h1", "analyzed": "false", "duration_ms": "3.25",
		},
		ValueJSON: []byte(value),
	}
}

func planRouter(t *testing.T) (http.Handler, func(string, string) int) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	t.Cleanup(func() { truncateAll(t, pool) })
	auth := NewAuth("test-token")
	api := NewAPI(pool)
	api.SetAuth(auth)
	router := NewRouter(UIConfig{}, nil, nil, auth, NewInventory(pool), NewPipeline(pool, nil), api, NewTopologyAPI(pool), NewAshAPI(pool))
	count := func(table, instance string) int {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE instance_id = $1`, instance).Scan(&n))
		return n
	}
	return router, count
}

// TestINTAEX002: two pushes of the same plan with different ts store one
// query_plans row with source auto_explain and two plan_samples rows; a push
// repeated verbatim adds nothing.
func TestINTAEX002(t *testing.T) {
	router, count := planRouter(t)
	pool := getSharedPool(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	plan := `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"t"}}]`

	first, second := now.Add(-2*time.Second), now.Add(-time.Second)
	pushPlanFacts(t, router, instance, now, autoPlanFact(first, plan))
	pushPlanFacts(t, router, instance, now, autoPlanFact(second, ""))
	pushPlanFacts(t, router, instance, now, autoPlanFact(second, ""))

	require.Equal(t, 1, count("query_plans", instance))
	require.Equal(t, 2, count("plan_samples", instance))
	var source string
	var captured time.Time
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT source, captured_at FROM query_plans WHERE instance_id = $1`, instance).Scan(&source, &captured))
	require.Equal(t, "auto_explain", source)
	require.True(t, first.Equal(captured), captured)
	var duration float64
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT max(duration_ms) FROM plan_samples WHERE instance_id = $1 AND datname = 'app' AND queryid = 77 AND plan_hash = 'h1'`, instance).Scan(&duration))
	require.Equal(t, 3.25, duration)
	var facts int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM object_facts WHERE kind = 'plan'`).Scan(&facts))
	require.Zero(t, facts)

	var dropAfter string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT config->>'drop_after' FROM timescaledb_information.jobs WHERE proc_name = 'policy_retention' AND hypertable_name = 'plan_samples'`).Scan(&dropAfter))
	require.Equal(t, "30 days", dropAfter)
}

// TestINTAEX003: a pushed body containing PASSWORD 'x' stores no plan row and
// no sample row (I-4).
func TestINTAEX003(t *testing.T) {
	router, count := planRouter(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	pushPlanFacts(t, router, instance, now, autoPlanFact(now.Add(-time.Second), `[{"Plan":{"Node Type":"Result","Output":["ALTER ROLE app PASSWORD 'x'"]}}]`))
	require.Zero(t, count("query_plans", instance))
	require.Zero(t, count("plan_samples", instance))
}

// TestINTAEX004: two samples with parameters, purge: both rows remain with
// NULL parameters, one audit row of kind purge_plan_parameters, and neither
// the answer, the audit row nor the server log holds a parameter value.
func TestINTAEX004(t *testing.T) {
	router, count := planRouter(t)
	pool := getSharedPool(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	withParams := func(ts time.Time) wire.Fact {
		f := autoPlanFact(ts, "")
		f.ValueJSON = []byte(`{"parameters":"$1 = 'zz-value-9137'"}`)
		return f
	}
	pushPlanFacts(t, router, instance, now, withParams(now.Add(-2*time.Second)), withParams(now.Add(-time.Second)))
	ctx := context.Background()
	var withValue int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM plan_samples WHERE instance_id = $1 AND parameters = '$1 = ''zz-value-9137'''`, instance).Scan(&withValue))
	require.Equal(t, 2, withValue)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/plans/samples/parameters?instance_id="+instance, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"cleared":2}`, rec.Body.String())

	require.Equal(t, 2, count("plan_samples", instance))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM plan_samples WHERE instance_id = $1 AND parameters IS NOT NULL`, instance).Scan(&withValue))
	require.Zero(t, withValue)
	var args, who string
	require.NoError(t, pool.QueryRow(ctx, `SELECT args::text, requested_by FROM command_audit WHERE instance_id = $1 AND kind = 'purge_plan_parameters'`, instance).Scan(&args, &who))
	require.JSONEq(t, `{"cleared":2}`, args)
	require.True(t, strings.HasPrefix(who, "api-token"), who)
	require.Contains(t, logs.String(), "parameters of 2 samples")
	require.NotContains(t, logs.String()+rec.Body.String()+args, "zz-value-9137")

	// Without a credential the purge is refused.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/plans/samples/parameters?instance_id="+instance, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func getPlanJSON(t *testing.T, router http.Handler, url string, out any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out))
}

// TestAEX015: /plans reports the source of each stored plan.
func TestAEX015(t *testing.T) {
	router, _ := planRouter(t)
	pool := getSharedPool(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	pushPlanFacts(t, router, instance, now, autoPlanFact(now.Add(-time.Second), `[{"Plan":{"Node Type":"Seq Scan","Relation Name":"t"}}]`))
	_, err := pool.Exec(context.Background(), `INSERT INTO query_plans (tenant_id, instance_id, cluster_id, datname, queryid, plan_hash, analyzed, captured_at, plan) VALUES ('default', $1, 555444335, 'app', 77, 'h2', false, $2, '[{"Plan":{"Node Type":"Result"}}]')`, instance, now)
	require.NoError(t, err)

	var out struct {
		Plans []struct {
			PlanHash string `json:"plan_hash"`
			Source   string `json:"source"`
		} `json:"plans"`
	}
	getPlanJSON(t, router, "/api/v1/plans?queryid=77&instance_id="+instance, &out)
	sources := map[string]string{}
	for _, p := range out.Plans {
		sources[p.PlanHash] = p.Source
	}
	require.Equal(t, map[string]string{"h1": "auto_explain", "h2": "command"}, sources)
}

// TestINTAEX005: three samples of two plans; the list filters by plan_hash,
// newest first, and holds parameter values only on request.
func TestINTAEX005(t *testing.T) {
	router, count := planRouter(t)
	instance := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	sample := func(ts time.Time, hash string, params bool) wire.Fact {
		f := autoPlanFact(ts, "")
		f.Labels["plan_hash"] = hash
		f.Key = "77:" + hash + ":" + ts.Format(time.RFC3339Nano)
		if params {
			f.ValueJSON = []byte(`{"parameters":"$1 = '42'"}`)
		}
		return f
	}
	pushPlanFacts(t, router, instance, now,
		sample(now.Add(-3*time.Second), "h1", false),
		sample(now.Add(-2*time.Second), "h1", true),
		sample(now.Add(-time.Second), "h2", false))
	require.Equal(t, 3, count("plan_samples", instance))

	type row struct {
		TS            time.Time `json:"ts"`
		QueryID       string    `json:"queryid"`
		PlanHash      string    `json:"plan_hash"`
		HasParameters bool      `json:"has_parameters"`
		Parameters    *string   `json:"parameters"`
	}
	var out struct {
		Samples []row `json:"samples"`
	}
	base := "/api/v1/plans/samples?queryid=77&plan_hash=h1&instance_id=" + instance
	getPlanJSON(t, router, base, &out)
	require.Len(t, out.Samples, 2)
	require.True(t, out.Samples[0].TS.After(out.Samples[1].TS), "newest first")
	require.Equal(t, "77", out.Samples[0].QueryID)
	require.True(t, out.Samples[0].HasParameters)
	require.False(t, out.Samples[1].HasParameters)
	for _, s := range out.Samples {
		require.Equal(t, "h1", s.PlanHash)
		require.Nil(t, s.Parameters, "no values without include_parameters")
	}

	out.Samples = nil
	getPlanJSON(t, router, base+"&include_parameters=true", &out)
	require.Len(t, out.Samples, 2)
	require.NotNil(t, out.Samples[0].Parameters)
	require.Equal(t, "$1 = '42'", *out.Samples[0].Parameters)

	out.Samples = nil
	getPlanJSON(t, router, "/api/v1/plans/samples?queryid=77&instance_id="+instance+"&limit=1", &out)
	require.Len(t, out.Samples, 1)
	require.Equal(t, "h2", out.Samples[0].PlanHash)
}
