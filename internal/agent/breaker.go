package agent

import (
	"sync"
	"sync/atomic"
	"time"
)

// Breaker is a circuit breaker: 3 consecutive failures suspend an entry with exponential backoff.
// One success closes it.
type Breaker struct {
	mu              sync.Mutex
	failures        int
	state           int32 // 0 = closed, 1 = open
	openedAt        time.Time
	backoffAttempts int // how many times we've been open (for backoff calc)
}

// NewBreaker creates a closed circuit breaker.
func NewBreaker() *Breaker {
	return &Breaker{}
}

// Allow returns true if the circuit is closed and the entry may run.
// When open with a backoff, Allow blocks until the backoff expires or returns false.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == 0 {
		return true // closed, allow
	}

	// Open: check if backoff has expired
	backoff := b.calculateBackoff(b.backoffAttempts)
	if time.Since(b.openedAt) >= backoff {
		// Backoff expired; try again
		return true
	}

	return false // still backing off
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

// RecordFailure increments the failure counter and opens the circuit after 3 failures.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	if b.failures >= 3 && b.state == 0 {
		b.state = 1
		b.openedAt = time.Now()
		b.backoffAttempts++
	}
}

// calculateBackoff returns the exponential backoff duration for the given attempt count.
// Sequence: 1m, 2m, 4m, 8m, capped at 15m.
func (b *Breaker) calculateBackoff(attempt int) time.Duration {
	max := time.Duration(15 * time.Minute)
	backoff := time.Duration(1<<uint(attempt)) * time.Minute
	if backoff > max {
		backoff = max
	}
	return backoff
}

// State returns true if the circuit is open.
func (b *Breaker) State() bool {
	atomic.LoadInt32(&b.state)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == 1
}
