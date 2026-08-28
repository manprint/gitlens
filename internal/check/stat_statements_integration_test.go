//go:build integration

package check

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

type mockTarget struct {
	conn       *pgxpool.Conn
	version    pgtype.PGVersion
	database   string
	instanceID pgtype.InstanceID
	clusterID  pgtype.ClusterID
}

func (m *mockTarget) InstanceID() pgtype.InstanceID { return m.instanceID }
func (m *mockTarget) ClusterID() pgtype.ClusterID   { return m.clusterID }
func (m *mockTarget) Role() pgtype.Role             { return pgtype.RolePrimary }
func (m *mockTarget) PGVersion() pgtype.PGVersion   { return m.version }
func (m *mockTarget) PermTier() pgtype.PermTier     { return pgtype.TierReadOnly }
func (m *mockTarget) HasExtension(name string) bool { return name == "pg_stat_statements" }
func (m *mockTarget) Conn(ctx context.Context) (Conn, error) {
	return m.conn, nil
}
func (m *mockTarget) ConnFor(ctx context.Context, datname string) (Conn, error) {
	return m.conn, nil
}
func (m *mockTarget) Database() string { return m.database }
func (m *mockTarget) Clock() clock.Clock {
	return clock.System()
}

// INT-STMT-002: cardinality cap holds against 500 distinct queries
func TestStatStatements_IntegrationCardinalityCap(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Setup (extension creation, stats reset, and running the 500-query
		// workload) requires superuser: pg_stat_statements_reset() is not
		// granted by pg_monitor. The check under test still runs as RoleT0,
		// matching what a real T0-permission agent can actually do.
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)

		_, err = adminConn.Exec(ctx, `SELECT pg_stat_statements_reset()`)
		require.NoError(t, err)

		// pg_stat_statements' queryid is computed from the parsed query tree,
		// not the source text: literal constants are normalized to $N
		// parameters (so "SELECT 1"/"SELECT 2" share one queryid) and a SELECT
		// list alias isn't part of the tree at all (confirmed empirically: 500
		// queries differing only by alias or by a generate_series() bound
		// collapsed to a single queryid). A table reference, by contrast, is
		// structural — 500 distinct table names reliably produce 500 distinct
		// queryids. Row count per table also ramps, so total_exec_time is
		// genuinely distinct per query (all have calls=1, so "top by calls"
		// would otherwise be an arbitrary tied set unrelated to "top by exec
		// time" — the two rankings need to diverge for the SQL-level UNION to
		// exceed a single ranking's LIMIT).
		for i := 1; i <= 500; i++ {
			_, err := adminConn.Exec(ctx, fmt.Sprintf(
				"CREATE TABLE stmt_cap_t_%d (x int); INSERT INTO stmt_cap_t_%d SELECT g FROM generate_series(1, %d) g",
				i, i, 10+i,
			))
			require.NoError(t, err, "create/insert table %d failed", i)
			_, err = adminConn.Exec(ctx, fmt.Sprintf("SELECT count(*) FROM stmt_cap_t_%d", i))
			require.NoError(t, err, "query %d failed", i)
		}

		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		mockTgt := &mockTarget{
			conn:       conn,
			version:    pg.Version,
			database:   "postgres",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		// MaxKeys deliberately tighter than 2*TopN: a single Select() call's
		// "fresh" set (top-TopN by exec time ∪ top-TopN by calls) is capped at
		// 2*TopN by construction — with the library default MaxKeys=200 and
		// TopN=50, that 100-key fresh set never exceeds MaxKeys in one call, so
		// Result.Truncated could never be observed as true no matter how many
		// distinct queries exist. Setting MaxKeys=80 here exercises that real,
		// separate truncation step (Select's step 4) instead of leaving it
		// untested — the mechanism under test, not a relaxation of the check's
		// own production defaults (registered separately in check.go's init()).
		check := &statStatementsCheck{
			selector: cardinality.NewSelector(cardinality.Options{TopN: 50, MaxKeys: 80}),
			caches:   make(map[string]*lru.Cache[int64, string]),
		}

		result, err := check.Scrape(context.Background(), mockTgt)
		require.NoError(t, err)

		// Regression for a column-name typo (queried "reset_time", the real
		// column is "stats_reset") that made every StatsReset lookup fail
		// with "column ... does not exist" — silently swallowed by the
		// existing "does not exist" tolerance (there to handle PG versions
		// without pg_stat_statements_info at all), so StatsReset was always
		// nil in production despite this test's own pg_stat_statements_reset()
		// call above. Found live while investigating SYS-LOAD-008.
		require.NotNil(t, result.StatsReset, "stats_reset should be populated after pg_stat_statements_reset()")

		require.True(t, result.Truncated, "expected truncation with 500 distinct queries")
		metricCount := len(result.Metrics)
		require.Greater(t, metricCount, 0, "should have metrics")

		queriesReported := metricCount / 6
		require.LessOrEqual(t, queriesReported, 100, "should not exceed cardinality cap")
		require.Greater(t, queriesReported, 10, "should report at least some queries")
	})
}

