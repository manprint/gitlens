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
	router := NewRouter(UIConfig{}, nil, nil, NewAuth("test-token"), NewInventory(pool), NewPipeline(pool, nil), NewAPI(pool), NewTopologyAPI(pool), NewAshAPI(pool))
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
