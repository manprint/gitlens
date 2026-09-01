package main

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// slowQuery creates slow queries using pg_sleep. Multiple queries are executed
// concurrently, each holding a connection open for the specified duration.
func slowQuery(ctx context.Context, pool *pgxpool.Pool, sleepDuration time.Duration, count int, planSafe bool, report *Report) error {
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

			// pg_stat_statements normalizes literal constants to $N. The
			// parameter-free plan-safe variant intentionally uses a stable,
			// moderately expensive catalog query so it is retained in the
			// pg_stat_statements TopN for the UI EXPLAIN acceptance test.
			query := "SELECT pg_sleep($1)"
			args := []any{sleepDuration.Seconds()}
			if planSafe {
				query = "SELECT count(*) FROM pg_catalog.pg_proc CROSS JOIN pg_catalog.pg_class"
				args = nil
			}
			_, err = conn.Exec(ctx, query, args...)
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