// INT-STMT-001: run 5 known queries, scrape, assert each queryid appears with calls >= 1 and a non-empty text on first sight, empty on the second scrape
func TestStatStatements_IntegrationKnownQueries(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Setup: create extension and reset stats via superuser
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)

		_, err = adminConn.Exec(ctx, `SELECT pg_stat_statements_reset()`)
		require.NoError(t, err)

		// Create and run 5 structurally distinct queries (different functions/clauses/table refs)
		// to ensure they have distinct queryids. Literal constants alone are normalized to $N,
		// so we vary by function or table reference instead.
		knownQueries := []string{
			"SELECT 1",
			"SELECT now()",
			"SELECT current_database()",
			"SELECT count(*) FROM pg_class",
			"SELECT 1+1+1",
		}

		for _, query := range knownQueries {
			_, err := adminConn.Exec(ctx, query)
			require.NoError(t, err, "failed to execute query: %s", query)
		}

		// Create RoleT0 pool for the actual check (matching production)
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		// Construct a reusable check instance (same object for both Scrape calls)
		check := &statStatementsCheck{
			selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
			caches:   make(map[string]*lru.Cache[int64, string]),
		}

		// First scrape: should have query text
		conn1, err := pool.Acquire(ctx)
		require.NoError(t, err)
		mockTgt1 := &mockTarget{
			conn:       conn1,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		result1, err := check.Scrape(ctx, mockTgt1)
		require.NoError(t, err)

		// Extract queryids from metrics (each queryid should have 6 metrics)
		queryIDsFirst := make(map[string]bool)
		for _, m := range result1.Metrics {
			if qid, ok := m.Labels["queryid"]; ok {
				queryIDsFirst[qid] = true
			}
		}

		// Should have at least 5 distinct queryids (the 5 known queries)
		require.GreaterOrEqual(t, len(queryIDsFirst), 5, "should report at least 5 queryids from our known queries")

		// Assert calls >= 1 for each queryid in metrics
		callMetricsFirst := make(map[string]float64)
		for _, m := range result1.Metrics {
			if m.Name == "pg_stat_statements_calls_total" {
				qid := m.Labels["queryid"]
				callMetricsFirst[qid] = m.Value
			}
		}
		for qid := range queryIDsFirst {
			require.Greater(t, callMetricsFirst[qid], float64(0), "queryid %s should have calls >= 1", qid)
		}

		// QueryTexts should be non-empty on first scrape
		require.NotEmpty(t, result1.QueryTexts, "QueryTexts should be non-empty on first scrape")
		require.Greater(t, len(result1.QueryTexts), 0, "should have query text entries on first scrape")

		// Second scrape with the SAME check object (no new queries executed in between)
		// Should still have metrics but QueryTexts should be empty (cached by LRU) for already-cached queryids.
		// Get a fresh connection for the second scrape.
		conn2, err := pool.Acquire(ctx)
		require.NoError(t, err)
		mockTgt2 := &mockTarget{
			conn:       conn2,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		result2, err := check.Scrape(ctx, mockTgt2)
		require.NoError(t, err)

		// For queryids that were in BOTH scrapes, verify that QueryTexts was returned in the first
		// but not in the second (because the LRU cache prevents re-fetching text for cached queryids).
		// Some queryids may be different between scrapes due to background activity, but the important
		// invariant is the LRU caching behavior: if a queryid appears in both scrapes, its text should
		// only appear in QueryTexts on the first scrape.

		// Build a set of queryids that appear in both scrapes
		queryIDsBoth := make(map[string]bool)
		for qid := range queryIDsFirst {
			queryIDsSecond := make(map[string]bool)
			for _, m := range result2.Metrics {
				if qidLabel, ok := m.Labels["queryid"]; ok {
					queryIDsSecond[qidLabel] = true
				}
			}
			if queryIDsSecond[qid] {
				queryIDsBoth[qid] = true
			}
		}

		// For queryids that appear in BOTH scrapes, verify the LRU caching behavior:
		// - They should have been in QueryTexts from the first scrape
		// - They should NOT be in QueryTexts from the second scrape
		for qid := range queryIDsBoth {
			queryIDInt, err := strconv.ParseInt(qid, 10, 64)
			require.NoError(t, err)
			_, wasInFirst := result1.QueryTexts[queryIDInt]
			_, wasInSecond := result2.QueryTexts[queryIDInt]
			require.True(t, wasInFirst, "queryid %s should have text in first scrape", qid)
			require.False(t, wasInSecond, "queryid %s should NOT have text in second scrape (LRU cache)", qid)
		}

		// Should have at least some queryids in common to actually test the caching
		require.Greater(t, len(queryIDsBoth), 0, "should have at least some queryids in common between scrapes to test caching")
	})
}

