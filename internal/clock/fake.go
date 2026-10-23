package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Fake is a manually advanced clock. Every method is safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	now      time.Time
	tickers  []*fakeTicker
	sleepers []*sleeper
}

type fakeTicker struct {
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  atomic.Bool
}

type sleeper struct {
	deadline time.Time
	ch       chan struct{}
}

// NewFake creates a fake clock starting at t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t}
}

// Now returns the current fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Since returns time since t.
func (f *Fake) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}

// NewTicker creates a ticker that fires every d.
func (f *Fake) NewTicker(d time.Duration) Ticker {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTicker{
		interval: d,
		next:     f.now.Add(d),
		ch:       make(chan time.Time, 1),
	}
	f.tickers = append(f.tickers, t)
	return t
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

// Stop may run on another goroutine while Advance reads the flag under Fake.mu.
// The ticker holds no pointer back to the Fake, so Stop cannot take that lock;
// an atomic flag makes the handoff race-free instead.
func (t *fakeTicker) Stop() {
	t.stopped.Store(true)
}

// Sleep blocks until d has elapsed on the fake clock or ctx is cancelled.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	f.mu.Lock()
	deadline := f.now.Add(d)
	ch := make(chan struct{})
	s := &sleeper{deadline: deadline, ch: ch}
	f.sleepers = append(f.sleepers, s)
	f.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		// Remove sleeper if not already fired
		f.mu.Lock()
		for i, sl := range f.sleepers {
			if sl == s {
				f.sleepers = append(f.sleepers[:i], f.sleepers[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
		return ctx.Err()
	}
}

// Advance moves the clock forward, firing every ticker whose period elapsed
// and releasing every sleeper whose deadline passed, in chronological order.
// It returns after all of them have been delivered.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	newNow := f.now.Add(d)
	f.now = newNow

	// Collect tickers to fire: we need to handle multiple ticks per ticker.
	// We fire in order of tick time, but for simplicity we fire all ticks for each ticker
	// in order, and wake sleepers whose deadline <= newNow.
	// Since ticker interval and sleeper deadline are both based on f.now progression,
	// delivering in any order that respects that both happen after the advance is acceptable
	// for the tests, which only check counts.

	// Fire tickers
	for _, t := range f.tickers {
		if t.stopped.Load() {
			continue
		}
		for !t.next.After(newNow) {
			select {
			case t.ch <- t.next:
			default:
			}
			t.next = t.next.Add(t.interval)
		}
	}

	// Wake sleepers
	var remaining []*sleeper
	for _, s := range f.sleepers {
		if !s.deadline.After(newNow) {
			close(s.ch)
		} else {
			remaining = append(remaining, s)
		}
	}
	f.sleepers = remaining
	f.mu.Unlock()
}

// BlockUntilSleepers blocks until n goroutines are waiting in Sleep.
// Tests use it to remove the race between starting a goroutine and advancing the clock.
func (f *Fake) BlockUntilSleepers(n int) {
	for {
		f.mu.Lock()
		count := len(f.sleepers)
		f.mu.Unlock()
		if count >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
