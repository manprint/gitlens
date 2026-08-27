package check

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/pgtype"
)

func init() {
	Register(&statStatementsCheck{
		selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
		caches:   make(map[string]*lru.Cache[int64, string]),
	})
}

type statStatementsCheck struct {
	mu       sync.Mutex
	selector *cardinality.Selector
	caches   map[string]*lru.Cache[int64, string]
}

func (c *statStatementsCheck) Name() string { return "stat_statements" }

func (c *statStatementsCheck) Requires() Requirements {
	return Requirements{
		Scope:      ScopeDatabase,
		PermTier:   pgtype.TierReadOnly,
		Extensions: []string{"pg_stat_statements"},
	}
}

func (c *statStatementsCheck) DefaultInterval() time.Duration { return 60 * time.Second }
func (c *statStatementsCheck) Timeout() time.Duration         { return 15 * time.Second }

func (c *statStatementsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()

	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	// This SQL-side limit exists only to bound what crosses the wire from
	// Postgres — it must stay well above the Go-side cardinality.Selector's
	// real cap (TopN/MaxKeys) or the selector never sees enough candidates to
	// do its own job, and Result.Truncated can never legitimately fire.
	const sqlPrefilterLimit = 300
	query := fmt.Sprintf(`
WITH me AS (SELECT oid FROM pg_database WHERE datname = current_database()),
ranked AS (
    (SELECT queryid FROM pg_stat_statements, me
      WHERE dbid = me.oid AND queryid IS NOT NULL
      ORDER BY total_exec_time DESC LIMIT %d)
    UNION
    (SELECT queryid FROM pg_stat_statements, me
      WHERE dbid = me.oid AND queryid IS NOT NULL
      ORDER BY calls DESC LIMIT %d)
)
SELECT s.queryid, s.calls, s.total_exec_time, s.rows,
       s.shared_blks_hit, s.shared_blks_read, s.wal_bytes,
       LEFT(s.query, 8192) AS query
  FROM pg_stat_statements s
  JOIN ranked r USING (queryid), me
 WHERE s.dbid = me.oid
 ORDER BY s.queryid
`, sqlPrefilterLimit, sqlPrefilterLimit)

	rows, err := conn.Query(ctx, query)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "pg_stat_statements") {
			return Result{}, nil
		}
		return Result{}, err
	}
	defer rows.Close()

	type row struct {
		QueryID       int64
		Calls         int64
		TotalExecTime float64
		Rows          int64
		SharedBlksHit int64
		SharedBlksRd  int64
		WALBytes      int64
		Query         *string
	}

	var parsed []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.QueryID, &r.Calls, &r.TotalExecTime, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRd, &r.WALBytes, &r.Query); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}

	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	var statsReset *time.Time
	err = conn.QueryRow(ctx, `SELECT reset_time FROM pg_stat_statements_info`).Scan(&statsReset)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if !strings.Contains(err.Error(), "does not exist") {
			return Result{}, err
		}
	}

	candidates := make([]cardinality.Candidate, 0, len(parsed))
	for _, r := range parsed {
		candidates = append(candidates, cardinality.Candidate{
			Key:       fmt.Sprintf("%d", r.QueryID),
			Primary:   r.TotalExecTime,
			Secondary: float64(r.Calls),
		})
	}

	datname := t.Database()
	c.mu.Lock()
	cache, ok := c.caches[datname]
	if !ok {
		cache, _ = lru.New[int64, string](4096)
		c.caches[datname] = cache
	}
	c.mu.Unlock()

	selected, truncated := c.selector.Select(0, candidates)
	queryTexts := make(map[int64]string)

	var metrics []pgtype.Metric
	rowsByID := make(map[int64]row)
	for _, r := range parsed {
		rowsByID[r.QueryID] = r
	}

	for _, sel := range selected {
		queryID, err := strconv.ParseInt(sel.Key, 10, 64)
		if err != nil {
			continue
		}
		r, ok := rowsByID[queryID]
		if !ok {
			continue
		}

		labels := map[string]string{"queryid": fmt.Sprintf("%d", r.QueryID)}
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_calls_total",
			Value:  float64(r.Calls),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_total_exec_time_ms",
			Value:  r.TotalExecTime,
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_rows_total",
			Value:  float64(r.Rows),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_shared_blks_hit_total",
			Value:  float64(r.SharedBlksHit),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_shared_blks_read_total",
			Value:  float64(r.SharedBlksRd),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_wal_bytes_total",
			Value:  float64(r.WALBytes),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})

		if r.Query != nil {
			if _, cached := cache.Get(r.QueryID); !cached {
				queryTexts[r.QueryID] = *r.Query
				cache.Add(r.QueryID, *r.Query)
			}
		}
	}

	return Result{
		Metrics:    metrics,
		StatsReset: statsReset,
		Truncated:  truncated,
		QueryTexts: queryTexts,
	}, nil
}