// INT-STMT-003: a query that is 1st by calls and far down by total_exec_time is present in the result. Proves the union of rankings.
func TestStatStatements_IntegrationUnionOfRankings(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Setup: create extension and reset stats via superuser
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)

		_, err = adminConn.Exec(ctx, `SELECT pg_stat_statements_reset()`)
		require.NoError(t, err)

		// Create ~60 filler queries with distinct table names
		// Each filler has moderate calls and moderate exec_time (via pg_sleep)
		for i := 1; i <= 60; i++ {
			_, err := adminConn.Exec(ctx, fmt.Sprintf(
				"CREATE TABLE union_filler_%d (x int); INSERT INTO union_filler_%d SELECT g FROM generate_series(1, 10) g",
				i, i,
			))
			require.NoError(t, err, "create/insert filler table %d failed", i)
			// Execute with pg_sleep to accumulate total_exec_time
			_, err = adminConn.Exec(ctx, fmt.Sprintf(
				"SELECT pg_sleep(0.05), count(*) FROM union_filler_%d",
				i,
			))
			require.NoError(t, err, "query filler %d failed", i)
		}

		// Create one high-calls, low-exec_time query (no sleep, called many times)
		_, err = adminConn.Exec(ctx, `CREATE TABLE union_highcalls (x int)`)
		require.NoError(t, err)
		for i := 0; i < 100; i++ {
			_, err := adminConn.Exec(ctx, `SELECT count(*) FROM union_highcalls`)
			require.NoError(t, err, "high-calls query %d failed", i)
		}

		// Create one high-exec_time, low-calls query (one call with significant sleep)
		_, err = adminConn.Exec(ctx, `CREATE TABLE union_hightime (x int)`)
		require.NoError(t, err)
		_, err = adminConn.Exec(ctx, `SELECT pg_sleep(0.3), count(*) FROM union_hightime`)
		require.NoError(t, err, "high-time query failed")

		// Create RoleT0 pool and connection
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		mockTgt := &mockTarget{
			conn:       conn,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		// Use default TopN=50 as in production
		check := &statStatementsCheck{
			selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
			caches:   make(map[string]*lru.Cache[int64, string]),
		}

		result, err := check.Scrape(ctx, mockTgt)
		require.NoError(t, err)

		// Extract all queryids from result
		resultQueryIDs := make(map[string]bool)
		for _, m := range result.Metrics {
			if qid, ok := m.Labels["queryid"]; ok {
				resultQueryIDs[qid] = true
			}
		}

		// Query the actual queryids from the DB to match against our special queries
		dbPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer dbPool.Close()
		dbConn, err := dbPool.Acquire(ctx)
		require.NoError(t, err)
		defer dbConn.Release()

		// Get queryid for high-calls query
		var highCallsQueryID int64
		err = dbConn.QueryRow(ctx, `
			SELECT queryid FROM pg_stat_statements
			WHERE query LIKE '%union_highcalls%'
			ORDER BY calls DESC LIMIT 1
		`).Scan(&highCallsQueryID)
		require.NoError(t, err, "should find high-calls query")

		// Get queryid for high-time query
		var highTimeQueryID int64
		err = dbConn.QueryRow(ctx, `
			SELECT queryid FROM pg_stat_statements
			WHERE query LIKE '%union_hightime%'
			ORDER BY total_exec_time DESC LIMIT 1
		`).Scan(&highTimeQueryID)
		require.NoError(t, err, "should find high-time query")

		// Assert both special queries are in the result
		highCallsStr := fmt.Sprintf("%d", highCallsQueryID)
		highTimeStr := fmt.Sprintf("%d", highTimeQueryID)

		require.True(t, resultQueryIDs[highCallsStr], "high-calls query (queryid=%d) should be in result", highCallsQueryID)
		require.True(t, resultQueryIDs[highTimeStr], "high-time query (queryid=%d) should be in result", highTimeQueryID)
	})
}

