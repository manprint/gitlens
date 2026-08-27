//go:build e2e

package scenario

import (
	"context"
	"fmt"
	"time"
)

// poll calls check repeatedly until it reports true, returning its last
// error (or a plain timeout) once ctx is done. Scenario bodies wait for
// asynchronous agent/server behavior (a reconnect, an emitted event, a
// resumed series) this way instead of a fixed sleep, since the compose
// stack's actual timing varies with host load.
func poll(ctx context.Context, interval time.Duration, check func(context.Context) (bool, error)) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		ok, err := check(ctx)
		if err == nil && ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("poll timed out: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return fmt.Errorf("poll timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
