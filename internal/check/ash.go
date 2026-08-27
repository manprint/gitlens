package check

import (
	"context"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&ashCheck{})
}

type ashCheck struct{}

func (c *ashCheck) Name() string { return "ash" }

func (c *ashCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}

func (c *ashCheck) DefaultInterval() time.Duration { return 1 * time.Second }

func (c *ashCheck) Timeout() time.Duration { return 500 * time.Millisecond }

func (c *ashCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()

	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, `
SELECT COALESCE(datname, '')             AS datname,
       COALESCE(state, 'unknown')        AS state,
       COALESCE(wait_event_type, 'CPU')  AS wait_event_type,
       COALESCE(wait_event, 'CPU')       AS wait_event,
       query_id
  FROM pg_stat_activity
 WHERE backend_type = 'client backend'
   AND state <> 'idle'
   AND pid <> pg_backend_pid()
	`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var datname, state, waitEventType, waitEvent string
		var queryID *int64
		if err := rows.Scan(&datname, &state, &waitEventType, &waitEvent, &queryID); err != nil {
			return Result{}, err
		}
		count++
	}

	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	return Result{
		Metrics: []pgtype.Metric{
			// A point-in-time snapshot count of active backends, not a
			// monotonic total — Kind must be Gauge. Leaving Kind unset
			// (its zero value, "") passed pipeline.go's metric-kind switch
			// straight to the default "unknown metric kind" branch, which
			// rejects the ENTIRE instance's push for that cycle — not just
			// this one metric. Because ash runs every second (the shortest
			// interval of any check), that silently discarded every other
			// check's data too, on nearly every single push, the whole
			// session — undetected until a real agent(cmd/pglens-agent/run.go)
			// actually drove a real push against a real server for the
			// first time in phase 5.7's live E2E work.
			{Name: "ash_samples", Value: float64(count), Kind: pgtype.KindGauge},
		},
	}, nil
}
