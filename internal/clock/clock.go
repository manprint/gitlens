package clock

import (
	"context"
	"time"
)

// Clock abstracts time for deterministic testing.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	NewTicker(d time.Duration) Ticker
	// Sleep returns ctx.Err() if the context is cancelled first, nil otherwise.
	Sleep(ctx context.Context, d time.Duration) error
}

// Ticker is a ticker returned by Clock.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type systemClock struct{}

// System returns the real clock.
func System() Clock { return systemClock{} }

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Since(t time.Time) time.Duration { return time.Since(t) }

func (systemClock) NewTicker(d time.Duration) Ticker {
	t := time.NewTicker(d)
	return &systemTicker{t: t}
}

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type systemTicker struct {
	t *time.Ticker
}

func (s *systemTicker) C() <-chan time.Time { return s.t.C }

func (s *systemTicker) Stop() { s.t.Stop() }
