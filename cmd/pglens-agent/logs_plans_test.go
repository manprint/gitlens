package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func paramsMessage(params string) string {
	b, _ := json.Marshal(map[string]any{
		"Query Text": "SELECT * FROM t WHERE id = $1", "Query Parameters": params,
		"Plan": map[string]any{"Node Type": "Result"},
	})
	return "duration: 0.5 ms  plan:\n" + string(b)
}

func shippedParameters(t *testing.T, f wire.Fact) (string, bool) {
	t.Helper()
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(f.ValueJSON, &body))
	raw, ok := body["parameters"]
	if !ok {
		return "", false
	}
	var s string
	require.NoError(t, json.Unmarshal(raw, &s))
	return s, true
}

// AEX-010: parameters are off by default: no parameters key even when the log
// has them, and the value is nowhere in the result.
func TestAEX010(t *testing.T) {
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(1, 0, 77, paramsMessage("$1 = '4242'")))
	h.loop.scrape(context.Background())
	facts := planFacts(h.results[0])
	require.Len(t, facts, 1)
	_, ok := shippedParameters(t, facts[0])
	require.False(t, ok)
	raw, err := json.Marshal(h.results)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "4242")
}

// AEX-011: with plan_parameters on, the parameters ship in the value only,
// cut to 4096 bytes on a character boundary.
func TestAEX011(t *testing.T) {
	on := agent.CheckConfig{PlanParameters: boolp(true)}
	h := newLogHarness(t, jsonSettings, on, planLine(1, 0, 77, paramsMessage("$1 = '4242'")))
	h.loop.scrape(context.Background())
	facts := planFacts(h.results[0])
	require.Len(t, facts, 1)
	got, ok := shippedParameters(t, facts[0])
	require.True(t, ok)
	require.Equal(t, "$1 = '4242'", got)
	for k, v := range facts[0].Labels {
		require.NotContains(t, v, "4242", k)
	}
	require.NotContains(t, facts[0].Key, "4242")

	long := "$1 = '" + strings.Repeat("é", 3000) + "'"
	h = newLogHarness(t, jsonSettings, on, planLine(1, 0, 78, paramsMessage(long)))
	h.loop.scrape(context.Background())
	got, ok = shippedParameters(t, planFacts(h.results[0])[0])
	require.True(t, ok)
	require.LessOrEqual(t, len(got), planParametersMaxBytes)
	require.Greater(t, len(got), planParametersMaxBytes-2)
	require.True(t, strings.HasPrefix(long, got))
	require.True(t, utf8.ValidString(got))
}

// AEX-012: shipped parameters judged a credential drop the sample (I-4); with
// parameters off the same line ships its sample without them.
func TestAEX012(t *testing.T) {
	secret := "$1 = 'secret-token-ab12cd34ef56ab12cd34ef56ab12cd34', $2 = 'PASSWORD hunter2secret'"
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{PlanParameters: boolp(true)}, planLine(1, 0, 77, paramsMessage(secret)))
	h.loop.scrape(context.Background())
	r := h.results[0]
	require.Empty(t, planFacts(r))
	require.Equal(t, 1.0, planDropped(r, "credential"))
	raw, err := json.Marshal(h.results)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hunter2secret")

	h = newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(1, 0, 77, paramsMessage(secret)))
	h.loop.scrape(context.Background())
	require.Len(t, planFacts(h.results[0]), 1)
	raw, err = json.Marshal(h.results)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hunter2secret")
}
