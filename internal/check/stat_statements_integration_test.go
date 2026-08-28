//go:build integration

package check

import (
	"context"
	"fmt"
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
