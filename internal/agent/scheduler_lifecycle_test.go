package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/leaktest"
	"github.com/manprint/pglens/internal/pgtype"
)

// blockingCheck scrapes forever until released, so a test can hold every
// worker slot and then assert on what Stop() does about it.
type blockingCheck struct {
	name    string
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (c *blockingCheck) Name() string                   { return c.name }
func (c *blockingCheck) Requires() check.Requirements   { return check.Requirements{} }
func (c *blockingCheck) DefaultInterval() time.Duration { return 10 * time.Millisecond }
func (c *blockingCheck) Timeout() time.Duration         { return 2 * time.Second }
func (c *blockingCheck) Scrape(ctx context.Context, t check.Target) (check.Result, error) {
	c.once.Do(func() { close(c.entered) })
	select {
	case <-c.release:
	case <-ctx.Done():
	}
	return check.Result{}, nil
}

func lifecycleTarget() check.Target {
	return &check.SimpleTarget{DatabaseValue: "app", ClockValue: clock.System(), PGVersionValue: pgtype.PGVersion(150000)}
}

// TestScheduler_StopLeavesNoGoroutines — Stop()'s whole contract is that the
// per-entry ticker loops and every scrape goroutine they spawned are gone
// when it returns. Nothing asserted that: a loop that missed its ctx.Done()
// case kept ticking for the rest of the process, and the test that started it
// still passed.
func TestScheduler_StopLeavesNoGoroutines(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewScheduler(ScheduleOptions{MaxWorkers: 2, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 5 * time.Second})
	c := &blockingCheck{name: "blocking", release: make(chan struct{}), entered: make(chan struct{})}
	s.AddEntryWithInterval(lifecycleTarget(), c, "", 10*time.Millisecond)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Drain results so nothing parks on an unread channel.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range s.Results() {
		}
	}()

	select {
	case <-c.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the check never ran")
	}

	s.Stop()
	close(c.release)
	<-drained
}

// TestScheduler_StopBeforeStartIsANoOp — Stop() used to dereference a nil
// cancel func. cmd/pglens-agent calls Stop() from its push loop the moment
// the server revokes the agent, which can happen before Start has run on a
// slow target, and calls it again at shutdown.
func TestScheduler_StopBeforeStartIsANoOp(t *testing.T) {
	defer leaktest.Check(t)()
	s := NewScheduler(DefaultScheduleOptions())
	s.Stop() // must not panic
	s.Stop()
}

// TestScheduler_StopIsIdempotent — the revocation path and the shutdown path
// both call Stop() on the same scheduler.
func TestScheduler_StopIsIdempotent(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewScheduler(ScheduleOptions{MaxWorkers: 1, NoJitter: true, Clock: clock.System(), ShutdownTimeout: 2 * time.Second})
	s.AddEntryWithInterval(lifecycleTarget(), &blockingCheck{name: "idem", release: make(chan struct{}), entered: make(chan struct{})}, "", time.Second)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range s.Results() {
		}
	}()
	s.Stop()
	s.Stop()
	<-drained
}

// TestScheduler_StopIsPromptWithEveryWorkerBusy — the ticker loop acquired
// its worker slot with a bare channel send. With every worker parked on an
// unresponsive target, that send blocked, so the loop could not even reach
// its ctx.Done() case and Stop() waited out the whole ShutdownTimeout.
func TestScheduler_StopIsPromptWithEveryWorkerBusy(t *testing.T) {
	defer leaktest.Check(t)()

	const shutdownTimeout = 10 * time.Second
	s := NewScheduler(ScheduleOptions{MaxWorkers: 1, NoJitter: true, Clock: clock.System(), ShutdownTimeout: shutdownTimeout})
	c := &blockingCheck{name: "busy", release: make(chan struct{}), entered: make(chan struct{})}
	// Two entries, one worker: the second entry's loop is guaranteed to be
	// waiting on the semaphore while the first holds it.
	s.AddEntryWithInterval(lifecycleTarget(), c, "a", 5*time.Millisecond)
	s.AddEntryWithInterval(lifecycleTarget(), c, "b", 5*time.Millisecond)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range s.Results() {
		}
	}()
	select {
	case <-c.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the check never ran")
	}

	start := time.Now()
	go func() {
		// Release the in-flight scrape shortly after Stop is asked for, the
		// way a real scrape finishes on its own context deadline.
		time.Sleep(200 * time.Millisecond)
		close(c.release)
	}()
	s.Stop()
	if elapsed := time.Since(start); elapsed >= shutdownTimeout {
		t.Errorf("Stop took %v, i.e. it waited out the full ShutdownTimeout instead of unwinding", elapsed)
	}
	<-drained
}
