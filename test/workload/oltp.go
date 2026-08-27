package main

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// oltp generates a pgbench-shaped read/write workload. It's used as realistic
// background noise so scenarios don't run against an unnaturally idle database.
func oltp(ctx context.Context, pool *pgxpool.Pool, targetTPS int, duration time.Duration, report *Report) error {
	done := time.Now().Add(duration)
	txnsPerSecond := float64(targetTPS)
	intervalBetweenTxns := time.Duration(float64(time.Second) / txnsPerSecond)

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failureCount atomic.Int64

	// Launch a few worker goroutines to achieve the target TPS
	numWorkers := 4
	if targetTPS < 10 {
		numWorkers = 1
	}

	txnsPerWorker := targetTPS / numWorkers

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			ticker := time.NewTicker(intervalBetweenTxns)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if time.Now().After(done) {
						return
					}

					err := runOLTPTransaction(ctx, pool)
					if err != nil {
						failureCount.Add(1)
					} else {
						successCount.Add(1)
					}

				case <-ctx.Done():
					return
				}
			}
		}(w)
	}

	// Wait for the duration to elapse
	time.Sleep(duration)

	// Cancel context to stop all workers
	wg.Wait()

	report.Succeeded = int(successCount.Load())
	report.Failed = int(failureCount.Load())
	report.Details["target_tps"] = targetTPS
	report.Details["num_workers"] = numWorkers
	report.Details["txns_per_worker"] = txnsPerWorker
	report.Details["total_transactions"] = successCount.Load() + failureCount.Load()

	return nil
}

// runOLTPTransaction executes a single OLTP transaction (mix of reads and writes).
func runOLTPTransaction(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Randomly decide between read and write operations
	for i := 0; i < 3; i++ {
		if rand.Float64() < 0.7 {
			// 70% reads
			id := rand.Intn(100) + 1
			var value int
			err = tx.QueryRow(ctx, "SELECT value FROM oltp_test WHERE id = $1", id).Scan(&value)
			if err != nil {
				return err
			}
		} else {
			// 30% writes
			id := rand.Intn(100) + 1
			newValue := rand.Intn(1000)
			_, err = tx.Exec(ctx, "UPDATE oltp_test SET value = $1 WHERE id = $2", newValue, id)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}
