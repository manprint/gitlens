package main

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lockStorm creates lock contention by having N sessions doing SELECT...FOR UPDATE
// on one row. The report gives the observed maximum wait time.
func lockStorm(ctx context.Context, pool *pgxpool.Pool, sessions int, duration, hold time.Duration, report *Report) error {
	done := time.Now().Add(duration)
	var maxWait atomic.Int64
	var attempts atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(done) {
				attempts.Add(1)
				start := time.Now()
				err := func() error {
					tx, err := pool.Begin(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = tx.Rollback(ctx) }()

					// This will wait for the lock
					_, err = tx.Exec(ctx, "SELECT 1 FROM lockstorm_test WHERE id = 1 FOR UPDATE")
					if err != nil {
						return err
					}

					// The default is deliberately short for the load scenario. UI
					// acceptance can opt into a longer hold so the 10s lock sampler
					// observes a stable blocking pair.
					time.Sleep(hold)

					return tx.Commit(ctx)
				}()

				if err == nil {
					wait := time.Since(start)
					// Update max wait time
					currentMax := maxWait.Load()
					for wait.Nanoseconds() > currentMax {
						if maxWait.CompareAndSwap(currentMax, wait.Nanoseconds()) {
							break
						}
						currentMax = maxWait.Load()
					}
				}
			}
		}()
	}

	wg.Wait()

	maxWaitDuration := time.Duration(maxWait.Load())
	report.Succeeded = int(attempts.Load())
	report.Failed = 0
	report.Details["sessions"] = sessions
	report.Details["attempts"] = attempts.Load()
	report.Details["max_wait_ms"] = maxWaitDuration.Seconds() * 1000

	return nil
}
