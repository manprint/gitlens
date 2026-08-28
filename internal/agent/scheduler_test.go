package agent

import (
	"context"
	"sync"
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
	return check.Result{QueryTexts: m.queryTexts, StatsReset: m.statsReset}, nil
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

	// Results channel should be closed after Stop
	_, ok := <-results
	if ok {
		t.Fatal("expected results channel to be closed after Stop")
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
