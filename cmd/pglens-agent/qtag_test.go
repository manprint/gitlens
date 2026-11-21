package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
)

const qtagTrace = "5bd66ef5095369c7b0d1f8f4bd33716a"

const qtagStatement = `SELECT * FROM nope /*application='shop',traceparent='00-` + qtagTrace + `-c532cb4098ac3dd2-01'*/`

// TestQTAG011: a log entry whose statement carries traceparent ships the
// trace id label; with statement_text off the statement itself stays home.
// An automatic plan carries the id of its Query Text too.
func TestQTAG011(t *testing.T) {
	line := jsonLine(1, "ERROR", "42P01", `relation "nope" does not exist`, qtagStatement)
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, line)
	h.loop.scrape(context.Background())
	f := h.results[0].Facts[0]
	require.Equal(t, qtagTrace, f.Labels["trace_id"])
	require.NotContains(t, factBody(t, f), "statement")
	raw, err := json.Marshal(h.results)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "nope /*")

	b, _ := json.Marshal(map[string]any{"Query Text": qtagStatement, "Plan": map[string]any{"Node Type": "Result"}})
	h = newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(1, 0, 77, "duration: 0.5 ms  plan:\n"+string(b)))
	h.loop.scrape(context.Background())
	plans := planFacts(h.results[0])
	require.Len(t, plans, 1)
	require.Equal(t, qtagTrace, plans[0].Labels["trace_id"])

	h = newLogHarness(t, jsonSettings, agent.CheckConfig{}, jsonLine(1, "ERROR", "42P01", "x", "SELECT 1"))
	h.loop.scrape(context.Background())
	require.NotContains(t, h.results[0].Facts[0].Labels, "trace_id")
}
