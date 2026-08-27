package main

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// deadlock creates deterministic deadlocks by having pairs of transactions
// update two rows in opposite order with a WaitGroup barrier between the first
// and second update to ensure the deadlock happens consistently.
func deadlock(ctx context.Context, pool *pgxpool.Pool, pairs int, duration time.Duration, report *Report) error {
	done := time.Now().Add(duration)
	deadlockCount := 0
	attemptCount := 0

	for time.Now().Before(done) {
		for i := 0; i < pairs; i++ {
			attemptCount++

			// Two goroutines that will try to create a deadlock by updating
			// rows in opposite order with synchronization
			var wg sync.WaitGroup
			var firstUpdated sync.WaitGroup
			wg.Add(2)
			firstUpdated.Add(2)

			// Transaction 1: updates row 1 then row 2
			go func() {
				defer wg.Done()
				tx, err := pool.Begin(ctx)
				if err != nil {
					return
				}
				defer func() { _ = tx.Rollback(ctx) }()

				_, _ = tx.Exec(ctx, "UPDATE deadlock_test SET value = value + 1 WHERE id = 1")
				firstUpdated.Done()
				firstUpdated.Wait() // Wait for both txns to do their first update

				// Small delay to increase chance of deadlock
				time.Sleep(1 * time.Millisecond)

				_, _ = tx.Exec(ctx, "UPDATE deadlock_test SET value = value + 1 WHERE id = 2")
				_ = tx.Commit(ctx)
			}()

			// Transaction 2: updates row 2 then row 1 (opposite order)
			go func() {
				defer wg.Done()
				tx, err := pool.Begin(ctx)
				if err != nil {
					return
				}
				defer func() { _ = tx.Rollback(ctx) }()

				_, _ = tx.Exec(ctx, "UPDATE deadlock_test SET value = value + 1 WHERE id = 2")
				firstUpdated.Done()
				firstUpdated.Wait() // Wait for both txns to do their first update

				// Small delay to increase chance of deadlock
				time.Sleep(1 * time.Millisecond)

				_, _ = tx.Exec(ctx, "UPDATE deadlock_test SET value = value + 1 WHERE id = 1")
				_ = tx.Commit(ctx)
			}()

			wg.Wait()
			deadlockCount++
		}
	}

	report.Succeeded = deadlockCount
	report.Failed = attemptCount - deadlockCount
	report.Details["pairs_attempted"] = attemptCount
	report.Details["deadlocks_created"] = deadlockCount

	return nil
}
