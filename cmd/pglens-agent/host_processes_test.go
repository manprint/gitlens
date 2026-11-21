package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

// fakeProcesses serves one list of samples per call to Processes.
type fakeProcesses struct {
	names   map[int]string
	parents map[int]int
	ticks   [][]host.ProcessSample
	calls   int
}

func (f *fakeProcesses) ProcessName(pid int) (string, error) { return f.names[pid], nil }
func (f *fakeProcesses) ParentPID(pid int) (int, error)      { return f.parents[pid], nil }
func (f *fakeProcesses) Processes(int) ([]host.ProcessSample, error) {
	s := f.ticks[min(f.calls, len(f.ticks)-1)]
	f.calls++
	return s, nil
}

type fakeProcessQuerier struct {
	backend  int
	sessions map[int]processSession
	queries  int
}

func (q *fakeProcessQuerier) BackendPID(context.Context) (int, error) {
	q.queries++
	return q.backend, nil
}

func (q *fakeProcessQuerier) Activity(context.Context) (map[int]processSession, error) {
	q.queries++
	return q.sessions, nil
}

func u64p(v uint64) *uint64 { return &v }

func processFixture() (*fakeProcesses, *fakeProcessQuerier) {
	src := &fakeProcesses{
		names:   map[int]string{100: "postgres", 101: "postgres", 102: "postgres"},
		parents: map[int]int{101: 100, 102: 100},
		ticks: [][]host.ProcessSample{
			{
				{PID: 100, StartTicks: 1, CPUTicks: 100, RSSBytes: 10 << 20},
				{PID: 101, StartTicks: 2, CPUTicks: 1000, RSSBytes: 50 << 20, ReadBytes: u64p(0), WriteBytes: u64p(0)},
				{PID: 102, StartTicks: 3, CPUTicks: 0, RSSBytes: 5 << 20},
			},
			{
				{PID: 100, StartTicks: 1, CPUTicks: 110, RSSBytes: 10 << 20},
				{PID: 101, StartTicks: 2, CPUTicks: 1500, RSSBytes: 60 << 20, ReadBytes: u64p(10 << 20), WriteBytes: u64p(0)},
				{PID: 102, StartTicks: 3, CPUTicks: 0, RSSBytes: 5 << 20},
			},
		},
	}
	qid := int64(-42)
	q := &fakeProcessQuerier{backend: 102, sessions: map[int]processSession{
		101: {BackendType: "client backend", Datname: "app", Usename: "web", ApplicationName: "shop", State: "active", QueryID: &qid},
		102: {BackendType: "client backend", Datname: "postgres", Usename: "pglens", State: "active"},
	}}
	return src, q
}

func processConfig(t *testing.T, processes string, dsn string) *agent.Config {
	t.Helper()
	cfg := &agent.Config{Targets: []agent.TargetConfig{{Name: "pg", DSN: dsn}}}
	if processes == "off" {
		off := false
		cfg.Host.Processes.Enabled = &off
	}
	return cfg
}

func TestPROC007_TwoIntervalsShipRatesWithSessions(t *testing.T) {
	src, q := processFixture()
	var log bytes.Buffer
	cfg := processConfig(t, "", "postgres://pglens@localhost/postgres")
	trackers := processTrackers(cfg, src, func(int) processQuerier { return q }, newHourlyWarner(&log, time.Now))
	require.Len(t, trackers, 1)

	t0 := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	var got []wire.Result
	emit := func(target string, r wire.Result) {
		require.Equal(t, "pg", target)
		got = append(got, r)
	}
	collectProcesses(context.Background(), trackers, t0, emit)
	require.Empty(t, got, "the first interval is a baseline")

	collectProcesses(context.Background(), trackers, t0.Add(10*time.Second), emit)
	require.Len(t, got, 1)
	r := got[0]
	require.Equal(t, wire.HostProcessesCheck, r.Check)
	require.Equal(t, t0.Add(10*time.Second), r.TS)
	byPID := map[string]wire.Fact{}
	for _, f := range r.Facts {
		require.Equal(t, wire.HostProcessFact, f.Kind)
		byPID[f.Key] = f
	}
	require.Len(t, byPID, 3)

	backend := byPID["101"]
	require.Equal(t, map[string]string{
		"pid": "101", "backend_type": "client backend", "datname": "app", "usename": "web",
		"application_name": "shop", "state": "active", "queryid": "-42",
	}, backend.Labels)
	var v map[string]float64
	require.NoError(t, json.Unmarshal(backend.ValueJSON, &v))
	require.InDelta(t, 0.5, v["cpu_ratio"], 1e-9)
	require.Equal(t, float64(60<<20), v["rss_bytes"])
	require.Equal(t, float64(1<<20), v["read_bytes_per_s"])
	require.Contains(t, v, "write_bytes_per_s")

	pm := byPID["100"]
	require.Equal(t, map[string]string{"pid": "100", "backend_type": "postmaster"}, pm.Labels)
	var pv map[string]float64
	require.NoError(t, json.Unmarshal(pm.ValueJSON, &pv))
	require.NotContains(t, pv, "read_bytes_per_s", "unknown I/O is left out")
	for _, f := range r.Facts {
		require.NoError(t, f.Validate(), f.Key)
	}
	require.Empty(t, log.String())
}

func TestPROC008_DisabledProcessesQueryNothing(t *testing.T) {
	src, q := processFixture()
	cfg := processConfig(t, "off", "postgres://pglens@localhost/postgres")
	trackers := processTrackers(cfg, src, func(int) processQuerier { return q }, newHourlyWarner(&bytes.Buffer{}, time.Now))
	collectProcesses(context.Background(), trackers, time.Now(), func(string, wire.Result) { t.Fatal("fact shipped") })
	collectProcesses(context.Background(), trackers, time.Now().Add(time.Minute), func(string, wire.Result) { t.Fatal("fact shipped") })
	require.Zero(t, q.queries)
	require.Zero(t, src.calls)
}

func TestPROC009_RemoteTargetGetsNothing(t *testing.T) {
	src, q := processFixture()
	cfg := processConfig(t, "", "postgres://pglens@db.example.internal:5432/postgres")
	trackers := processTrackers(cfg, src, func(int) processQuerier { return q }, newHourlyWarner(&bytes.Buffer{}, time.Now))
	require.Empty(t, trackers)
	collectProcesses(context.Background(), trackers, time.Now(), func(string, wire.Result) { t.Fatal("fact shipped") })
	require.Zero(t, q.queries)
}

func TestPROC009_OtherNamespaceIsSkippedAndLoggedOnce(t *testing.T) {
	src, q := processFixture()
	src.names[102] = "bash" // pg_backend_pid names another program here
	var log bytes.Buffer
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	clockNow := now
	cfg := processConfig(t, "", "postgres://pglens@localhost/postgres")
	trackers := processTrackers(cfg, src, func(int) processQuerier { return q }, newHourlyWarner(&log, func() time.Time { return clockNow }))
	for i := 0; i < 3; i++ {
		collectProcesses(context.Background(), trackers, now, func(string, wire.Result) { t.Fatal("fact shipped") })
	}
	require.Equal(t, 1, bytes.Count(log.Bytes(), []byte("\n")), log.String())
	require.Contains(t, log.String(), "namespace")
	require.Zero(t, src.calls, "no /proc listing without a recognised postmaster")
	clockNow = now.Add(61 * time.Minute)
	collectProcesses(context.Background(), trackers, now, func(string, wire.Result) {})
	require.Equal(t, 2, bytes.Count(log.Bytes(), []byte("\n")))
}
