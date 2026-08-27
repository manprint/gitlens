package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// distinctQueries generates many distinct queries to test queryid cardinality.
// The key is to generate distinct **parse trees**, not just different literals.
// We emit expressions of increasing length: SELECT 1+1, SELECT 1+1+1, etc.
// This ensures that PostgreSQL's literal normalization doesn't collapse them
// into a single query, and works on all supported versions including 18.
func distinctQueries(ctx context.Context, pool *pgxpool.Pool, count int, report *Report) error {
	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failureCount atomic.Int64

	// Process in parallel batches to speed up execution
	batchSize := 100
	for start := 0; start < count; start += batchSize {
		wg.Add(1)
		go func(batchStart int) {
			defer wg.Done()

			end := batchStart + batchSize
			if end > count {
				end = count
			}

			for i := batchStart; i < end; i++ {
				// Generate a distinct query by creating an expression of increasing length
				// For i=0: SELECT 1+1
				// For i=1: SELECT 1+1+1
				// For i=2: SELECT 1+1+1+1
				// etc.
				expr := generateExpression(i)
				query := fmt.Sprintf("SELECT %s", expr)

				_, err := pool.Exec(ctx, query)
				if err != nil {
					failureCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(start)
	}

	wg.Wait()

	report.Succeeded = int(successCount.Load())
	report.Failed = int(failureCount.Load())
	report.Details["count"] = count
	report.Details["queries_generated"] = count
	report.Details["queries_succeeded"] = successCount.Load()
	report.Details["queries_failed"] = failureCount.Load()

	return nil
}

// generateExpression creates a distinct expression for each index.
// Uses increasing lengths of additions to ensure parse trees differ.
// For example:
//
//	0 -> "1+1"
//	1 -> "1+1+1"
//	2 -> "1+1+1+1"
//	etc.
func generateExpression(index int) string {
	// Each index adds one more "+1" to the expression
	parts := make([]string, index+2)
	for i := 0; i < len(parts); i++ {
		parts[i] = "1"
	}
	return strings.Join(parts, "+")
}
