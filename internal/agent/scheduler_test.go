package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// mockConn is a minimal check.Conn implementation
type mockConn struct {
	released bool
}

func (m *mockConn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (m *mockConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}
func (m *mockConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (m *mockConn) Release() {
	m.released = true
}

// mockTarget is a minimal check.Target for testing
type mockTarget struct {
	name string
	conn *mockConn
}

func (m *mockTarget) InstanceID() pgtype.InstanceID { return pgtype.InstanceID{} }
func (m *mockTarget) ClusterID() pgtype.ClusterID   { return 0 }
func (m *mockTarget) Role() pgtype.Role             { return pgtype.RolePrimary }
func (m *mockTarget) PGVersion() pgtype.PGVersion   { return 160000 }
func (m *mockTarget) PermTier() pgtype.PermTier     { return pgtype.TierReadOnly }
func (m *mockTarget) HasExtension(name string) bool { return false }
func (m *mockTarget) Conn(ctx context.Context) (check.Conn, error) {
	return m.conn, nil
}
func (m *mockTarget) ConnFor(ctx context.Context, datname string) (check.Conn, error) {
	return m.conn, nil
}
func (m *mockTarget) Database() string { return m.name }
func (m *mockTarget) Clock() clock.Clock {
	return clock.System()
}

// mockCheck is a minimal check.Check for testing
type mockCheck struct {
	name       string
	interval   time.Duration
	timeout    time.Duration
	callCnt    int
	scrapedDBs []string         // t.Database() as seen by each Scrape call, in order
	queryTexts map[int64]string // returned verbatim in check.Result.QueryTexts, if set
	statsReset *time.Time       // returned verbatim in check.Result.StatsReset, if set
	partial    bool             // returned verbatim in check.Result.Partial
	mu         sync.Mutex
}

func (m *mockCheck) Name() string {
	return m.name
}
func (m *mockCheck) Requires() check.Requirements {
	return check.Requirements{}
}
func (m *mockCheck) DefaultInterval() time.Duration {
	return m.interval
}
func (m *mockCheck) Timeout() time.Duration {
	return m.timeout
}
func (m *mockCheck) Scrape(ctx context.Context, t check.Target) (check.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCnt++
	m.scrapedDBs = append(m.scrapedDBs, t.Database())
	return check.Result{QueryTexts: m.queryTexts, StatsReset: m.statsReset, Partial: m.partial}, nil
}

// TestScheduler_RunsAtInterval tests that a check runs at its scheduled interval
func TestScheduler_RunsAtInterval(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	check := &mockCheck{name: "test_check", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	target := &mockTarget{name: "target1", conn: &mockConn{}}
	s.AddEntry(target, check, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Collect at least one run
	runCount := 0
	timeout := time.After(2 * time.Second)
	for runCount < 1 {
		select {
		case <-results:
			runCount++
		case <-timeout:
			t.Fatalf("timeout waiting for runs, got %d", runCount)
		}
	}

	s.Stop()

	if runCount < 1 {
		t.Errorf("expected at least 1 run, got %d", runCount)
	}
}

func TestScheduler_AddEntryWithIntervalOverride(t *testing.T) {
	s := NewScheduler(ScheduleOptions{
		MaxWorkers: 1, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 5 * time.Second,
	})
	mc := &mockCheck{name: "overridden", interval: time.Hour, timeout: time.Second}
	s.AddEntryWithInterval(&mockTarget{name: "target", conn: &mockConn{}}, mc, "", 20*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	select {
	case <-s.Results():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("configured interval was not applied")
	}
}

// TestScheduler_DatabaseScopedEntry_OverridesTargetDatabase proves a
// ScopeDatabase entry's Scrape() sees the entry's own discovered database
// name via t.Database(), not the target's own name — the two are
// genuinely different strings in any real multi-database deployment (found
// live: target "pg", monitored database "postgres"; stat_statements.go's
// own `t.ConnFor(ctx, t.Database())` silently tried to connect to a
// database literally named "pg", which doesn't exist, failing every scrape
// with no visible log).
func TestScheduler_DatabaseScopedEntry_OverridesTargetDatabase(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	mc := &mockCheck{name: "db_scoped_check", interval: 50 * time.Millisecond, timeout: 50 * time.Millisecond}
	target := &mockTarget{name: "pg", conn: &mockConn{}}
	s.AddEntry(target, mc, "postgres")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()
	select {
	case res := <-results:
		if res.Err != nil {
			t.Fatalf("unexpected scrape error: %v", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a run")
	}
	s.Stop()

	mc.mu.Lock()
	defer mc.mu.Unlock()
	if len(mc.scrapedDBs) == 0 {
		t.Fatal("Scrape was never called")
	}
	if got := mc.scrapedDBs[0]; got != "postgres" {
		t.Fatalf("expected Scrape's target.Database() to report the entry's own database %q, got %q (the target's own name)", "postgres", got)
	}
}

// TestScheduler_PropagatesQueryTexts proves check.Result.QueryTexts reaches
// EntryResult unchanged — found live re-verifying phase 7.5's README:
// stat_statements.go computed real query texts all along, but EntryResult
// never carried the field at all, so /api/v1/ash/top's query_text join
// always returned "" no matter how correctly the check itself ran.
func TestScheduler_PropagatesQueryTexts(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	want := map[int64]string{42: "SELECT 1"}
	mc := &mockCheck{name: "stat_statements", interval: 50 * time.Millisecond, timeout: 50 * time.Millisecond, queryTexts: want}
	target := &mockTarget{name: "pg", conn: &mockConn{}}
	s.AddEntry(target, mc, "postgres")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-s.Results():
		if res.Err != nil {
			t.Fatalf("unexpected scrape error: %v", res.Err)
		}
		if len(res.QueryTexts) != 1 || res.QueryTexts[42] != "SELECT 1" {
			t.Fatalf("expected QueryTexts to propagate unchanged, got %v", res.QueryTexts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a run")
	}
	s.Stop()
}

// TestScheduler_PropagatesStatsReset proves check.Result.StatsReset reaches
// EntryResult unchanged — the same class of gap as QueryTexts (row 142):
// stat_statements.go has always computed this from
// pg_stat_statements_info.reset_time, but nothing carried it past
// scrapeEntry, so SYS-RESET-002's counter_reset_detected event had no
// explicit reset hint to work from.
func TestScheduler_PropagatesStatsReset(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	want := time.Now().Add(-time.Minute)
	mc := &mockCheck{name: "stat_statements", interval: 50 * time.Millisecond, timeout: 50 * time.Millisecond, statsReset: &want}
	target := &mockTarget{name: "pg", conn: &mockConn{}}
	s.AddEntry(target, mc, "postgres")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case res := <-s.Results():
		if res.Err != nil {
			t.Fatalf("unexpected scrape error: %v", res.Err)
		}
		if res.StatsReset == nil || !res.StatsReset.Equal(want) {
			t.Fatalf("expected StatsReset to propagate unchanged, got %v", res.StatsReset)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a run")
	}
	s.Stop()
}

// TestScheduler_PropagatesPartial keeps a deliberate top-N subset
// distinguishable from a complete result all the way to the wire.
func TestScheduler_PropagatesPartial(t *testing.T) {
	s := NewScheduler(ScheduleOptions{
		MaxWorkers: 1, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 5 * time.Second,
	})
	mc := &mockCheck{name: "table_stats", interval: 50 * time.Millisecond, timeout: 50 * time.Millisecond, partial: true}
	s.AddEntry(&mockTarget{name: "pg", conn: &mockConn{}}, mc, "postgres")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	select {
	case res := <-s.Results():
		if !res.Partial || res.Truncated {
			t.Fatalf("partial=%v truncated=%v, want partial only", res.Partial, res.Truncated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for a run")
	}
}

// TestScheduler_JitterWithinBounds tests that jitter and no-jitter modes work
func TestScheduler_JitterWithinBounds(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      2,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	check1 := &mockCheck{name: "check1", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	check2 := &mockCheck{name: "check2", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	target1 := &mockTarget{name: "target1", conn: &mockConn{}}
	target2 := &mockTarget{name: "target2", conn: &mockConn{}}

	s.AddEntry(target1, check1, "")
	s.AddEntry(target2, check2, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Collect at least one run
	runCount := 0
	timeout := time.After(2 * time.Second)
collect:
	for runCount < 1 {
		select {
		case <-results:
			runCount++
		case <-timeout:
			break collect
		}
	}

	s.Stop()

	if runCount < 1 {
		t.Errorf("expected runs without jitter, got %d", runCount)
	}
}

// TestScheduler_PerCheckTimeout tests that each check uses its own timeout
func TestScheduler_PerCheckTimeout(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	check := &mockCheck{name: "test_check", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	target := &mockTarget{name: "target1", conn: &mockConn{}}
	s.AddEntry(target, check, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Verify timeout is per-check
	if check.Timeout() != 50*time.Millisecond {
		t.Errorf("expected check timeout 50ms, got %v", check.Timeout())
	}

	// Collect one result
	timeout := time.After(2 * time.Second)
	select {
	case <-results:
	case <-timeout:
	}

	s.Stop()
}

// TestScheduler_SlowTargetDoesNotStarveOthers tests bounded concurrency
func TestScheduler_SlowTargetDoesNotStarveOthers(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      2,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	check1 := &mockCheck{name: "check1", interval: 100 * time.Millisecond, timeout: 1 * time.Second}
	check2 := &mockCheck{name: "check2", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	target1 := &mockTarget{name: "target1", conn: &mockConn{}}
	target2 := &mockTarget{name: "target2", conn: &mockConn{}}

	s.AddEntry(target1, check1, "")
	s.AddEntry(target2, check2, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Collect at least one run
	runCount := 0
	timeout := time.After(2 * time.Second)
collect:
	for runCount < 1 {
		select {
		case <-results:
			runCount++
		case <-timeout:
			break collect
		}
	}

	s.Stop()

	if runCount < 1 {
		t.Errorf("expected runs, got %d results", runCount)
	}
}

// TestScheduler_GracefulShutdown tests that Stop() closes the results channel
func TestScheduler_GracefulShutdown(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      1,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	check := &mockCheck{name: "test_check", interval: 100 * time.Millisecond, timeout: 50 * time.Millisecond}
	target := &mockTarget{name: "target1", conn: &mockConn{}}
	s.AddEntry(target, check, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Stop should complete and close channel
	s.Stop()

	// Stop must close the results channel; results buffered before the
	// close (e.g. the immediate first scrape) are drained first.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-results:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("expected results channel to be closed after Stop")
		}
	}
}

// TestScheduler_Concurrent tests scheduler with 50 entries under -race
func TestScheduler_Concurrent(t *testing.T) {
	opts := ScheduleOptions{
		MaxWorkers:      8,
		NoJitter:        true,
		Clock:           clock.System(),
		ShutdownTimeout: 5 * time.Second,
	}
	s := NewScheduler(opts)

	const numEntries = 50
	for i := 0; i < numEntries; i++ {
		check := &mockCheck{
			name:     "check_" + string(rune(i)),
			interval: 100 * time.Millisecond,
			timeout:  50 * time.Millisecond,
		}
		target := &mockTarget{name: "target", conn: &mockConn{}}
		s.AddEntry(target, check, "")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	results := s.Results()

	// Collect results
	runCount := 0
	timeout := time.After(2 * time.Second)
collect:
	for runCount < 1 {
		select {
		case <-results:
			runCount++
		case <-timeout:
			break collect
		}
	}

	s.Stop()

	if runCount < 1 {
		t.Errorf("expected at least 1 run, got %d", runCount)
	}
}

// TestScheduler_BreakerIsPerEntry proves one failing target cannot open the
// circuit for the same check on a healthy target. The breaker used to be
// keyed by check name alone, so three failures against an unreachable
// instance silenced that check fleet-wide for the whole backoff.
func TestScheduler_BreakerIsPerEntry(t *testing.T) {
	s := NewScheduler(ScheduleOptions{
		MaxWorkers: 2, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 5 * time.Second,
	})
	failing := &failingCheck{name: "shared_check"}
	healthy := &mockCheck{name: "shared_check", interval: 10 * time.Millisecond, timeout: time.Second}
	s.AddEntry(&mockTarget{name: "down", conn: &mockConn{}}, failing, "")
	s.AddEntry(&mockTarget{name: "up", conn: &mockConn{}}, healthy, "")

	keys := map[breakerKey]bool{}
	for k := range s.breaker {
		keys[k] = true
	}
	if len(keys) != 2 {
		t.Fatalf("breakers = %v, want one per entry", keys)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	upResults := 0
	deadline := time.After(3 * time.Second)
	for upResults < 10 {
		select {
		case r := <-s.Results():
			if r.Entry.TargetName == "up" {
				if r.Err != nil {
					t.Fatalf("healthy target reported %v", r.Err)
				}
				upResults++
			}
		case <-deadline:
			t.Fatalf("healthy target produced %d results; its breaker was opened by the failing target", upResults)
		}
	}
	if s.breaker[breakerKey{target: "up", check: "shared_check"}].Allow() != true {
		t.Fatal("healthy target's breaker is open")
	}
}

type failingCheck struct {
	name string
}

func (f *failingCheck) Name() string                   { return f.name }
func (f *failingCheck) Requires() check.Requirements   { return check.Requirements{} }
func (f *failingCheck) DefaultInterval() time.Duration { return 10 * time.Millisecond }
func (f *failingCheck) Timeout() time.Duration         { return time.Second }
func (f *failingCheck) Scrape(context.Context, check.Target) (check.Result, error) {
	return check.Result{}, errors.New("connection refused")
}

// overlapCheck records how many of its own scrapes run at once. Each scrape
// takes five times the entry interval.
type overlapCheck struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	starts      int
}

func (o *overlapCheck) Name() string                   { return "overlap" }
func (o *overlapCheck) Requires() check.Requirements   { return check.Requirements{} }
func (o *overlapCheck) DefaultInterval() time.Duration { return 10 * time.Millisecond }
func (o *overlapCheck) Timeout() time.Duration         { return time.Second }
func (o *overlapCheck) Scrape(ctx context.Context, _ check.Target) (check.Result, error) {
	o.mu.Lock()
	o.starts++
	o.inFlight++
	if o.inFlight > o.maxInFlight {
		o.maxInFlight = o.inFlight
	}
	o.mu.Unlock()

	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
	}

	o.mu.Lock()
	o.inFlight--
	o.mu.Unlock()
	return check.Result{}, nil
}

// TestScheduler_SlowScrapeIsNotOverlapped proves that a scrape longer than
// its interval is never joined by the next tick's scrape of the same entry.
// The ticks in the meantime are skipped, so the entry never runs more than
// one scrape at a time, even with a free worker for every tick.
func TestScheduler_SlowScrapeIsNotOverlapped(t *testing.T) {
	s := NewScheduler(ScheduleOptions{
		MaxWorkers: 4, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 5 * time.Second,
	})
	c := &overlapCheck{}
	s.AddEntryWithInterval(&mockTarget{name: "slow", conn: &mockConn{}}, c, "", 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	received := 0
	deadline := time.After(5 * time.Second)
	for received < 3 {
		select {
		case <-s.Results():
			received++
		case <-deadline:
			t.Fatalf("the entry produced %d results in 5s, want 3", received)
		}
	}
	s.Stop()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.starts < 3 {
		t.Fatalf("scrapes started = %d, want at least 3", c.starts)
	}
	if c.maxInFlight != 1 {
		t.Fatalf("up to %d scrapes of one entry ran at once, want 1", c.maxInFlight)
	}
}

// maxRand returns the largest value rand.Int63n(n) can return and records n.
func maxRand(seen *[]int64) func(int64) int64 {
	return func(n int64) int64 {
		if seen != nil {
			*seen = append(*seen, n)
		}
		return n - 1
	}
}

func TestFIRSTRUN001_DelayBelowShortIntervals(t *testing.T) {
	for _, interval := range []time.Duration{10 * time.Second, 30 * time.Second} {
		var seen []int64
		got := firstRunDelay(interval, maxRand(&seen))
		if got >= interval {
			t.Errorf("firstRunDelay(%v) = %v, want < %v", interval, got, interval)
		}
		if len(seen) != 1 || seen[0] != int64(interval) {
			t.Errorf("firstRunDelay(%v) drew from %v, want [%d]", interval, seen, int64(interval))
		}
	}
}

func TestFIRSTRUN002_HourlyCappedAtTenMinutes(t *testing.T) {
	var seen []int64
	got := firstRunDelay(time.Hour, maxRand(&seen))
	if len(seen) != 1 || seen[0] != int64(600*time.Second) {
		t.Fatalf("drew from %v, want [%d]", seen, int64(600*time.Second))
	}
	if got != 600*time.Second-1 {
		t.Errorf("firstRunDelay(1h) = %v, want just under 600s", got)
	}
}

func TestFIRSTRUN003_FifteenMinutesCappedAtTwoMinutes(t *testing.T) {
	var seen []int64
	got := firstRunDelay(15*time.Minute, maxRand(&seen))
	if got >= 120*time.Second || len(seen) != 1 || seen[0] != int64(120*time.Second) {
		t.Errorf("firstRunDelay(15m) = %v drawn from %v, want < 120s drawn from 120s", got, seen)
	}
}

func TestFIRSTRUN004_OneMinuteCappedAtThirtySeconds(t *testing.T) {
	var seen []int64
	got := firstRunDelay(time.Minute, maxRand(&seen))
	if got >= 30*time.Second || len(seen) != 1 || seen[0] != int64(30*time.Second) {
		t.Errorf("firstRunDelay(1m) = %v drawn from %v, want < 30s drawn from 30s", got, seen)
	}
}

func TestFIRSTRUN005_ZeroDrawAndNonPositiveInterval(t *testing.T) {
	zero := func(int64) int64 { return 0 }
	if got := firstRunDelay(time.Hour, zero); got != 0 {
		t.Errorf("firstRunDelay(1h, 0) = %v, want 0", got)
	}
	called := false
	never := func(int64) int64 { called = true; return 0 }
	for _, interval := range []time.Duration{0, -time.Second} {
		if got := firstRunDelay(interval, never); got != 0 {
			t.Errorf("firstRunDelay(%v) = %v, want 0", interval, got)
		}
	}
	if called {
		t.Error("rnd called for a non-positive interval")
	}
}

// stubCheck mirrors a registered check's name and interval; Scrape only counts.
type stubCheck struct {
	name     string
	interval time.Duration
	runs     atomic.Int64
}

func (c *stubCheck) Name() string                   { return c.name }
func (c *stubCheck) Requires() check.Requirements   { return check.Requirements{} }
func (c *stubCheck) DefaultInterval() time.Duration { return c.interval }
func (c *stubCheck) Timeout() time.Duration         { return time.Second }
func (c *stubCheck) Scrape(context.Context, check.Target) (check.Result, error) {
	c.runs.Add(1)
	return check.Result{}, nil
}

func startFake(t *testing.T, opts ScheduleOptions, entries ...check.Check) (*Scheduler, *clock.Fake) {
	t.Helper()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	opts.Clock = clk
	if opts.MaxWorkers == 0 {
		opts.MaxWorkers = 4
	}
	opts.ShutdownTimeout = 5 * time.Second
	s := NewScheduler(opts)
	target := &mockTarget{name: "target1", conn: &mockConn{}}
	for _, c := range entries {
		s.AddEntry(target, c, "")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		s.Stop()
	})
	return s, clk
}

func expectResult(t *testing.T, s *Scheduler, what string) EntryResult {
	t.Helper()
	select {
	case r := <-s.Results():
		return r
	case <-time.After(5 * time.Second):
		t.Fatalf("no result: %s", what)
		return EntryResult{}
	}
}

func expectNoResult(t *testing.T, s *Scheduler, what string) {
	t.Helper()
	select {
	case r := <-s.Results():
		t.Fatalf("unexpected result for %s: %s", r.Entry.CheckName, what)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestFIRSTRUN006_EveryDefaultCheckRunsWithinTheCap(t *testing.T) {
	registered := check.All()
	if len(registered) == 0 {
		t.Fatal("check.All() is empty")
	}
	stubs := make([]*stubCheck, 0, len(registered))
	entries := make([]check.Check, 0, len(registered))
	for _, c := range registered {
		stub := &stubCheck{name: c.Name(), interval: c.DefaultInterval()}
		stubs = append(stubs, stub)
		entries = append(entries, stub)
	}
	s, clk := startFake(t, ScheduleOptions{FirstRunRand: maxRand(nil)}, entries...)

	// An Advance sent before an entry sleeps would be lost.
	clk.BlockUntilSleepers(len(entries))
	clk.Advance(600 * time.Second)

	seen := map[string]bool{}
	for len(seen) < len(entries) {
		seen[expectResult(t, s, "one result per entry within 600 s").Entry.CheckName] = true
	}
	for _, stub := range stubs {
		if stub.runs.Load() < 1 {
			t.Errorf("%s (interval %v) did not run within 600 s", stub.name, stub.interval)
		}
	}
}

func TestFIRSTRUN007_HourlyCheckRunsAgainOneIntervalLater(t *testing.T) {
	stub := &stubCheck{name: "hourly", interval: time.Hour}
	s, clk := startFake(t, ScheduleOptions{FirstRunRand: maxRand(nil)}, stub)

	clk.BlockUntilSleepers(1)
	expectNoResult(t, s, "before the first-run delay")
	clk.Advance(600 * time.Second)
	expectResult(t, s, "first run after the first-run delay")
	expectNoResult(t, s, "right after the first run")

	clk.Advance(time.Hour - time.Second)
	expectNoResult(t, s, "before one interval elapsed")
	clk.Advance(time.Second)
	expectResult(t, s, "second run one interval after the first")
	expectNoResult(t, s, "after the second run")
	if got := stub.runs.Load(); got != 2 {
		t.Errorf("runs = %d, want 2", got)
	}
}

func TestFIRSTRUN008_NoJitterRunsAtStartThenOnInterval(t *testing.T) {
	stub := &stubCheck{name: "hourly", interval: time.Hour}
	s, clk := startFake(t, ScheduleOptions{NoJitter: true}, stub)

	expectResult(t, s, "immediate run before any Advance")
	expectNoResult(t, s, "before Advance(1h)")
	clk.Advance(time.Hour)
	expectResult(t, s, "run after Advance(1h)")
	expectNoResult(t, s, "after the second run")
	if got := stub.runs.Load(); got != 2 {
		t.Errorf("runs = %d, want 2", got)
	}
}