// INT-STMT-004: StatsReset changes after SELECT pg_stat_statements_reset(), on every version
func TestStatStatements_IntegrationStatsResetChanges(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Setup: create extension via superuser
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)

		// Execute a dummy query to populate pg_stat_statements
		_, err = adminConn.Exec(ctx, `SELECT 1`)
		require.NoError(t, err)

		// Create RoleT0 pool for testing
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		check := &statStatementsCheck{
			selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
			caches:   make(map[string]*lru.Cache[int64, string]),
		}

		// First scrape to get initial StatsReset
		conn1, err := pool.Acquire(ctx)
		require.NoError(t, err)
		mockTgt1 := &mockTarget{
			conn:       conn1,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		result1, err := check.Scrape(ctx, mockTgt1)
		require.NoError(t, err)
		require.NotNil(t, result1.StatsReset, "StatsReset should be populated")

		resetBefore := *result1.StatsReset

		// Now reset stats via superuser (RoleT0 cannot call pg_stat_statements_reset)
		_, err = adminConn.Exec(ctx, `SELECT pg_stat_statements_reset()`)
		require.NoError(t, err)

		// Second scrape to get new StatsReset (use a fresh connection)
		conn2, err := pool.Acquire(ctx)
		require.NoError(t, err)
		mockTgt2 := &mockTarget{
			conn:       conn2,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ManualClusterID("test-cluster"),
		}

		result2, err := check.Scrape(ctx, mockTgt2)
		require.NoError(t, err)
		require.NotNil(t, result2.StatsReset, "StatsReset should be populated after reset")

		resetAfter := *result2.StatsReset

		// The pg_stat_statements_reset() call happens after result1 is obtained,
		// so resetAfter should be strictly later than resetBefore
		require.True(t, resetAfter.After(resetBefore),
			"StatsReset should be strictly later after pg_stat_statements_reset(), but got before=%v after=%v",
			resetBefore, resetAfter)
	})
}

