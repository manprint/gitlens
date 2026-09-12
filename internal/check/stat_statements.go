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
		selectors: newScopedSelectors(cardinality.Options{TopN: 50}),
		caches:    make(map[string]*textCache),
	})
}

type statStatementsCheck struct {
	mu        sync.Mutex
	selectors *scopedSelectors
	caches    map[string]*textCache
}

// textCache remembers which query texts have already been shipped for one
// (cluster, database), so a text the server already stores is not resent on
// every scrape. lastUsed lets an abandoned scope's 4096-entry LRU be released
// instead of pinned for the lifetime of the agent process.
type textCache struct {
	lru      *lru.Cache[int64, string]
	lastUsed time.Time
}

func (c *statStatementsCheck) Name() string { return "stat_statements" }

// SetTopN applies the configured cardinality limit before scheduling begins.
func (c *statStatementsCheck) SetTopN(topN int) { c.selectors.SetTopN(topN) }

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
       LEFT(s.query, 8192) AS query,
       (SELECT count(*) > %d FROM pg_stat_statements, me
         WHERE dbid = me.oid AND queryid IS NOT NULL) AS prefilter_truncated
  FROM pg_stat_statements s
  JOIN ranked r USING (queryid), me
 WHERE s.dbid = me.oid
 ORDER BY s.queryid
`, sqlPrefilterLimit, sqlPrefilterLimit, sqlPrefilterLimit)

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
		QueryID            int64
		Calls              int64
		TotalExecTime      float64
		Rows               int64
		SharedBlksHit      int64
		SharedBlksRd       int64
		WALBytes           int64
		Query              *string
		PrefilterTruncated bool
	}

	var parsed []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.QueryID, &r.Calls, &r.TotalExecTime, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRd, &r.WALBytes, &r.Query, &r.PrefilterTruncated); err != nil {
			return Result{}, err
		}
		parsed = append(parsed, r)
	}

	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	var statsReset *time.Time
	err = conn.QueryRow(ctx, `SELECT stats_reset FROM pg_stat_statements_info`).Scan(&statsReset)
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

	// Keyed by (cluster, database) — deliberately a different scope from the
	// selectors above, which are per (instance, database).
	//
	// This cache exists to avoid resending text the server already has, so its
	// scope has to be the server's own storage key: store.QueryTextRow is
	// upserted on (tenant, cluster_id, datname, queryid, pg_major), with no
	// instance in it. Keying by bare database name (what this did before) was
	// wrong in the direction that loses data: two clusters both monitoring a
	// database called "app" share one cache, so the second cluster's texts are
	// suppressed as "already sent" and the server never learns them for that
	// cluster. Keying by instance instead would be wrong in the harmless
	// direction — a primary and its standby would each ship the same text once
	// — but it is still a resend the server does not need.
	cacheKey := textCacheKey(t)
	now := scopeNow(t)
	c.mu.Lock()
	entry, ok := c.caches[cacheKey]
	if !ok {
		l, _ := lru.New[int64, string](4096)
		entry = &textCache{lru: l}
		c.caches[cacheKey] = entry
	}
	entry.lastUsed = now
	if len(c.caches) > 1 {
		for k, e := range c.caches {
			if !e.lastUsed.IsZero() && now.Sub(e.lastUsed) > scopeIdleTTL {
				delete(c.caches, k)
			}
		}
	}
	cache := entry.lru
	c.mu.Unlock()
	selector, cycle := c.selectors.next(t)

	// A real, monotonically increasing cycle is what makes
	// cardinality.Selector's Hysteresis a genuine N-cycle grace period
	// instead of a permanently open one: Select's own hysteresisThreshold
	// only advances past 0 once cycle > Hysteresis, so passing a constant
	// cycle (as an earlier version of this line did) means any key that
	// was ever fresh is retained forever, for as long as it keeps
	// appearing in the SQL-side candidates — found live via SYS-LOAD-008,
	// where pglens_series_total grew unbounded (2109, no sign of
	// stabilizing) under a churning workload despite MaxKeys correctly
	// capping any single Select() call's result. Forget then prunes
	// exactly what Hysteresis says should no longer be retained, so the
	// check's own retention state stays bounded too, not just its output.
	selected, truncated := selector.Select(cycle, candidates)
	selector.Forget(cycle)
	// The SQL pre-filter deliberately bounds the rows sent over the wire, so
	// the selector cannot observe candidates discarded by that limit. Carry
	// the count-based signal from SQL through to the public result instead of
	// presenting a partial view as complete.
	for _, r := range parsed {
		truncated = truncated || r.PrefilterTruncated
	}
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
