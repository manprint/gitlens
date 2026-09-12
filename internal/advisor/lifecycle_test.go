package advisor

import (
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/leaktest"
)

// TestEngine_StopLeavesNoGoroutines — Start launches the advisor pass loop
// for the life of the server process.
func TestEngine_StopLeavesNoGoroutines(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithStore(nil, clock.NewFake(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)), time.Second, nil)
	e.Start(t.Context())
	e.Stop()
}

// TestEngine_StopWithoutStartIsANoOp — main() defers Stop unconditionally,
// including on the startup paths that return before Start is reached.
func TestEngine_StopWithoutStartIsANoOp(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithStore(nil, clock.NewFake(time.Now()), time.Second, nil)
	e.Stop()
	e.Stop()
}

// TestEngine_StopIsIdempotent — the deferred Stop can be reached twice on the
// listener-failure path.
func TestEngine_StopIsIdempotent(t *testing.T) {
	defer leaktest.Check(t)()

	e := NewEngineWithStore(nil, clock.NewFake(time.Now()), time.Second, nil)
	e.Start(t.Context())
	e.Stop()
	e.Stop()
}
