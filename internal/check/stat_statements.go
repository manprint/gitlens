package check

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	lru      *lru.Cache[int64, sentText]
	lastUsed time.Time
}

// sentText is one shipped query text and when it was shipped.
type sentText struct {
	text   string
	sentAt time.Time
}

func (c *statStatementsCheck) Name() string { return "stat_statements" }

// StatementsSource is implemented by a target that knows which database's
// pg_stat_statements view stands in for a database without one of its own.
type StatementsSource interface {
	// StatementsDatabase names that database; empty means the maintenance
	// database, the one Target.Conn connects to.
	StatementsDatabase() string
}

func statementsDatabase(t Target) string {
	if s, ok := t.(StatementsSource); ok {
		return s.StatementsDatabase()
	}
	return ""
}

// StatementsConn connects to the database whose pg_stat_statements view is
// read for a database that has none: the maintenance database unless the
// target found the view only in another one.
func StatementsConn(ctx context.Context, t Target) (Conn, error) {
	if db := statementsDatabase(t); db != "" {
		return t.ConnFor(ctx, db)
	}
	return t.Conn(ctx)
}

// StatementsSourceName names StatementsConn's database for an error message.
func StatementsSourceName(t Target) string {
	if db := statementsDatabase(t); db != "" {
		return fmt.Sprintf("database %q", db)
	}
	return "the maintenance database"
}

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

// statementRow is one queryid of the scraped database, already summed over
// every (userid, toplevel) row pg_stat_statements keeps for it.
type statementRow struct {
	QueryID         int64
	Calls           int64
	TotalExecTime   float64
	Rows            int64
	SharedBlksHit   int64
	SharedBlksRead  int64
	WALBytes        int64
	TempBlksWritten int64
	Query           *string
}

// statementsQuery aggregates per queryid before ranking. pg_stat_statements
// keeps one row per (userid, dbid, queryid, toplevel), so the same statement
// run by two roles, or both at top level and inside a function, is several
// rows. Reporting one of them at random (the previous behaviour) undercounted
// the statement, and whenever a different row won the next scrape the counter
// appeared to go backwards, which the server's delta engine reads as a reset.
//
// The view is referenced once: every reference re-reads the whole external
// query-text file. %[1]s selects the database whose rows are read and %[2]d is
// the per-ranking pre-filter.
const statementsQuery = `
WITH agg AS (
    SELECT queryid,
           sum(calls)::bigint             AS calls,
           sum(total_exec_time)::float8   AS total_exec_time,
           sum(rows)::bigint              AS rows,
           sum(shared_blks_hit)::bigint   AS shared_blks_hit,
           sum(shared_blks_read)::bigint  AS shared_blks_read,
           sum(wal_bytes)::bigint         AS wal_bytes,
           sum(temp_blks_written)::bigint AS temp_blks_written,
           min(query)                     AS query
      FROM pg_stat_statements
     WHERE dbid = (SELECT oid FROM pg_database WHERE datname = %[1]s)
       AND queryid IS NOT NULL
     GROUP BY queryid
),
ranked AS (
    (SELECT queryid FROM agg ORDER BY total_exec_time DESC LIMIT %[2]d)
    UNION
    (SELECT queryid FROM agg ORDER BY calls DESC LIMIT %[2]d)
)
SELECT a.queryid, a.calls, a.total_exec_time, a.rows,
       a.shared_blks_hit, a.shared_blks_read, a.wal_bytes, a.temp_blks_written,
       LEFT(a.query, 8192),
       (SELECT count(*) FROM agg)
  FROM agg a
  JOIN ranked USING (queryid)
 ORDER BY a.queryid`

// textResendInterval bounds how long a shipped query text is trusted to be
// stored server-side. An envelope can be lost after the text left the cache
// (buffer overflow, a rejected push), and without a resend the statement
// would stay textless for the lifetime of the agent process.
const textResendInterval = time.Hour

