package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/host"
	"github.com/manprint/pglens/internal/wire"
)

// groupTargetsByDataDir buckets the local targets by their data directory,
// so a directory shared by several targets is measured once. A target whose
// directory is not known yet goes in the "" bucket: it still gets cpu, load
// and memory, without disk fields. Each bucket lists its targets sorted.
func groupTargetsByDataDir(targets []string, dirs map[string]string) map[string][]string {
	buckets := make(map[string][]string)
	for _, name := range targets {
		dir := dirs[name]
		buckets[dir] = append(buckets[dir], name)
	}
	for dir := range buckets {
		sort.Strings(buckets[dir])
	}
	return buckets
}

// diskWarner logs why a target has no disk metrics, once per target and
// reason, so a missing mount is reported without a line every tick.
type diskWarner struct {
	out  io.Writer
	mu   sync.Mutex
	seen map[string]string // target -> last reason logged
}

func newDiskWarner(out io.Writer) *diskWarner {
	return &diskWarner{out: out, seen: make(map[string]string)}
}

// warn logs reason for target unless it is the reason logged last. An empty
// reason means the disk was measured: the next failure is logged again.
func (w *diskWarner) warn(target, reason string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if reason == "" {
		delete(w.seen, target)
		return
	}
	if w.seen[target] == reason {
		return
	}
	w.seen[target] = reason
	_, _ = fmt.Fprintf(w.out, "warning: host disk metrics unavailable for target %s: %s (mount the data directory read-only at the same path in the agent container)\n", target, reason)
}

// hostDataDirs asks each local target for its data directory. A role that
// may not read it is reported once and left without disk metrics; any other
// failure (a target still starting) is retried on the next tick silently.
// The targets are asked in parallel, so unreachable ones delay the tick by
// one timeout, not one each.
func hostDataDirs(ctx context.Context, managers map[string]*agent.Manager, warner *diskWarner) map[string]string {
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	dirs := make(map[string]string, len(managers))
	for name, mgr := range managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir, err := mgr.DataDirectory(ctx)
			if err != nil {
				if errors.Is(err, agent.ErrDataDirNotReadable) {
					warner.warn(name, err.Error())
				}
				return
			}
			mu.Lock()
			dirs[name] = dir
			mu.Unlock()
		}()
	}
	wg.Wait()
	return dirs
}

// collectHostByDataDir collects one sample per tick (CPU, load, memory) and
// measures each data directory's filesystem on top of it, handing the result
// to every target of that directory. A failed collection skips the tick.
func collectHostByDataDir(ctx context.Context, collector *host.Collector, buckets map[string][]string, warner *diskWarner, errOut io.Writer, emit func(target string, s host.Sample)) {
	if len(buckets) == 0 {
		return
	}
	base, err := collector.Collect(ctx, "")
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "warning: host collection: %v\n", err)
		return
	}
	dirs := make([]string, 0, len(buckets))
	for dir := range buckets {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		sample := collector.WithDisk(ctx, base, dir)
		for _, target := range buckets[dir] {
			if dir != "" {
				warner.warn(target, sample.DiskUnavailable)
			}
			emit(target, sample)
		}
	}
}

// processLabelMaxBytes cuts free-text session labels, as ASH does.
const processLabelMaxBytes = 64

// processSession is the pg_stat_activity row of one process.
type processSession = agent.ProcessSession

// processSource reads processes from /proc (host.Collector).
type processSource interface {
	ProcessName(pid int) (string, error)
	ParentPID(pid int) (int, error)
	Processes(postmaster int) ([]host.ProcessSample, error)
}

// processQuerier asks the target for its own backend pid and its sessions.
type processQuerier interface {
	BackendPID(ctx context.Context) (int, error)
	Activity(ctx context.Context) (map[int]processSession, error)
}

type managerProcessQuerier struct{ m *agent.Manager }

func (q managerProcessQuerier) BackendPID(ctx context.Context) (int, error) {
	return q.m.BackendPID(ctx)
}

func (q managerProcessQuerier) Activity(ctx context.Context) (map[int]processSession, error) {
	return q.m.ProcessSessions(ctx)
}

// hourlyWarner logs one line per target and error kind per hour, so a target
// whose processes cannot be read does not flood the log.
type hourlyWarner struct {
	out  io.Writer
	now  func() time.Time
	mu   sync.Mutex
	last map[string]time.Time
}

func newHourlyWarner(out io.Writer, now func() time.Time) *hourlyWarner {
	return &hourlyWarner{out: out, now: now, last: make(map[string]time.Time)}
}

func (w *hourlyWarner) warn(target, kind string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := target + "\x00" + kind
	now := w.now()
	if at, ok := w.last[key]; ok && now.Sub(at) < time.Hour {
		return
	}
	w.last[key] = now
	_, _ = fmt.Fprintf(w.out, "warning: host processes of target %s skipped (%s): %v\n", target, kind, err)
}

// isPostgresProcess recognises a PostgreSQL server process by its executable
// name. A pid from pg_backend_pid that names another program means the agent
// sees a different PID namespace than the server (PostgreSQL in another
// container), and its /proc must not be read as the server's.
func isPostgresProcess(name string) bool {
	return name == "postgres" || name == "postmaster"
}

// processTracker follows the processes of one target between host intervals.
type processTracker struct {
	target     string
	src        processSource
	q          processQuerier
	topN       int
	warn       *hourlyWarner
	postmaster int // cached; 0 asks the target again
	prev       []host.ProcessSample
	prevAt     time.Time
}

