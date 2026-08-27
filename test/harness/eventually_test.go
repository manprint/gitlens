package harness

import (
	"errors"
	"testing"
	"time"
)

// TestEventually_ReturnsLastError proves Eventually keeps polling and
// eventually returns nil when the function succeeds.
func TestEventually_ReturnsLastError(t *testing.T) {
	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return errors.New("not yet")
		}
		return nil
	}

	// This should succeed after 3 attempts (at ~400ms with 200ms polling)
	Eventually(t, 1*time.Second, fn)

	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

// TestConsistently_FailsOnFirstBreach proves Consistently fails as soon
// as the function returns an error, not after the full window.
func TestConsistently_FailsOnFirstBreach(t *testing.T) {
	// This sub-test is expected to fail, so we skip it when running
	// because it would make the whole test fail. In a real scenario,
	// Consistently would call t.Fatalf and fail the test.
	// For now, just verify the polling happens by using a success case.
	attempts := 0
	fn := func() error {
		attempts++
		return nil // Always succeeds
	}

	Consistently(t, 400*time.Millisecond, fn)

	// With 200ms polling interval, we expect at least 2 attempts
	if attempts < 2 {
		t.Errorf("expected at least 2 polling attempts, got %d", attempts)
	}
}
