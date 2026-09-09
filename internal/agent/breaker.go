package agent

import (
	"sync"
	"time"
)

// Breaker is a circuit breaker: 3 consecutive failures suspend an entry with exponential backoff.
// One success closes it.
type Breaker struct {
	mu sync.Mutex
	// now is injectable so the backoff ladder is testable without sleeping.
	now             func() time.Time
	failures        int
	state           int32 // 0 = closed, 1 = open
	openedAt        time.Time
	backoffAttempts int // how many times the backoff has escalated while open
}

// NewBreaker creates a closed circuit breaker.
func NewBreaker() *Breaker {
	return &Breaker{now: time.Now}
}

func (b *Breaker) clock() time.Time {
	if b.now == nil {
		return time.Now()
	}
	return b.now()
}

// Allow returns true if the circuit is closed and the entry may run.
// While open it returns false until the current backoff has elapsed, then
// allows a single trial scrape (half-open). RecordSuccess closes the circuit;
// RecordFailure re-arms it with the next, longer backoff.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == 0 {
		return true // closed, allow
	}

	// Open: check if backoff has expired
	backoff := b.calculateBackoff(b.backoffAttempts)
	return b.clock().Sub(b.openedAt) >= backoff
}

// RecordSuccess resets the failure counter and closes the circuit if open.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures = 0
	if b.state == 1 {
		b.state = 0
		b.backoffAttempts = 0
	}
}

// RecordFailure increments the failure counter and opens the circuit after 3
// failures. A failure while already open is the half-open trial failing: it
// restarts the backoff clock and escalates to the next step of the ladder.
//
// Without that second half, the documented ladder was unreachable: once open,
// openedAt was never moved again, so `Allow()` returned true forever from the
// first expiry onwards (every subsequent scrape ran unthrottled against a
// target that was still failing), and backoffAttempts never advanced past 1 —
// making the whole 1m/2m/4m/8m/15m sequence dead code. Opening at
// backoffAttempts=0 also fixes the ladder's own first step, which used to
// start at 2m instead of the documented 1m.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	if b.state == 1 {
		b.openedAt = b.clock()
		b.backoffAttempts++
		return
	}
	if b.failures >= 3 {
		b.state = 1
		b.openedAt = b.clock()
		b.backoffAttempts = 0
	}
}

// calculateBackoff returns the exponential backoff duration for the given attempt count.
// Sequence: 1m, 2m, 4m, 8m, capped at 15m.
func (b *Breaker) calculateBackoff(attempt int) time.Duration {
	max := time.Duration(15 * time.Minute)
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 62 {
		return max
	}
	backoff := time.Duration(1<<uint(attempt)) * time.Minute
	if backoff > max {
		backoff = max
	}
	return backoff
}

// State returns true if the circuit is open.
func (b *Breaker) State() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == 1
}
