package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/wire"
)

func autovacuumFacts(r wire.Result) []wire.Fact {
	var out []wire.Fact
	for _, f := range r.Facts {
		if f.Kind == autovacuumRunKind {
			out = append(out, f)
		}
	}
	return out
}

func metricValue(r wire.Result, name string) float64 {
	for _, m := range r.Metrics {
		if m.Name == name {
			return m.Value
		}
	}
	return -1
}

// AVH-006: one autovacuum_run fact per run with its labels and numbers; over
// 500 runs in a scrape the rest are counted as dropped.
func TestAVH006(t *testing.T) {
	raw, err := os.ReadFile("../../internal/pglog/testdata/autovacuum_pg18_vacuum.txt")
	require.NoError(t, err)
	msg := strings.TrimRight(string(raw), "\n")

	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, planLine(7, 3, 0, msg))
	h.loop.scrape(context.Background())
	require.NotEmpty(t, h.results)
	facts := autovacuumFacts(h.results[0])
	require.Len(t, facts, 1)
	f := facts[0]
	require.Equal(t, "s1/7", f.Key)
	require.Equal(t, map[string]string{
		"ts": "2026-11-21T10:00:03Z", "datname": "postgres", "schemaname": "public", "relname": "avh",
		"kind": "vacuum", "aggressive": "false", "wraparound": "false",
	}, f.Labels)
	var value map[string]float64
	require.NoError(t, json.Unmarshal(f.ValueJSON, &value))
	require.Equal(t, 19900.0, value["tuples_removed"])
	require.Equal(t, 112061.0, value["wal_bytes"])
	require.Equal(t, 6.0, value["wal_fpi"])
	require.Contains(t, value, "elapsed_seconds")
	require.Contains(t, value, "index_scans")
	require.Len(t, value, 16)
	require.Zero(t, metricValue(h.results[0], "pg_log_autovacuum_dropped"))

	analyze := "automatic analyze of table \"app.public.t\"\nsystem usage: CPU: user: 0.01 s, system: 0.00 s, elapsed: 0.02 s"
	lines := make([]string, 0, 501)
	for i := range 501 {
		lines = append(lines, planLine(i+1, i, 0, analyze))
	}
	h = newLogHarness(t, jsonSettings, agent.CheckConfig{}, lines...)
	h.loop.scrape(context.Background())
	require.NotEmpty(t, h.results)
	facts = autovacuumFacts(h.results[0])
	require.Len(t, facts, 500)
	require.Equal(t, 1.0, metricValue(h.results[0], "pg_log_autovacuum_dropped"))
	require.JSONEq(t, `{"cpu_user_seconds":0.01,"cpu_system_seconds":0,"elapsed_seconds":0.02}`, string(facts[0].ValueJSON))
}