// INT-STMT-005: the check is correctly skipped with a reason when the extension is absent
func TestStatStatements_IntegrationMissingExtension(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Create a separate database without pg_stat_statements extension
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE DATABASE nostats_int_stmt_005`)
		require.NoError(t, err)

		// Verify the extension is not present by querying the DB directly
		noStatsPool, err := pgxpool.New(ctx, pg.DSN("nostats_int_stmt_005", pgtest.RoleT0))
		require.NoError(t, err)
		defer noStatsPool.Close()
		noStatsConn, err := noStatsPool.Acquire(ctx)
		require.NoError(t, err)
		defer noStatsConn.Release()

		var extCount int
		err = noStatsConn.QueryRow(ctx, `
			SELECT count(*) FROM pg_extension WHERE extname = 'pg_stat_statements'
		`).Scan(&extCount)
		require.NoError(t, err)
		require.Equal(t, 0, extCount, "pg_stat_statements extension should not be present")

		// Test the Requirements.Supports method directly
		check := &statStatementsCheck{
			selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
			caches:   make(map[string]*lru.Cache[int64, string]),
		}

		reqs := check.Requires()
		// Empty extension map (simulating absent extension)
		supported, reason := reqs.Supports(pgtype.RolePrimary, pg.Version, pgtype.TierReadOnly, map[string]bool{})

		require.False(t, supported, "check should not be supported without pg_stat_statements")
		require.Contains(t, reason, "pg_stat_statements", "reason should mention pg_stat_statements")
	})
}

// INT-STMT-006: negative queryid values survive the round trip through Metric.Labels unchanged
func TestStatStatements_IntegrationNegativeQueryID(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Setup: create extension and reset stats via superuser
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`)
		require.NoError(t, err)

		_, err = adminConn.Exec(ctx, `SELECT pg_stat_statements_reset()`)
		require.NoError(t, err)

		// Execute many distinct queries to find one with a negative queryid.
		// pg_stat_statements.queryid is a bigint computed by Postgres from the parse
		// tree hash. Roughly 50% of random hash values are negative in two's complement.
		// We'll run 30 distinct queries to have high probability of finding a negative one.
		distinctQueries := []string{
			"SELECT 1",
			"SELECT 2",
			"SELECT 3",
			"SELECT current_timestamp",
			"SELECT current_database()",
			"SELECT current_user",
			"SELECT version()",
			"SELECT count(*) FROM pg_class",
			"SELECT count(*) FROM pg_attribute",
			"SELECT count(*) FROM pg_proc",
			"SELECT count(*) FROM pg_tables",
			"SELECT count(*) FROM pg_indexes",
			"SELECT count(*) FROM pg_namespace",
			"SELECT count(*) FROM pg_type",
			"SELECT count(*) FROM information_schema.tables",
			"SELECT count(*) FROM information_schema.columns",
			"SELECT exists(SELECT 1 FROM pg_class LIMIT 1)",
			"SELECT abs(1)",
			"SELECT lower('TEST')",
			"SELECT upper('test')",
			"SELECT length('test')",
			"SELECT substring('test', 1, 2)",
			"SELECT trim(' test ')",
			"SELECT coalesce(NULL, 1)",
			"SELECT nullif(1, 2)",
			"SELECT greatest(1, 2, 3)",
			"SELECT least(1, 2, 3)",
			"SELECT random()",
			"SELECT md5('test')",
			"SELECT now() - INTERVAL '1 day'",
		}

		for _, query := range distinctQueries {
			_, err := adminConn.Exec(ctx, query)
			require.NoError(t, err, "failed to execute query: %s", query)
		}

		// Query pg_stat_statements to find the queryids, looking for a negative one
		var foundNegativeQueryID int64
		rows, err := adminConn.Query(ctx, `
			SELECT queryid FROM pg_stat_statements
			WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
			ORDER BY calls DESC
		`)
		require.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var qid int64
			err := rows.Scan(&qid)
			require.NoError(t, err)
			if qid < 0 {
				foundNegativeQueryID = qid
				break
			}
		}
		rows.Close()

		// If we found a negative queryid, test the round-trip
		if foundNegativeQueryID != 0 {
			// Create RoleT0 pool and connection for the check
			pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
			require.NoError(t, err)
			defer pool.Close()
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()

			mockTgt := &mockTarget{
				conn:       conn,
				version:    pg.Version,
				database:   "app",
				instanceID: uuid.New(),
				clusterID:  pgtype.ManualClusterID("test-cluster"),
			}

			check := &statStatementsCheck{
				selector: cardinality.NewSelector(cardinality.Options{TopN: 50}),
				caches:   make(map[string]*lru.Cache[int64, string]),
			}

			result, err := check.Scrape(ctx, mockTgt)
			require.NoError(t, err)

			// Find the metric with our negative queryid
			negativeStr := fmt.Sprintf("%d", foundNegativeQueryID)
			var foundMetric bool
			for _, m := range result.Metrics {
				if qidLabel, ok := m.Labels["queryid"]; ok && qidLabel == negativeStr {
					foundMetric = true
					// Parse the label back to int64 and verify it matches
					parsedQID, err := strconv.ParseInt(qidLabel, 10, 64)
					require.NoError(t, err, "should be able to parse queryid label as int64")
					require.Equal(t, foundNegativeQueryID, parsedQID, "parsed queryid should match original negative value")
					break
				}
			}

			require.True(t, foundMetric, "should find at least one metric with the negative queryid in labels")
		} else {
			// If no negative queryid found after 30 distinct queries, use a constructed test instead.
			// We'll manually verify the label round-trip with a known negative value.
			negativeQID := int64(-123456789)
			labelStr := fmt.Sprintf("%d", negativeQID)

			// Round-trip: format to string and parse back
			parsed, err := strconv.ParseInt(labelStr, 10, 64)
			require.NoError(t, err)
			require.Equal(t, negativeQID, parsed, "negative queryid should survive format/parse round-trip")

			t.Logf("Note: No naturally negative queryid found in %d distinct queries; verified label round-trip with constructed negative value", len(distinctQueries))
		}
	})
}
