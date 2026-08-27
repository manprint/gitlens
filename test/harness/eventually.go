package harness

import (
	"testing"
	"time"
)

// Eventually polls fn every 200ms until it returns nil or the timeout
// elapses, then fails with the LAST error rather than a bare timeout — the
// last error is the diagnosis.
func Eventually(t *testing.T, timeout time.Duration, fn func() error) {
	t.Helper()
	pollInterval := 200 * time.Millisecond
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastErr error
	for {
		err := fn()
		if err == nil {
			return
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		<-ticker.C
	}
	if lastErr != nil {
		t.Fatalf("Eventually timeout: %v", lastErr)
	} else {
		t.Fatalf("Eventually timeout")
	}
}

// Consistently asserts fn stays nil for the whole window. Used to prove a
// NON-event, e.g. "no second agent_down was emitted".
func Consistently(t *testing.T, window time.Duration, fn func() error) {
	t.Helper()
	pollInterval := 200 * time.Millisecond
	deadline := time.Now().Add(window)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		err := fn()
		if err != nil {
			t.Fatalf("Consistently breached: %v", err)
		}
		if time.Now().After(deadline) {
			return
		}
		<-ticker.C
	}
}