// tick reads the target's processes and returns one host_processes result
// with the rates since the previous tick. The first tick, and any tick after
// a failure, only records a baseline and ships nothing.
func (t *processTracker) tick(ctx context.Context, now time.Time) (wire.Result, bool) {
	if t.postmaster == 0 {
		pm, kind, err := t.findPostmaster(ctx)
		if err != nil {
			t.warn.warn(t.target, kind, err)
			t.prev = nil
			return wire.Result{}, false
		}
		t.postmaster = pm
	}
	samples, err := t.src.Processes(t.postmaster)
	if err == nil && (len(samples) == 0 || samples[0].PID != t.postmaster) {
		err = fmt.Errorf("postmaster %d is gone", t.postmaster)
	}
	if err != nil {
		t.warn.warn(t.target, "proc", err)
		t.postmaster, t.prev = 0, nil
		return wire.Result{}, false
	}
	var rates []host.ProcessRate
	if t.prev != nil {
		rates = host.ProcessRates(t.prev, samples, now.Sub(t.prevAt).Seconds())
	}
	t.prev, t.prevAt = samples, now
	if len(rates) == 0 {
		return wire.Result{}, false
	}
	top := host.TopProcesses(rates, t.topN, t.postmaster)
	sessions, err := t.q.Activity(ctx)
	if err != nil {
		t.warn.warn(t.target, "connection", err)
		return wire.Result{}, false
	}
	r := wire.Result{Check: wire.HostProcessesCheck, TS: now}
	for _, p := range top {
		if f, ok := processFact(p, sessions, t.postmaster); ok {
			r.Facts = append(r.Facts, f)
		}
	}
	return r, len(r.Facts) > 0
}

func (t *processTracker) findPostmaster(ctx context.Context) (int, string, error) {
	backend, err := t.q.BackendPID(ctx)
	if err != nil {
		return 0, "connection", err
	}
	name, err := t.src.ProcessName(backend)
	if err != nil {
		return 0, "proc", fmt.Errorf("backend %d: %w", backend, err)
	}
	if !isPostgresProcess(name) {
		return 0, "namespace", fmt.Errorf("pid %d of pg_backend_pid is not a PostgreSQL process in the agent's /proc (PostgreSQL runs in another PID namespace)", backend)
	}
	pm, err := t.src.ParentPID(backend)
	if err != nil {
		return 0, "proc", fmt.Errorf("backend %d: %w", backend, err)
	}
	if name, err := t.src.ProcessName(pm); err != nil || !isPostgresProcess(name) {
		return 0, "namespace", fmt.Errorf("parent %d of backend %d is not the postmaster", pm, backend)
	}
	return pm, "", nil
}

// processFact turns one process rate into a host_process fact. The command
// line and every path stay on the host: only the pid, the session labels and
// the numbers are shipped.
func processFact(p host.ProcessRate, sessions map[int]processSession, postmaster int) (wire.Fact, bool) {
	s, ok := sessions[p.PID]
	if p.PID == postmaster && s.BackendType == "" {
		s.BackendType = "postmaster"
	} else if !ok {
		s.BackendType = "other"
	}
	labels := map[string]string{
		"pid":              strconv.Itoa(p.PID),
		"backend_type":     cutUTF8(s.BackendType, processLabelMaxBytes),
		"datname":          cutUTF8(s.Datname, processLabelMaxBytes),
		"usename":          cutUTF8(s.Usename, processLabelMaxBytes),
		"application_name": cutUTF8(s.ApplicationName, processLabelMaxBytes),
		"state":            cutUTF8(s.State, processLabelMaxBytes),
	}
	if s.QueryID != nil && *s.QueryID != 0 {
		labels["queryid"] = strconv.FormatInt(*s.QueryID, 10)
	}
	for k, v := range labels {
		if v == "" {
			delete(labels, k)
		}
	}
	body := map[string]any{"cpu_ratio": p.CPURatio, "rss_bytes": p.RSSBytes}
	if p.ReadBytesPerS != nil {
		body["read_bytes_per_s"] = *p.ReadBytesPerS
	}
	if p.WriteBytesPerS != nil {
		body["write_bytes_per_s"] = *p.WriteBytesPerS
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return wire.Fact{}, false
	}
	return wire.Fact{Kind: wire.HostProcessFact, Key: strconv.Itoa(p.PID), Labels: labels, ValueJSON: raw}, true
}

// processTrackers builds one tracker per host-local target when
// host.processes is on; remote targets never get one.
func processTrackers(cfg *agent.Config, src processSource, querier func(i int) processQuerier, warn *hourlyWarner) map[string]*processTracker {
	if !cfg.HostProcessesEnabled() {
		return nil
	}
	out := make(map[string]*processTracker)
	for i, tc := range cfg.Targets {
		if !agent.TargetIsLocal(tc) {
			continue
		}
		out[tc.Name] = &processTracker{target: tc.Name, src: src, q: querier(i), topN: cfg.HostProcessesTopN(), warn: warn}
	}
	return out
}

// collectProcesses ticks every tracker, in target order, handing each
// result to emit.
func collectProcesses(ctx context.Context, trackers map[string]*processTracker, now time.Time, emit func(target string, r wire.Result)) {
	names := make([]string, 0, len(trackers))
	for name := range trackers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if r, ok := trackers[name].tick(ctx, now); ok {
			emit(name, r)
		}
	}
}
