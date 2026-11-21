package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pglog"
	"github.com/manprint/pglens/internal/wire"
)

type memSource struct {
	name  string
	data  []byte
	reads int
}

func (m *memSource) Current(context.Context) (string, int64, error) {
	return m.name, int64(len(m.data)), nil
}

func (m *memSource) Size(_ context.Context, name string) (int64, bool, error) {
	if name != m.name {
		return 0, false, nil
	}
	return int64(len(m.data)), true, nil
}

func (m *memSource) ReadAt(_ context.Context, _ string, off, n int64) ([]byte, error) {
	m.reads++
	end := min(off+n, int64(len(m.data)))
	return m.data[off:end], nil
}

func jsonLine(line int, severity, sqlstate, msg, statement string) string {
	b, _ := json.Marshal(map[string]any{
		"timestamp": "2026-11-21 10:00:00.000 UTC", "pid": 42, "session_id": "s1", "line_num": line,
		"error_severity": severity, "state_code": sqlstate, "message": msg, "statement": statement,
		"dbname": "app", "user": "u", "application_name": "psql",
	})
	return string(b) + "\n"
}

type logHarness struct {
	loop    *logLoop
	src     *memSource
	results []wire.Result
}

func newLogHarness(t *testing.T, s agent.LogSettings, cfg agent.CheckConfig, lines ...string) *logHarness {
	t.Helper()
	state := filepath.Join(t.TempDir(), "pglog-state.json")
	src := &memSource{name: "log/postgresql.json", data: []byte(strings.Join(lines, ""))}
	require.NoError(t, pglog.SavePositions(state, map[string]pglog.Position{"db": {File: src.name}}))
	h := &logHarness{src: src}
	h.loop = &logLoop{
		target: "db", cfg: cfg, state: &logState{path: state}, clk: clock.NewFake(time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)),
		settings: func(context.Context) (agent.LogSettings, error) { return s, nil },
		source:   func(string, agent.LogSettings) (pglog.Source, string) { return src, "" },
		emit:     func(r wire.Result) { h.results = append(h.results, r) },
		logf:     func(string, ...any) {},
	}
	return h
}

var jsonSettings = agent.LogSettings{Destination: "jsonlog", Collector: "on", Timezone: "UTC", Messages: "C"}

func metric(r wire.Result, name, class string) (float64, bool) {
	for _, m := range r.Metrics {
		if m.Name == name && m.Labels["class"] == class {
			return m.Value, true
		}
	}
	return 0, false
}

func boolp(b bool) *bool { return &b }

// PGL-017
func TestPGL017ScrapeCountsShipsAndSaves(t *testing.T) {
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{},
		jsonLine(1, "ERROR", "23505", `duplicate key value violates unique constraint "t_pkey"`, "INSERT INTO t VALUES (1)"),
		jsonLine(2, "LOG", "00000", "duration: 1200.000 ms  plan:\nQuery Text: SELECT 1", ""),
		jsonLine(3, "LOG", "00000", "connection received: host=[local]", ""),
	)
	h.loop.scrape(context.Background())
	require.Len(t, h.results, 2, "the result and the note of the text-format plan")
	require.Equal(t, autoExplainFormatNotJSON, h.results[1].SkipReason)
	r := h.results[0]
	require.Equal(t, logsCheck, r.Check)
	v, ok := metric(r, "pg_log_events", pglog.ClassErrorStatement)
	require.True(t, ok)
	require.Equal(t, 1.0, v)
	v, ok = metric(r, "pg_log_events", pglog.ClassAutoExplainPlan)
	require.True(t, ok)
	require.Equal(t, 1.0, v)
	require.Len(t, r.Facts, 1, "only shipped classes become facts")
	f := r.Facts[0]
	require.Equal(t, logEventKind, f.Kind)
	require.Equal(t, "s1/1", f.Key)
	require.Equal(t, pglog.ClassErrorStatement, f.Labels["class"])
	require.Equal(t, "23505", f.Labels["sqlstate"])
	require.Equal(t, "2026-11-21T10:00:00Z", f.Labels["ts"])
	for _, name := range []string{"pg_log_panic_events", "pg_log_corruption_events", "pg_log_disk_full_events", "pg_log_oom_events", "pg_log_events_dropped"} {
		v, ok := metric(r, name, "")
		require.True(t, ok, name)
		require.Zero(t, v, name)
	}
	read, _ := metric(r, "pg_log_bytes_read", "")
	require.Equal(t, float64(len(h.src.data)), read)

	saved, err := pglog.LoadPositions(h.loop.state.path)
	require.NoError(t, err)
	require.Equal(t, pglog.Position{File: h.src.name, Offset: int64(len(h.src.data))}, saved["db"])
}

