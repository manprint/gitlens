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

// sentText records one shipped query text: the server's hash of its raw text,
// which tells a changed text without reading it, and when it was shipped.
type sentText struct {
	hash   int64
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
	// TextHash is hashtextextended of the raw query text, nil when unknown.
	TextHash *int64
}

// statementsQuery aggregates per queryid before ranking. pg_stat_statements
// keeps one row per (userid, dbid, queryid, toplevel), so the same statement
// run by two roles, or both at top level and inside a function, is several
// rows. Reporting one of them at random (the previous behaviour) undercounted
// the statement, and whenever a different row won the next scrape the counter
// appeared to go backwards, which the server's delta engine reads as a reset.
//
// The view is referenced once: every reference re-reads the whole external
// query-text file. %[1]s selects the database whose rows are read, %[2]d is
// the per-ranking pre-filter and %[3]s is the view's schema-qualified name.
//
// The text itself is not read here: up to twice the pre-filter rows rank, and
// their texts ran to megabytes on every scrape although the server already
// stored nearly all of them. A hash of each text stands in for it, and
// statementTextsQuery reads only the texts the server lacks.
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
      FROM %[3]s
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
       hashtextextended(a.query, 0),
       (SELECT count(*) FROM agg)
  FROM agg a
  JOIN ranked USING (queryid)
 ORDER BY a.queryid`

// statementTextsQuery reads the raw texts of the queryids in %[4]s, for the
// database %[1]s selects, through the view %[2]s. A text longer than %[3]d
// bytes reads as NULL.
const statementTextsQuery = `
SELECT queryid, CASE WHEN octet_length(q) <= %[3]d THEN q END
  FROM (SELECT queryid, min(query) AS q
          FROM %[2]s
         WHERE dbid = (SELECT oid FROM pg_database WHERE datname = %[1]s)
           AND queryid = ANY (%[4]s::bigint[])
         GROUP BY queryid) t`

// statementTextLimit is the longest query text the texts query reads. A
// longer text is read as NULL and not shipped: only a text read whole can be
// redacted, since a credential's keyword may lie past any cut. PostgreSQL keeps
// utility statements whole, so a long DO block or CREATE FUNCTION keeps its
// full text and is affected by the limit. Only the reported statements whose
// text the server lacks are read, so the worst case per scrape is the
// selector's MaxKeys times this limit, and a steady workload reads none.
const statementTextLimit = 16384

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
	// The texts are read later through the same connection and view, for the
	// same database, as the statistics.
	database, args := "current_database()", []any(nil)
	read, err := readStatements(ctx, conn, c.Timeout(), database, prefilter)
	if pgErrorCode(err) == pgUndefinedTable {
		// CREATE EXTENSION creates the view in one database only, while the
		// statistics themselves are cluster-wide. The agent found the view
		// in another database when it scheduled this check, so read this
		// database's rows through it instead of reporting nothing for it.
		conn.Release()
		conn, err = StatementsConn(ctx, t)
		if err != nil {
			return Result{}, err
		}
		database, args = "$1", []any{t.Database()}
		read, err = readStatements(ctx, conn, c.Timeout(), database, prefilter, args...)
		if pgErrorCode(err) == pgUndefinedTable {
			conn.Release()
			return Result{}, fmt.Errorf("pg_stat_statements is not installed in database %q nor in %s: %w", t.Database(), StatementsSourceName(t), err)
		}
	}
	defer conn.Release()
	if err != nil {
		return Result{}, explainStatementsError(err)
	}
	parsed, distinct, statsReset := read.rows, read.distinct, read.statsReset

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
	var needText []int64

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

		if r.TextHash != nil {
			prev, cached := cache.Get(r.QueryID)
			if !cached || prev.hash != *r.TextHash || now.Sub(prev.sentAt) >= textResendInterval {
				needText = append(needText, r.QueryID)
			}
		}
	}

	queryTexts := make(map[int64]string)
	if len(needText) > 0 {
		texts, err := readStatementTexts(ctx, conn, read.schema, database, needText, args...)
		if err != nil {
			return Result{}, err
		}
		for _, id := range needText {
			raw, ok := texts[id]
			if !ok {
				continue // longer than statementTextLimit, or gone since
			}
			// Redacted before it is kept, so a credential is never shipped.
			// The text is cut to its display size only after redaction,
			// because a cut before it can drop the keyword that makes a value
			// a credential.
			queryTexts[id] = truncateUTF8(redactQueryText(raw), 8192)
			cache.Add(id, sentText{hash: *rowsByID[id].TextHash, sentAt: now})
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
func readStatements(ctx context.Context, conn Conn, timeout time.Duration, database string, prefilter int, args ...any) (statementsRead, error) {
	if err := ApplySessionLimits(ctx, conn, timeout); err != nil {
		return statementsRead{}, err
	}
	schema, err := ExtensionSchema(ctx, conn, "pg_stat_statements")
	if err != nil {
		return statementsRead{}, err
	}
	// With compute_query_id off PostgreSQL computes no query ID, so the view
	// records no statement that runs from then on and keeps the rows it had.
	// Reading those rows would present frozen counters as current, so the
	// check names the setting instead. PostgreSQL 13 has no such setting:
	// pg_stat_statements computes query IDs itself, and missing_ok reads ''.
	var queryIDSetting string
	err = conn.QueryRow(ctx, "SELECT COALESCE(current_setting('compute_query_id', true), '')").Scan(&queryIDSetting)
	if err != nil {
		return statementsRead{}, err
	}
	if queryIDSetting == "off" {
		return statementsRead{}, errors.New("compute_query_id is off, so pg_stat_statements records no new statements (set compute_query_id to auto or on)")
	}
	view := pgx.Identifier{schema, "pg_stat_statements"}.Sanitize()
	rows, err := conn.Query(ctx, fmt.Sprintf(statementsQuery, database, prefilter, view), args...)
	if err != nil {
		return statementsRead{}, err
	}
	defer rows.Close()

	var parsed []statementRow
	var distinct int64
	for rows.Next() {
		var r statementRow
		if err := rows.Scan(&r.QueryID, &r.Calls, &r.TotalExecTime, &r.Rows,
			&r.SharedBlksHit, &r.SharedBlksRead, &r.WALBytes, &r.TempBlksWritten,
			&r.TextHash, &distinct); err != nil {
			return statementsRead{}, err
		}
		parsed = append(parsed, r)
	}
	if err := rows.Err(); err != nil {
		return statementsRead{}, err
	}

	// pg_stat_statements_info only exists from extension version 1.9 on; an
	// older extension simply has no reset hint to offer.
	var statsReset *time.Time
	err = conn.QueryRow(ctx, "SELECT stats_reset FROM "+pgx.Identifier{schema, "pg_stat_statements_info"}.Sanitize()).Scan(&statsReset)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && pgErrorCode(err) != pgUndefinedTable {
		return statementsRead{}, err
	}
	return statementsRead{rows: parsed, distinct: distinct, statsReset: statsReset, schema: schema}, nil
}

// statementsRead is what readStatements read: the ranked rows, the count of
// distinct statements, the extension's reset time and the extension's schema.
type statementsRead struct {
	rows       []statementRow
	distinct   int64
	statsReset *time.Time
	schema     string
}

// readStatementTexts reads the raw texts of ids through the view in schema,
// for the database the SQL expression database names; args are that
// expression's parameters. An id whose text is too long, or that is gone,
// is missing from the result.
func readStatementTexts(ctx context.Context, conn Conn, schema, database string, ids []int64, args ...any) (map[int64]string, error) {
	view := pgx.Identifier{schema, "pg_stat_statements"}.Sanitize()
	query := fmt.Sprintf(statementTextsQuery, database, view, statementTextLimit, fmt.Sprintf("$%d", len(args)+1))
	rows, err := conn.Query(ctx, query, append(append([]any(nil), args...), ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	texts := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var text *string
		if err := rows.Scan(&id, &text); err != nil {
			return nil, err
		}
		if text != nil {
			texts[id] = *text
		}
	}
	return texts, rows.Err()
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
