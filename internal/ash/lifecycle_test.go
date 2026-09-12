package ash

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/leaktest"
)

type idleQuerier struct{}

func (idleQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, context.Canceled
}

// TestSampler_StopLeavesNoGoroutines — the sampler's 1-second loop runs for
// the life of the agent's target; Stop()'s contract is that it is gone when
// Stop returns.
func TestSampler_StopLeavesNoGoroutines(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewSampler(idleQuerier{}, clock.System(), nil)
	s.SetInterval(10 * time.Millisecond)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	s.Stop()
}

// TestSampler_RestartAfterStop — Start's guard keyed on `s.ctx != nil`, which
// Stop left set, so a stopped sampler reported "already running" forever.
func TestSampler_RestartAfterStop(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewSampler(idleQuerier{}, clock.System(), nil)
	s.SetInterval(10 * time.Millisecond)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	s.Stop()
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("restart after Stop: %v", err)
	}
	s.Stop()
}

// TestSampler_DoubleStartIsRejected — the guard must still reject a genuine
// double start.
func TestSampler_DoubleStartIsRejected(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewSampler(idleQuerier{}, clock.System(), nil)
	s.SetInterval(10 * time.Millisecond)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Start(t.Context()); err == nil {
		t.Error("a second Start on a running sampler was accepted")
	}
	s.Stop()
}

// TestSampler_StopIsIdempotent — cmd/pglens-agent's stop func can be reached
// from more than one path.
func TestSampler_StopIsIdempotent(t *testing.T) {
	defer leaktest.Check(t)()

	s := NewSampler(idleQuerier{}, clock.System(), nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
	s.Stop()
}