func (c *statStatementsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	// This SQL-side limit exists only to bound what crosses the wire from
	// Postgres. It stays above the selector's own per-ranking budget, or the
	// selector would never see candidates it is supposed to keep.
	prefilter := max(300, 2*c.selectors.TopN())

	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	parsed, distinct, statsReset, err := readStatements(ctx, conn, c.Timeout(), "current_database()", prefilter)
	conn.Release()
	if pgErrorCode(err) == pgUndefinedTable {
		// CREATE EXTENSION creates the view in one database only, while the
		// statistics themselves are cluster-wide. The agent found the view
		// in another database when it scheduled this check, so read this
		// database's rows through it instead of reporting nothing for it.
		source, serr := StatementsConn(ctx, t)
		if serr != nil {
			return Result{}, serr
		}
		parsed, distinct, statsReset, err = readStatements(ctx, source, c.Timeout(), "$1", prefilter, t.Database())
		source.Release()
		if pgErrorCode(err) == pgUndefinedTable {
			return Result{}, fmt.Errorf("pg_stat_statements is not installed in database %q nor in %s: %w", t.Database(), StatementsSourceName(t), err)
		}
	}
	if err != nil {
		return Result{}, explainStatementsError(err)
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
		l, _ := lru.New[int64, sentText](4096)
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
	queryTexts := make(map[int64]string)

	var metrics []pgtype.Metric
	rowsByID := make(map[int64]statementRow, len(parsed))
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
			Value:  float64(r.SharedBlksRead),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_wal_bytes_total",
			Value:  float64(r.WALBytes),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})
		metrics = append(metrics, pgtype.Metric{
			Name:   "pg_stat_statements_temp_blks_written_total",
			Value:  float64(r.TempBlksWritten),
			Kind:   pgtype.KindCounter,
			Labels: labels,
		})

		if r.Query != nil {
			prev, cached := cache.Get(r.QueryID)
			if !cached || prev.text != *r.Query || now.Sub(prev.sentAt) >= textResendInterval {
				queryTexts[r.QueryID] = *r.Query
				cache.Add(r.QueryID, sentText{text: *r.Query, sentAt: now})
			}
		}
	}

	return Result{
		Metrics:    metrics,
		StatsReset: statsReset,
		Truncated:  truncated,
		Partial:    int64(len(selected)) < distinct,
		QueryTexts: queryTexts,
	}, nil
}

// readStatements runs the aggregated statements query on conn and reads the
// extension's reset time. database is the SQL expression naming the database
// whose rows are read; args are its parameters.
func readStatements(ctx context.Context, conn Conn, timeout time.Duration, database string, prefilter int, args ...any) ([]statementRow, int64, *time.Time, error) {
	if err := ApplySessionLimits(ctx, conn, timeout); err != nil {
		return nil, 0, nil, err
	}
	rows, err := conn.Query(ctx, fmt.Sprintf(statementsQuery, database, prefilter), args...)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()

	var parsed []statementRow
	var distinct int64
	for rows.Next() {
		var r statementRow
		if err := rows.Scan(&r.QueryID, &r.Calls, &r.TotalExecTime, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRead, &r.WALBytes, &r.TempBlksWritten,
			&r.Query, &distinct); err != nil {
			return nil, 0, nil, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}

	// pg_stat_statements_info only exists from extension version 1.9 on; an
	// older extension simply has no reset hint to offer.
	var statsReset *time.Time
	err = conn.QueryRow(ctx, `SELECT stats_reset FROM pg_stat_statements_info`).Scan(&statsReset)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && pgErrorCode(err) != pgUndefinedTable {
		return nil, 0, nil, err
	}
	return parsed, distinct, statsReset, nil
}

// explainStatementsError turns the two misconfigurations that used to be
// swallowed as "no data" into errors that say what to fix.
func explainStatementsError(err error) error {
	switch pgErrorCode(err) {
	case pgObjectNotInPrerequisiteState:
		return fmt.Errorf("pg_stat_statements is installed but not loaded (add it to shared_preload_libraries and restart): %w", err)
	case pgUndefinedColumn:
		return fmt.Errorf("the installed pg_stat_statements extension is older than this PostgreSQL release (ALTER EXTENSION pg_stat_statements UPDATE): %w", err)
	}
	return err
}
