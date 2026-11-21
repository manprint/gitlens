package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/wire"
)

func autoExplainMessage(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../internal/pglog/testdata/" + name)
	require.NoError(t, err)
	return string(raw)
}

// planLine is a jsonlog auto_explain line; second shifts its time.
func planLine(line, second int, queryID int64, msg string) string {
	b, _ := json.Marshal(map[string]any{
		"timestamp": fmt.Sprintf("2026-11-21 10:00:%02d.000 UTC", second%60),
		"pid":       42, "session_id": "s1", "line_num": line,
		"error_severity": "LOG", "state_code": "00000", "message": msg,
		"dbname": "app", "user": "u", "query_id": queryID,
	})
	return string(b) + "\n"
}

func planFacts(r wire.Result) []wire.Fact {
	var out []wire.Fact
	for _, f := range r.Facts {
		if f.Kind == planFactKind {
			out = append(out, f)
		}
	}
	return out
}

func planDropped(r wire.Result, reason string) float64 {
	for _, m := range r.Metrics {
		if m.Name == "pg_log_plans_dropped" && m.Labels["reason"] == reason {
			return m.Value
		}
	}
	return -1
}

func notes(results []wire.Result) []string {
	var out []string
	for _, r := range results {
		if r.SkipReason != "" {
			out = append(out, r.SkipReason)
		}
	}
	return out
}

// AEX-003: the same plan twice within an hour ships its body once; both
// samples carry their duration.
func TestAEX003(t *testing.T) {
	msg := autoExplainMessage(t, "autoexplain_json.txt")
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(1, 0, 77, msg), planLine(2, 1, 77, msg))
	h.loop.scrape(context.Background())
	require.NotEmpty(t, h.results)
	facts := planFacts(h.results[0])
	require.Len(t, facts, 2)

	var body struct {
		Plan []map[string]json.RawMessage `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(facts[0].ValueJSON, &body))
	require.Len(t, body.Plan, 1)
	require.Contains(t, body.Plan[0], "Plan")
	require.JSONEq(t, `{}`, string(facts[1].ValueJSON))

	hash, err := agent.PlanHash(json.RawMessage(mustField(t, facts[0].ValueJSON, "plan")))
	require.NoError(t, err)
	for i, f := range facts {
		require.Equal(t, "0.01", f.Labels["duration_ms"], i)
		require.Equal(t, "auto_explain", f.Labels["source"])
		require.Equal(t, "app", f.Labels["datname"])
		require.Equal(t, "77", f.Labels["queryid"])
		require.Equal(t, "false", f.Labels["analyzed"])
		require.Equal(t, hash, f.Labels["plan_hash"])
		require.True(t, strings.HasPrefix(f.Key, "77:"+hash+":"), f.Key)
	}
	require.NotEqual(t, facts[0].Key, facts[1].Key)
	require.Equal(t, "2026-11-21T10:00:01Z", facts[1].Labels["ts"])

	// An hour later the body ships again.
	h.src.data = append(h.src.data, planLine(3, 2, 77, msg)...)
	h.loop.clk.(interface{ Advance(time.Duration) }).Advance(time.Hour)
	h.loop.scrape(context.Background())
	last := h.results[len(h.results)-1]
	facts = planFacts(last)
	require.Len(t, facts, 1)
	require.Contains(t, string(facts[0].ValueJSON), `"plan"`)
}

func mustField(t *testing.T, raw json.RawMessage, key string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	return m[key]
}

// AEX-004: at most 50 plan facts per scrape; a line without query_id is
// dropped and counted.
func TestAEX004(t *testing.T) {
	msg := autoExplainMessage(t, "autoexplain_json.txt")
	var lines []string
	for i := range 51 {
		lines = append(lines, planLine(i+1, i, 77, msg))
	}
	lines = append(lines, planLine(52, 0, 0, msg))
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, lines...)
	h.loop.scrape(context.Background())
	r := h.results[0]
	require.Len(t, planFacts(r), 50)
	require.Equal(t, 1.0, planDropped(r, "budget"))
	require.Equal(t, 1.0, planDropped(r, "no_queryid"))
	require.Equal(t, 0.0, planDropped(r, "credential"))
	require.Equal(t, 0.0, planDropped(r, "format"))
	read, ok := metric(r, "pg_log_plans", "")
	require.True(t, ok)
	require.Equal(t, 52.0, read)
}

// AEX-005: a plan in text format is dropped with reason format and the loop
// reports the note auto_explain_format_not_json.
func TestAEX005(t *testing.T) {
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(1, 0, 77, autoExplainMessage(t, "autoexplain_text.txt")))
	h.loop.scrape(context.Background())
	require.Empty(t, planFacts(h.results[0]))
	require.Equal(t, 1.0, planDropped(h.results[0], "format"))
	require.Equal(t, []string{autoExplainFormatNotJSON}, notes(h.results))
}

// AEX-006: a sample whose statement may hold a credential ships nothing (I-4).
func TestAEX006(t *testing.T) {
	msg := "duration: 0.5 ms  plan:\n" + `{"Query Text": "ALTER ROLE app PASSWORD 'hunter2secret'", "Plan": {"Node Type": "Result"}}`
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{StatementText: boolp(true)}, planLine(1, 0, 77, msg))
	h.loop.scrape(context.Background())
	r := h.results[0]
	require.Empty(t, planFacts(r))
	require.Equal(t, 1.0, planDropped(r, "credential"))
	raw, err := json.Marshal(h.results)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hunter2secret")
}
