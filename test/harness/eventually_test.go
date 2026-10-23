package harness

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// fakeT stands in for *testing.T so a test can observe Fatalf. Like the real
// one, Fatalf stops the goroutine it runs on, so runFatal calls the helper
// from its own goroutine. Only Helper and Fatalf are used by the helpers under
// test; any other method panics on the nil embedded TB.
type fakeT struct {
	testing.TB
	failed bool
	fatal  string
}

func (f *fakeT) Helper() {}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.failed = true
	f.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// runFatal runs body on its own goroutine and reports what it did to the fake.
func runFatal(body func(t testing.TB)) *fakeT {
	f := &fakeT{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		body(f)
	}()
	<-done
	return f
}

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

// TestEventually_FailsWithLastErrorOnTimeout proves a timeout is a failure
// that names the most recent error, not a bare "timeout".
func TestEventually_FailsWithLastErrorOnTimeout(t *testing.T) {
	attempts := 0
	f := runFatal(func(tb testing.TB) {
		Eventually(tb, 300*time.Millisecond, func() error {
			attempts++
			return fmt.Errorf("attempt %d", attempts)
		})
	})

	if !f.failed {
		t.Fatalf("Eventually returned without failing after %d attempts", attempts)
	}
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts before the timeout, got %d", attempts)
	}
	if want := fmt.Sprintf("Eventually timeout: attempt %d", attempts); f.fatal != want {
		t.Errorf("timeout message: got %q, want %q", f.fatal, want)
	}
}

// TestConsistently_HoldsForTheWholeWindow proves Consistently returns without
// failing when the function never breaches.
func TestConsistently_HoldsForTheWholeWindow(t *testing.T) {
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

// TestConsistently_FailsOnFirstBreach proves Consistently fails as soon as
// the function returns an error, not after the full window.
func TestConsistently_FailsOnFirstBreach(t *testing.T) {
	attempts := 0
	start := time.Now()
	f := runFatal(func(tb testing.TB) {
		Consistently(tb, 5*time.Second, func() error {
			attempts++
			if attempts == 3 {
				return errors.New("breach")
			}
			return nil
		})
	})

	if !f.failed {
		t.Fatalf("Consistently returned without failing on a breach after %d calls", attempts)
	}
	if attempts != 3 {
		t.Errorf("expected Consistently to stop at the breach (call 3), got %d calls", attempts)
	}
	if want := "Consistently breached: breach"; f.fatal != want {
		t.Errorf("breach message: got %q, want %q", f.fatal, want)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Consistently ran %v after the breach, want an immediate failure", elapsed)
	}
}
