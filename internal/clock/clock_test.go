package clock_test

import (
	"context"
	"testing"
	"time"

	"github.com/manprint/pglens/internal/clock"
	"github.com/stretchr/testify/require"
)

func TestWithOffset_NowShiftsByOffset(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := clock.NewFake(start)
	behind := clock.WithOffset(base, -5*time.Minute)
	ahead := clock.WithOffset(base, 5*time.Minute)

	require.Equal(t, start.Add(-5*time.Minute), behind.Now())
	require.Equal(t, start.Add(5*time.Minute), ahead.Now())
}

func TestWithOffset_SinceUsesShiftedNow(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := clock.NewFake(start)
	ahead := clock.WithOffset(base, 5*time.Minute)

	require.Equal(t, 5*time.Minute, ahead.Since(start))
}

func TestFake_Now(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	require.Equal(t, start, f.Now())
	f.Advance(time.Second)
	require.Equal(t, start.Add(time.Second), f.Now())
}

func TestFake_TickerFires(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	ticker := f.NewTicker(time.Second)
	f.Advance(3 * time.Second)
	// Should have 3 ticks, but channel capacity 1 means only 1 buffered if not drained.
	// Drain and count via repeated Advance with draining.
	// Instead test with draining:
	f2 := clock.NewFake(start)
	t2 := f2.NewTicker(time.Second)
	// Advance 999ms -> 0 ticks
	f2.Advance(999 * time.Millisecond)
	select {
	case <-t2.C():
		t.Fatal("should not have fired")
	default:
	}
	// Advance 1ms more -> 1 tick
	f2.Advance(time.Millisecond)
	select {
	case <-t2.C():
	default:
		t.Fatal("should have fired after 1s")
	}
	// Ensure 3 ticks over 3s when drained
	f3 := clock.NewFake(start)
	t3 := f3.NewTicker(time.Second)
	count := 0
	for i := 0; i < 3; i++ {
		f3.Advance(time.Second)
		select {
		case <-t3.C():
			count++
		default:
			t.Fatalf("tick %d missing", i)
		}
	}
	require.Equal(t, 3, count)
	// Also test initial f ticker: draining after 3s advance should give at least 1
	select {
	case <-ticker.C():
	default:
		t.Fatal("expected at least one tick after 3s")
	}
}

func TestFake_TickerStop(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	ticker := f.NewTicker(time.Second)
	ticker.Stop()
	f.Advance(5 * time.Second)
	select {
	case <-ticker.C():
		t.Fatal("should not fire after Stop")
	default:
	}
	// Stop should not panic on second call
	ticker.Stop()
}

func TestFake_SleepReleases(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	done := make(chan error, 1)
	go func() {
		done <- f.Sleep(context.Background(), 5*time.Second)
	}()
	f.BlockUntilSleepers(1)
	f.Advance(5 * time.Second)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not return after Advance")
	}
}

func TestFake_SleepContextCancel(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- f.Sleep(ctx, 10*time.Second)
	}()
	f.BlockUntilSleepers(1)
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
		require.Equal(t, context.Canceled, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep did not return after cancel")
	}
}

func TestFake_SlowConsumerDoesNotBlock(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	ticker := f.NewTicker(time.Millisecond)
	// Never drain ticker
	for i := 0; i < 100; i++ {
		f.Advance(time.Millisecond)
	}
	// If Advance blocked, we would not reach here
	_ = ticker
}

func TestFake_Concurrent(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := clock.NewFake(start)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			f.Now()
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		f.Advance(time.Millisecond)
	}
	<-done
}

func TestSystem_ClockAndTicker(t *testing.T) {
	t.Parallel()
	c := clock.System()
	before := time.Now()
	now := c.Now()
	require.False(t, now.Before(before))
	require.Less(t, c.Since(now), time.Second)

	ticker := c.NewTicker(time.Millisecond)
	defer ticker.Stop()
	select {
	case <-ticker.C():
	case <-time.After(time.Second):
		t.Fatal("system ticker did not fire")
	}
}

func TestSystem_Sleep(t *testing.T) {
	t.Parallel()
	c := clock.System()
	require.NoError(t, c.Sleep(context.Background(), time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, c.Sleep(ctx, time.Second), context.Canceled)
}

func TestWithOffset_DelegatesTickerAndSleep(t *testing.T) {
	t.Parallel()
	f := clock.NewFake(time.Unix(0, 0))
	c := clock.WithOffset(f, time.Hour)
	ticker := c.NewTicker(time.Second)
	f.Advance(time.Second)
	select {
	case <-ticker.C():
	default:
		t.Fatal("delegated ticker did not fire")
	}
	ticker.Stop()

	done := make(chan error, 1)
	go func() { done <- c.Sleep(context.Background(), time.Second) }()
	f.BlockUntilSleepers(1)
	f.Advance(time.Second)
	require.NoError(t, <-done)
}