// PGL-018
func TestPGL018FactCap(t *testing.T) {
	lines := make([]string, 0, logFactsPerScrape+1)
	for i := range logFactsPerScrape + 1 {
		lines = append(lines, jsonLine(i+1, "ERROR", "22012", "division by zero", ""))
	}
	h := newLogHarness(t, jsonSettings, agent.CheckConfig{}, lines...)
	h.loop.scrape(context.Background())
	require.Len(t, h.results, 1)
	require.Len(t, h.results[0].Facts, logFactsPerScrape)
	v, _ := metric(h.results[0], "pg_log_events_dropped", "")
	require.Equal(t, 1.0, v)
	v, _ = metric(h.results[0], "pg_log_events", pglog.ClassErrorStatement)
	require.Equal(t, float64(logFactsPerScrape+1), v)
}

func factBody(t *testing.T, f wire.Fact) map[string]string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(f.ValueJSON, &body))
	return body
}

// PGL-019
func TestPGL019StatementText(t *testing.T) {
	line := jsonLine(1, "ERROR", "42P01", `relation "nope" does not exist`, "SELECT * FROM nope")
	for _, on := range []bool{false, true} {
		t.Run(fmt.Sprint(on), func(t *testing.T) {
			cfg := agent.CheckConfig{}
			if on {
				cfg.StatementText = boolp(true)
			}
			h := newLogHarness(t, jsonSettings, cfg, line)
			h.loop.scrape(context.Background())
			body := factBody(t, h.results[0].Facts[0])
			require.Equal(t, `relation "nope" does not exist`, body["message"])
			if on {
				require.Equal(t, "SELECT * FROM nope", body["statement"])
			} else {
				require.NotContains(t, body, "statement")
			}
		})
	}
}

// PGL-020
func TestPGL020CredentialWithheld(t *testing.T) {
	cfg := agent.CheckConfig{StatementText: boolp(true)}
	h := newLogHarness(t, jsonSettings, cfg,
		jsonLine(1, "ERROR", "42704", `role "x" does not exist`, "ALTER ROLE x PASSWORD 'secret'"))
	h.loop.scrape(context.Background())
	f := h.results[0].Facts[0]
	require.Equal(t, logWithheld, factBody(t, f)["statement"])
	raw, err := json.Marshal(h.results[0])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
}

// PGL-021
func TestPGL021NoStructuredFormat(t *testing.T) {
	s := jsonSettings
	s.Destination = "stderr"
	h := newLogHarness(t, s, agent.CheckConfig{}, jsonLine(1, "ERROR", "22012", "division by zero", ""))
	h.loop.scrape(context.Background())
	require.Len(t, h.results, 1)
	require.Equal(t, logsNoStructuredFormat, h.results[0].SkipReason)
	require.Empty(t, h.results[0].Metrics)
	require.Zero(t, h.src.reads)
}

func TestLogLoopNotes(t *testing.T) {
	s := jsonSettings
	s.Messages = "it_IT.UTF-8"
	h := newLogHarness(t, s, agent.CheckConfig{}, jsonLine(1, "ERROR", "22012", "divisione per zero", ""))
	h.loop.scrape(context.Background())
	require.Len(t, h.results, 2)
	require.Equal(t, logsMessagesNotEnglish, h.results[1].SkipReason)

	s = jsonSettings
	s.Timezone = "Mars/Olympus"
	h = newLogHarness(t, s, agent.CheckConfig{}, jsonLine(1, "ERROR", "22012", "division by zero", ""))
	h.loop.scrape(context.Background())
	require.Len(t, h.results, 2)
	require.Equal(t, logsTimezoneUnknown, h.results[1].SkipReason)

	h = newLogHarness(t, jsonSettings, agent.CheckConfig{})
	h.loop.source = func(string, agent.LogSettings) (pglog.Source, string) { return nil, logsFileUnreadable }
	h.loop.scrape(context.Background())
	require.Equal(t, logsFileUnreadable, h.results[0].SkipReason)

	src := newLogSource(nil, agent.CheckConfig{}, false, func() string { return "/x" })
	got, note := src(pglog.FormatJSON, jsonSettings)
	require.Nil(t, got)
	require.Equal(t, logsFileUnreadable, note)
}

func TestLogStateSharedAcrossTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pglog-state.json")
	a, b := sharedLogState(path), sharedLogState(path)
	require.Same(t, a, b)
	_, ok := a.get("one", t.Logf)
	require.False(t, ok)
	require.NoError(t, a.set("one", pglog.Position{File: "x", Offset: 1}))
	require.NoError(t, b.set("two", pglog.Position{File: "y", Offset: 2}))
	saved, err := pglog.LoadPositions(path)
	require.NoError(t, err)
	require.Equal(t, map[string]pglog.Position{"one": {File: "x", Offset: 1}, "two": {File: "y", Offset: 2}}, saved)
}
