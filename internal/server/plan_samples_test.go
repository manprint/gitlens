package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func autoPlanLabels() map[string]string {
	return map[string]string{
		"source": "auto_explain", "ts": "2026-11-21T10:00:01.5Z", "datname": "app",
		"queryid": "-77", "plan_hash": "h1", "analyzed": "true", "duration_ms": "12.5",
	}
}

// AEX-007: a fact with a body gives a plan row and a sample row; one without
// a body gives the sample row only.
func TestAEX007(t *testing.T) {
	inst := uuid.New()
	plan := `[{"Plan":{"Node Type":"Seq Scan","Actual Total Time":1}}]`
	p, skip, reason := autoPlanFromFact(autoPlanLabels(), json.RawMessage(`{"plan":`+plan+`}`), "default", 7, inst)
	require.False(t, skip)
	require.Empty(t, reason)
	ts := time.Date(2026, 11, 21, 10, 0, 1, 500_000_000, time.UTC)
	require.Equal(t, ts, p.sample.TS)
	require.Equal(t, int64(-77), p.sample.QueryID)
	require.Equal(t, "h1", p.sample.PlanHash)
	require.Equal(t, 12.5, p.sample.DurationMS)
	require.True(t, p.sample.Analyzed)
	require.Equal(t, "app", p.sample.Datname)
	require.Equal(t, inst, p.sample.InstanceID)
	require.Equal(t, int64(7), p.sample.ClusterID)
	require.NotNil(t, p.plan)
	require.Equal(t, "auto_explain", p.plan.Source)
	require.Equal(t, ts, p.plan.CapturedAt)
	require.JSONEq(t, plan, string(p.plan.Plan))
	require.Equal(t, "h1", p.plan.PlanHash)
	require.True(t, p.plan.Analyzed)

	p, skip, reason = autoPlanFromFact(autoPlanLabels(), json.RawMessage(`{}`), "default", 7, inst)
	require.False(t, skip)
	require.Empty(t, reason)
	require.Nil(t, p.plan)
	require.Equal(t, ts, p.sample.TS)

	// A body that may hold a credential drops the whole fact (I-4).
	_, skip, reason = autoPlanFromFact(autoPlanLabels(), json.RawMessage(`{"plan":[{"Plan":{"Filter":"(password = 'hunter2secret'::text)"}}]}`), "default", 7, inst)
	require.False(t, skip)
	require.Equal(t, planRejectedSecret, reason)
}

// AEX-008: labels outside the contract reject the fact.
func TestAEX008(t *testing.T) {
	for name, mutate := range map[string]func(map[string]string){
		"negative duration": func(l map[string]string) { l["duration_ms"] = "-1" },
		"text duration":     func(l map[string]string) { l["duration_ms"] = "fast" },
		"NaN duration":      func(l map[string]string) { l["duration_ms"] = "NaN" },
		"no duration":       func(l map[string]string) { delete(l, "duration_ms") },
		"queryid":           func(l map[string]string) { l["queryid"] = "x" },
		"no plan_hash":      func(l map[string]string) { l["plan_hash"] = "" },
		"ts":                func(l map[string]string) { l["ts"] = "yesterday" },
	} {
		l := autoPlanLabels()
		mutate(l)
		_, skip, reason := autoPlanFromFact(l, json.RawMessage(`{}`), "default", 7, uuid.New())
		require.False(t, skip, name)
		require.Equal(t, "invalid_fact", reason, name)
	}
	_, _, reason := autoPlanFromFact(autoPlanLabels(), json.RawMessage(`[`), "default", 7, uuid.New())
	require.Equal(t, "invalid_fact", reason)
}

// AEX-009: a plan fact without source auto_explain is skipped as before.
func TestAEX009(t *testing.T) {
	l := autoPlanLabels()
	delete(l, "source")
	_, skip, reason := autoPlanFromFact(l, json.RawMessage(`{"plan":[]}`), "default", 7, uuid.New())
	require.True(t, skip)
	require.Empty(t, reason)
	l["source"] = "command"
	_, skip, _ = autoPlanFromFact(l, nil, "default", 7, uuid.New())
	require.True(t, skip)
}
