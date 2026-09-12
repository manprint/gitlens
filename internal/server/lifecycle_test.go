package server

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/leaktest"
)

// TestStaleness_StopLeavesNoGoroutines — Start launches an evaluator loop
// that lives for the life of the server; Stop()'s contract is that the loop
// is gone when it returns. Nothing asserted it.
func TestStaleness_StopLeavesNoGoroutines(t *testing.T) {
	defer leaktest.Check(t)()

	clk := clock.NewFake(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	s := NewStaleness(nil, clk)
	s.Start(t.Context())
	s.Stop()
}

// TestStaleness_StopWithoutStartIsPrompt — Stop() waited a flat two seconds
// on a doneCh that nothing would ever close when Start had not run. A server
// started without PGLENS_DSN paid that on every shutdown.
func TestStaleness_StopWithoutStartIsPrompt(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewStaleness(nil, clock.NewFake(time.Now()))
	start := time.Now()
	s.Stop()
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Stop without Start took %v; it should not wait on a goroutine that was never launched", elapsed)
	}
}

// TestStaleness_StartAfterStopDoesNotPanic — startOnce would otherwise launch
// a loop whose `defer close(doneCh)` closes a channel Stop already closed.
func TestStaleness_StartAfterStopDoesNotPanic(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewStaleness(nil, clock.NewFake(time.Now()))
	s.Stop()
	s.Start(t.Context())
	s.Stop()
}

// TestStaleness_StopIsIdempotent — main() stops it from a defer, and a failed
// listener path can reach the same defer twice.
func TestStaleness_StopIsIdempotent(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewStaleness(nil, clock.NewFake(time.Now()))
	s.Start(t.Context())
	s.Stop()
	s.Stop()
}
