package alert

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/leaktest"
)

// TestEngine_StopLeavesNoGoroutines — Start launches the rule-evaluation loop
// for the life of the server process; Stop()'s contract is that it is gone
// when Stop returns.
func TestEngine_StopLeavesNoGoroutines(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithInterval(nil, clock.NewFake(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)), time.Second, nil, nil, nil)
	e.Start(t.Context())
	e.Stop()
}

// TestEngine_StartAfterStopDoesNotPanic — Stop closes doneCh itself when the
// engine was never started. A later Start would then launch a loop whose
// `defer close(e.doneCh)` closes an already-closed channel: a panic that
// takes down the whole server, not just the engine.
func TestEngine_StartAfterStopDoesNotPanic(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithInterval(nil, clock.NewFake(time.Now()), time.Second, nil, nil, nil)
	e.Stop()
	e.Start(t.Context())
	e.Stop()
}

// TestEngine_StopIsIdempotent — main() stops the engine from a defer.
func TestEngine_StopIsIdempotent(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithInterval(nil, clock.NewFake(time.Now()), time.Second, nil, nil, nil)
	e.Start(t.Context())
	e.Stop()
	e.Stop()
}

// TestEvaluator_ConcurrentStepIsSafe — Tick is exported and driven directly
// by the integration suites as well as by the engine's own ticker. The
// evaluator's state map had no lock, so two concurrent callers crashed the
// process with "concurrent map writes".
func TestEvaluator_ConcurrentStepIsSafe(t *testing.T) {
	ev := NewEvaluator()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	rule := Rule{ID: "r", Comparator: GT, Threshold: 0, For: 0, Severity: SeverityWarning}

	done := make(chan struct{})
	for w := 0; w < 4; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				ev.Step(rule, Sample{Value: float64(i), Labels: map[string]string{"w": string(rune('a' + w))}}, now.Add(time.Duration(i)*time.Second))
				ev.Forget(now)
			}
		}(w)
	}
	for w := 0; w < 4; w++ {
		<-done
	}
}
