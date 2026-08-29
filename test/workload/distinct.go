package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const rotationPeriod = 65 * time.Second

// distinctQueries generates many distinct queries to test queryid cardinality.
// The key is to generate distinct **parse trees**, not just different literals.
// We emit expressions of increasing length: SELECT 1+1, SELECT 1+1+1, etc.
// This ensures that PostgreSQL's literal normalization doesn't collapse them
// into a single query, and works on all supported versions including 18.
//
// Every query executes EXACTLY ONCE. pg_stat_statements entries persist once
// written — a query's cumulative total_exec_time/calls stay exactly as they
// were left, visible to every later scrape, with no need to re-execute it to
// "keep it visible". An earlier version of this file re-executed the whole
// `count`-sized batch every rotation instead (to satisfy pglens's RATE-based
// metrics — internal/delta.Engine computes calls_rate/exec_time_rate_ms
// between consecutive scrapes, and a query's raw cumulative total_exec_time
// alone, not its rate, is what internal/cardinality.Selector actually ranks
// on, so that repetition was never required for selection in the first
// place). That repetition caused every one of the `count` cold queries to
// have IDENTICAL, ever-growing `calls` — a `count`-way tie whose
// ascending-queryid tiebreak (Select's "top TopN by secondary") could shift
// unpredictably scrape to scrape as transient failures broke individual
// queries' counts asymmetrically, contributing far more "ever fresh" series
// than intended and blowing well past pglens_series_total's budget — found
// live via SYS-LOAD-008. With one execution per query, `calls` is fixed at 1
// for everyone forever after the first scrape, so that tiebreak set is
// stable and contributes a constant, non-growing handful of series, not a
// source of ongoing churn.
//
// Deliberate churn instead comes ONLY from a ROTATING "hot group" of
// hotGroupSize queries, each executed once with a much larger pg_sleep than
// the rest ("cold" queries get 1ms — just enough to be genuinely
// significant and structurally distinct, never enough to contend for
// top-TopN-by-primary). This matters because
// internal/cardinality.Selector's Truncated flag only fires when the fresh
// (top-TopN-by-primary ∪ top-TopN-by-secondary) ∪ retained set exceeds
// MaxKeys (200) — NOT when raw candidates exceed TopN (50). A workload
// where every query has a fixed, stable relative weight makes the
// top-TopN-by-primary set IDENTICAL every scrape: nothing new ever gets
// retained, so keptList never grows past ~100 and Truncated never fires, no
// matter how long the workload runs. Each rotation's hot group has enough
// weight to fully displace every previous group from the
// top-TopN-by-primary window (it sorts strictly higher), so the previous
// group falls back to "retained" — kept for as long as internal/check's
// stat_statements.go's own cycle/Hysteresis window says it should be, and
// for as long as it remains visible in that scrape's SQL-side candidates.
// The rotationPeriod pacing between hot-group assignments (rather than
// assigning every group's weight up front) is what makes each group
// genuinely take a turn as "current leader" at a distinct scrape moment —
// assigning them all at once would make the single highest-weighted group
// permanently win every scrape, with nothing ever transitioning out of
// fresh into retained.
func distinctQueries(ctx context.Context, pool *pgxpool.Pool, count int, duration time.Duration, report *Report) error {
	const hotGroupSize = 50 // matches cardinality.Selector's TopN
	const hotSleepMs = 300
	const coldSleepMs = 1

	numGroups := (count + hotGroupSize - 1) / hotGroupSize
	numRotations := rotationCount(duration, numGroups)

	var successCount, failureCount atomic.Int64

	runRange := func(lo, hi int, sleepMsFor func(i int) int) {
		var wg sync.WaitGroup
		const batchSize = 100
		for start := lo; start < hi; start += batchSize {
			wg.Add(1)
			go func(batchStart int) {
				defer wg.Done()

				end := batchStart + batchSize
				if end > hi {
					end = hi
				}

				for i := batchStart; i < end; i++ {
					// Generate a distinct query by creating an expression of increasing length
					// For i=0: SELECT 1+1
					// For i=1: SELECT 1+1+1
					// For i=2: SELECT 1+1+1+1
					// etc.
					expr := generateExpression(i)
					query := fmt.Sprintf("SELECT %s, pg_sleep(0.001 * %d)", expr, sleepMsFor(i))

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
	}

	hotUpperBound := numRotations * hotGroupSize
	if hotUpperBound > count {
		hotUpperBound = count
	}
	// Cold batch: every query outside the rotation's reach, executed once at
	// minimal weight — up front, all together.
	runRange(hotUpperBound, count, func(int) int { return coldSleepMs })

	rotations := 0
runLoop:
	for round := 0; round < numRotations; round++ {
		lo := round * hotGroupSize
		hi := lo + hotGroupSize
		if hi > count {
			hi = count
		}
		if lo >= hi {
			break
		}
		// Each rotation's boost must clearly outrank every prior rotation's
		// total, or it never displaces the current top-TopN-by-primary set —
		// (round+1) growth keeps each new hot group's total comfortably
		// above every earlier group's.
		weight := hotSleepMs * (round + 1)
		roundStart := time.Now()
		runRange(lo, hi, func(int) int { return weight })
		rotations++

		if round == numRotations-1 {
			break
		}
		if remaining := rotationPeriod - time.Since(roundStart); remaining > 0 {
			select {
			case <-ctx.Done():
				break runLoop
			case <-time.After(remaining):
			}
		}
	}

	report.Succeeded = int(successCount.Load())
	report.Failed = int(failureCount.Load())
	report.Details["count"] = count
	report.Details["rotations"] = rotations
	report.Details["hot_group_size"] = hotGroupSize
	report.Details["queries_generated"] = count
	report.Details["queries_succeeded"] = successCount.Load()
	report.Details["queries_failed"] = failureCount.Load()

	return nil
}

// rotationCount returns the number of hot groups that fit in the requested
// workload window. A duration exactly equal to N rotation periods contains N
// groups; adding one unconditionally makes the workload run one extra group
// beyond its declared duration (SYS-LOAD-008 exposed this at 390s).
func rotationCount(duration time.Duration, groups int) int {
	if duration <= 0 || groups <= 0 {
		return 0
	}
	rotations := int((duration + rotationPeriod - 1) / rotationPeriod)
	if rotations > groups {
		return groups
	}
	return rotations
}

// generateExpression creates a distinct expression for each index.
// Uses increasing lengths of additions to ensure parse trees differ.
// For example:
//
//	0 -> "1+1"
//	1 -> "1+1+1"
//	2 -> "1+1+1+1"
//	etc.
//
// A single ever-deepening "+" chain does not scale to a 5000-query workload
// on its own: PostgreSQL's parser has a fixed max_stack_depth, and a chain
// around ~4000-4200 terms starts failing with "stack depth limit exceeded"
// — confirmed live during SYS-LOAD-008, where ~18% of the highest-index
// queries in a --count 5000 run silently failed for exactly this reason.
// Below chainThreshold this keeps the original single-chain shape (and
// output) unchanged. At and above it, uniqueness instead comes from a
// (width, depth) pair encoded as `width` comma-separated target-list
// columns, only the first of which carries a chain — width stays far under
// PostgreSQL's own ~1664-entry target-list cap, and depth stays far under
// the stack-depth limit, so no index in a 5000-query run can ever hit
// either ceiling. A comma-joined expression can never collide with a
// pre-threshold single-chain one: the former always has "," in it, the
// latter never does.
func generateExpression(index int) string {
	const chainThreshold = 1000
	if index < chainThreshold {
		// Each index adds one more "+1" to the expression
		parts := make([]string, index+2)
		for i := 0; i < len(parts); i++ {
			parts[i] = "1"
		}
		return strings.Join(parts, "+")
	}

	const maxWidth = 50
	offset := index - chainThreshold
	width := 2 + offset%maxWidth
	depth := 2 + offset/maxWidth

	chain := make([]string, depth)
	for i := range chain {
		chain[i] = "1"
	}
	cols := make([]string, width)
	cols[0] = strings.Join(chain, "+")
	for i := 1; i < width; i++ {
		cols[i] = "1"
	}
	return strings.Join(cols, ",")
}
