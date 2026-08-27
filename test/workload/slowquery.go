package main

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// slowQuery creates slow queries using pg_sleep. Multiple queries are executed
// concurrently, each holding a connection open for the specified duration.
func slowQuery(ctx context.Context, pool *pgxpool.Pool, sleepDuration time.Duration, count int, report *Report) error {
	var wg sync.WaitGroup
	successCount := 0
	failureCount := 0
	var mu sync.Mutex

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			conn, err := pool.Acquire(ctx)
			if err != nil {
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}
			defer conn.Release()

			// Execute a sleep query
			_, err = conn.Exec(ctx, "SELECT pg_sleep($1)", sleepDuration.Seconds())
			if err != nil {
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
		}()
	}

	wg.Wait()

	report.Succeeded = successCount
	report.Failed = failureCount
	report.Details["count"] = count
	report.Details["sleep_seconds"] = sleepDuration.Seconds()

	return nil
}
