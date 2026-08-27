package main

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// idleInTxn creates long-running idle transactions by starting transactions,
// executing one statement, and then holding them open without committing.
func idleInTxn(ctx context.Context, pool *pgxpool.Pool, sessions int, holdTime time.Duration, report *Report) error {
	var wg sync.WaitGroup
	successCount := 0
	failureCount := 0
	var mu sync.Mutex

	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tx, err := pool.Begin(ctx)
			if err != nil {
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			// Execute a statement
			_, err = tx.Exec(ctx, "SELECT 1")
			if err != nil {
				_ = tx.Rollback(ctx)
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()

			// Hold the transaction open for the specified duration
			select {
			case <-time.After(holdTime):
			case <-ctx.Done():
			}

			// Always rollback
			_ = tx.Rollback(ctx)
		}()
	}

	wg.Wait()

	report.Succeeded = successCount
	report.Failed = failureCount
	report.Details["sessions"] = sessions
	report.Details["hold_seconds"] = holdTime.Seconds()

	return nil
}
