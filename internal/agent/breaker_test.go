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
