package main

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// raceHotRows bounds how many of the table's `rows` are actually contended.
// Uniformly spreading workers across all `rows` (e.g. 20 workers over 100
// rows) makes genuine collisions the minority case — most sampled instants
// then show each worker idling between its own UPDATE and COMMIT
// (wait_event=ClientRead), which outnumbers the collision samples and wins
// the "dominant wait event" comparison (observed live: ClientRead dominant,
// 354 samples, zero Lock/transactionid). `race` is deliberately a
// worst-case contention generator, not a realistic uniform load (that's
// oltp's job), so it concentrates all workers on a small hot set regardless
// of how large the table itself is — `--rows` still governs how large the
// table is, just not how many of its rows are actually contended.
const raceHotRows = 5

// race creates transactionid lock contention (distinct from lockStorm's
// tuple-level FOR UPDATE contention): N workers each pick a row out of a
// small hot set and UPDATE it, holding the transaction open briefly before
// committing. With few enough hot rows and enough concurrent workers, two
// sessions frequently target the SAME row while one's UPDATE is still
// uncommitted — the second must wait on the first's transaction id, which
// PostgreSQL reports as wait_event_type=Lock, wait_event=transactionid
// (never wait_event=tuple, which is what a plain row lock wait — like
// lockStorm's SELECT...FOR UPDATE — produces instead).
func race(ctx context.Context, pool *pgxpool.Pool, workers, rows int, duration time.Duration, report *Report) error {
	done := time.Now().Add(duration)
	var attempts, conflicts atomic.Int64

	hotRows := raceHotRows
	if rows < hotRows {
		hotRows = rows
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(done) {
				attempts.Add(1)
				id := rand.Intn(hotRows) + 1 //nolint:gosec // deterministic replay via --seed is the point, matching every other workload command's use of the package-level rand source

				start := time.Now()
				err := func() error {
					tx, err := pool.Begin(ctx)
					if err != nil {
						return err
					}
					defer func() { _ = tx.Rollback(ctx) }()

					if _, err := tx.Exec(ctx, "UPDATE race_test SET value = value + 1 WHERE id = $1", id); err != nil {
						return err
					}

					// Hold the row's transaction id long enough for a
					// concurrent worker targeting the same id to actually
					// block on it, and long enough for ASH's 1s sampling
					// cadence to have a real chance of catching the wait.
					time.Sleep(200 * time.Millisecond)

					return tx.Commit(ctx)
				}()

				if err == nil && time.Since(start) > 250*time.Millisecond {
					// A commit that took meaningfully longer than the
					// deliberate 200ms hold itself was waiting on someone
					// else's uncommitted transaction on the same row.
					conflicts.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	report.Succeeded = int(attempts.Load())
	report.Failed = 0
	report.Details["workers"] = workers
	report.Details["rows"] = rows
	report.Details["attempts"] = attempts.Load()
	report.Details["observed_conflicts"] = conflicts.Load()

	return nil
}
