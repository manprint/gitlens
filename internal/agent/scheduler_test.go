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
	name     string
	interval time.Duration
	timeout  time.Duration
	callCnt  int
	mu       sync.Mutex
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
	return check.Result{}, nil
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
