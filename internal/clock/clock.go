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

// offsetClock shifts a base Clock's Now()/Since() by a fixed duration —
// simulates a misconfigured system clock (e.g. NTP drift) without touching
// the process's real time. Ticker/Sleep measure elapsed time, not absolute
// time, so they pass through to base unchanged.
type offsetClock struct {
	base   Clock
	offset time.Duration
}

// WithOffset returns a Clock whose Now() reports base.Now() shifted by
// offset — positive makes it appear ahead of base, negative makes it appear
// behind.
func WithOffset(base Clock, offset time.Duration) Clock {
	return offsetClock{base: base, offset: offset}
}

func (o offsetClock) Now() time.Time { return o.base.Now().Add(o.offset) }

func (o offsetClock) Since(t time.Time) time.Duration { return o.Now().Sub(t) }

func (o offsetClock) NewTicker(d time.Duration) Ticker { return o.base.NewTicker(d) }

func (o offsetClock) Sleep(ctx context.Context, d time.Duration) error { return o.base.Sleep(ctx, d) }
