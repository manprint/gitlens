package agent

import (
	"testing"
	"time"
)

// TestBreaker_OpensAfterThree tests that the breaker opens after 3 consecutive failures.
func TestBreaker_OpensAfterThree(t *testing.T) {
	b := NewBreaker()

	if !b.Allow() {
		t.Fatal("expected Allow to be true initially")
	}

	b.RecordFailure()
	if b.State() {
		t.Fatal("expected circuit to be closed after 1 failure")
	}

	b.RecordFailure()
	if b.State() {
		t.Fatal("expected circuit to be closed after 2 failures")
	}

	b.RecordFailure()
	if !b.State() {
		t.Fatal("expected circuit to be open after 3 failures")
	}

	if b.Allow() {
		t.Fatal("expected Allow to be false when circuit is open")
	}
}

// TestBreaker_Backoff tests the exponential backoff sequence: 1m, 2m, 4m, 8m, 15m, 15m.
func TestBreaker_Backoff(t *testing.T) {
	b := NewBreaker()

	tests := []struct {
		name            string
		attempt         int
		expectedBackoff time.Duration
	}{
		{"attempt 0", 0, 1 * time.Minute},
		{"attempt 1", 1, 2 * time.Minute},
		{"attempt 2", 2, 4 * time.Minute},
		{"attempt 3", 3, 8 * time.Minute},
		{"attempt 4", 4, 15 * time.Minute},
		{"attempt 5", 5, 15 * time.Minute},
		{"attempt 10", 10, 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backoff := b.calculateBackoff(tt.attempt)
			if backoff != tt.expectedBackoff {
				t.Errorf("expected backoff %v, got %v", tt.expectedBackoff, backoff)
			}
		})
	}
}

// TestBreaker_ClosesOnSuccess tests that one success closes the circuit and resets the counter.
func TestBreaker_ClosesOnSuccess(t *testing.T) {
	b := NewBreaker()

	// Open the circuit
	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	if !b.State() {
		t.Fatal("expected circuit to be open after 3 failures")
	}

	// Record a success
	b.RecordSuccess()
	if b.State() {
		t.Fatal("expected circuit to be closed after success")
	}

	// Verify failures counter is reset
	if !b.Allow() {
		t.Fatal("expected Allow to be true after closing")
	}

	// One more failure should not immediately open
	b.RecordFailure()
	if b.State() {
		t.Fatal("expected circuit to be closed after 1 failure (counter reset)")
	}
}

// TestBreaker_FirstBackoffIsOneMinute pins the first open window to the
// documented 1m. It used to open with backoffAttempts already at 1, so the
// very first suspension lasted 2m and the 1m step of the ladder never ran.
func TestBreaker_FirstBackoffIsOneMinute(t *testing.T) {
	now := time.Now()
	b := NewBreaker()
	b.now = func() time.Time { return now }

	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()

	now = now.Add(59 * time.Second)
	if b.Allow() {
		t.Fatal("expected Allow to be false 59s into the first 1m backoff")
	}
	now = now.Add(time.Second)
	if !b.Allow() {
		t.Fatal("expected Allow to be true once the first 1m backoff elapsed")
	}
}

// TestBreaker_HalfOpenFailureReArms tests that a failing trial scrape re-arms
// the breaker with the next, longer backoff. Previously openedAt was frozen at
// the moment the circuit opened, so after the first expiry Allow() returned
// true on every call forever: an unreachable instance was scraped at full
// rate, and the 2m/4m/8m/15m steps were dead code.
func TestBreaker_HalfOpenFailureReArms(t *testing.T) {
	now := time.Now()
	b := NewBreaker()
	b.now = func() time.Time { return now }

	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()

	for _, want := range []time.Duration{1 * time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute} {
		now = now.Add(want - time.Second)
		if b.Allow() {
			t.Fatalf("expected Allow to be false just before the %v backoff elapsed", want)
		}
		now = now.Add(time.Second)
		if !b.Allow() {
			t.Fatalf("expected the half-open trial to be allowed after %v", want)
		}
		// The trial fails: the breaker must suspend again, not stay permissive.
		b.RecordFailure()
		if !b.State() {
			t.Fatal("expected circuit to stay open after a failed trial")
		}
		if b.Allow() {
			t.Fatalf("expected Allow to be false immediately after a failed trial at %v", want)
		}
	}
}

// TestBreaker_HalfOpenSuccessResetsLadder tests that a successful trial closes
// the circuit and puts the backoff ladder back at its first step.
func TestBreaker_HalfOpenSuccessResetsLadder(t *testing.T) {
	now := time.Now()
	b := NewBreaker()
	b.now = func() time.Time { return now }

	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	now = now.Add(time.Minute)
	b.RecordFailure() // escalate to the 2m step
	now = now.Add(2 * time.Minute)
	if !b.Allow() {
		t.Fatal("expected a trial to be allowed after the 2m backoff")
	}

	b.RecordSuccess()
	if b.State() {
		t.Fatal("expected circuit to be closed after a successful trial")
	}

	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	now = now.Add(59 * time.Second)
	if b.Allow() {
		t.Fatal("expected the ladder to restart at 1m after a success")
	}
	now = now.Add(time.Second)
	if !b.Allow() {
		t.Fatal("expected Allow to be true once the restarted 1m backoff elapsed")
	}
}

// TestBreaker_BackoffStaysInRange sweeps far past the point where the old
// shift overflowed time.Duration. Every step must be positive, must not
// shrink, and must stop at the 15m cap.
func TestBreaker_BackoffStaysInRange(t *testing.T) {
	b := NewBreaker()
	const maxBackoff = 15 * time.Minute

	prev := time.Duration(0)
	for attempt := 0; attempt <= 100; attempt++ {
		backoff := b.calculateBackoff(attempt)
		if backoff <= 0 || backoff > maxBackoff {
			t.Fatalf("attempt %d: backoff %v, want within (0, %v]", attempt, backoff, maxBackoff)
		}
		if backoff < prev {
			t.Fatalf("attempt %d: backoff %v shrank from %v", attempt, backoff, prev)
		}
		prev = backoff
	}
	if prev != maxBackoff {
		t.Fatalf("backoff after 100 attempts = %v, want %v", prev, maxBackoff)
	}
}

// TestBreaker_OpenAfterLongOutageStaysSuspended drives a breaker through 40
// failed half-open trials, the state a target that stays down for hours
// reaches. Every trial must be suspended again right after it fails, and the
// breaker must not allow a scrape before the 15m cap has elapsed.
func TestBreaker_OpenAfterLongOutageStaysSuspended(t *testing.T) {
	now := time.Now()
	b := NewBreaker()
	b.now = func() time.Time { return now }

	b.RecordFailure()
	b.RecordFailure()
	b.RecordFailure()
	for i := 0; i < 40; i++ {
		now = now.Add(15 * time.Minute)
		if !b.Allow() {
			t.Fatalf("trial %d: expected the half-open trial to be allowed after the cap", i)
		}
		b.RecordFailure()
		if b.Allow() {
			t.Fatalf("trial %d: expected Allow to be false right after a failed trial", i)
		}
	}

	now = now.Add(15*time.Minute - time.Second)
	if b.Allow() {
		t.Fatal("expected Allow to be false just before the 15m cap elapsed")
	}
	now = now.Add(time.Second)
	if !b.Allow() {
		t.Fatal("expected Allow to be true once the 15m cap elapsed")
	}
}
