package check

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestStatStatements_Requires(t *testing.T) {
	t.Parallel()
	c := &statStatementsCheck{selectors: newScopedSelectors(cardinality.Options{TopN: 50}), caches: make(map[string]*textCache)}
	req := c.Requires()
	require.Equal(t, ScopeDatabase, req.Scope)
	require.Contains(t, req.Extensions, "pg_stat_statements")
	require.Equal(t, "stat_statements", c.Name())
	require.Equal(t, int64(60000000000), int64(c.DefaultInterval()))
	require.Equal(t, int64(15000000000), int64(c.Timeout()))
}

func TestStatStatements_NegativeQueryID(t *testing.T) {
	t.Parallel()
	queryID := int64(-1234567890)
	queryIDStr := fmt.Sprintf("%d", queryID)
	parsed, err := strconv.ParseInt(queryIDStr, 10, 64)
	require.NoError(t, err)
	require.Equal(t, queryID, parsed)
}

func TestStatStatements_MetricLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		queryID int64
	}{
		{"positive", 12345},
		{"negative", -67890},
		{"zero", 0},
		{"large", 9223372036854775807},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := fmt.Sprintf("%d", tt.queryID)
			roundtrip, err := strconv.ParseInt(label, 10, 64)
			require.NoError(t, err)
			require.Equal(t, tt.queryID, roundtrip)
		})
	}
}

// TestStatStatements_QueryTextCacheIsScopedToClusterAndDatabase pins the scope
// of the query-text LRU to the key the server actually stores text under.
//
// internal/store.QueryTextRow is upserted on (tenant, cluster_id, datname,
// queryid, pg_major) — no instance. The cache exists purely to avoid resending
// text the server already has, so its scope has to be that same key:
//
//   - Keyed by bare database name (the original), two different clusters both
//     monitoring a database called "app" share one cache. The second cluster's
//     texts are suppressed as "already sent" and the server never learns them
//     for that cluster — a silent, permanent loss of query text on any agent
//     watching more than one cluster.
//   - Keyed by instance, a primary and its standby each ship the same text
//     once. Harmless, but a resend the server does not need.
func TestStatStatements_QueryTextCacheIsScopedToClusterAndDatabase(t *testing.T) {
	t.Parallel()

	text := "SELECT 1"
	newTarget := func(cluster string) *SimpleTarget {
		conn := &mockConn{
			execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
				return &mockRows{rows: [][]any{
					{int64(42), int64(100), float64(100), int64(1), int64(0), int64(0), int64(0), int64(0), &text, int64(1)},
				}}, nil
			},
			queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
				var ts *time.Time
				return &mockRow{vals: []any{ts}}
			},
		}
		return &SimpleTarget{
			ConnFunc:        func(ctx context.Context) (Conn, error) { return conn, nil },
			ConnForFunc:     func(ctx context.Context, datname string) (Conn, error) { return conn, nil },
			ClockValue:      clock.System(),
			PGVersionValue:  150000,
			PermTierValue:   1,
			Extensions:      map[string]bool{"pg_stat_statements": true},
			DatabaseValue:   "app",
			ClusterIDValue:  pgtype.ManualClusterID(cluster),
			InstanceIDValue: uuid.New(),
		}
	}

	check := &statStatementsCheck{
		selectors: newScopedSelectors(cardinality.Options{TopN: 50}),
		caches:    make(map[string]*textCache),
	}

	clusterA1 := newTarget("cluster-a")
	res, err := check.Scrape(context.Background(), clusterA1)
	require.NoError(t, err)
	require.Contains(t, res.QueryTexts, int64(42), "first scrape of a cluster must ship the text")

	// Same cluster and database, a different instance (a standby, or simply a
	// re-resolved identity): the server already has this text.
	clusterA2 := newTarget("cluster-a")
	res, err = check.Scrape(context.Background(), clusterA2)
	require.NoError(t, err)
	require.NotContains(t, res.QueryTexts, int64(42), "text already stored for this cluster must not be resent")

	// A different cluster with an identically named database has never had
	// this text stored for it, so it must be shipped.
	clusterB := newTarget("cluster-b")
	res, err = check.Scrape(context.Background(), clusterB)
	require.NoError(t, err)
	require.Contains(t, res.QueryTexts, int64(42), "a second cluster with the same database name must still ship its text")
}

// statementsFixture is a target whose per-database and maintenance
// connections answer the statements query through separate functions.
type statementsFixture struct {
	perDB, maint func(sql string, args []any) (pgx.Rows, error)
	sqls         []string
	args         [][]any
	clk          clock.Clock
}

func (f *statementsFixture) conn(query func(sql string, args []any) (pgx.Rows, error)) *mockConn {
	return &mockConn{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			f.sqls = append(f.sqls, sql)
			f.args = append(f.args, args)
			return query(sql, args)
		},
		queryRowFunc: func(ctx context.Context, sql string, args ...any) pgx.Row {
			var ts *time.Time
			return &mockRow{vals: []any{ts}}
		},
	}
}

func (f *statementsFixture) target() *SimpleTarget {
	clk := f.clk
	if clk == nil {
		clk = clock.System()
	}
	return &SimpleTarget{
		ConnFunc: func(ctx context.Context) (Conn, error) {
			if f.maint == nil {
				return nil, fmt.Errorf("maintenance connection not expected")
			}
			return f.conn(f.maint), nil
		},
		ConnForFunc:     func(ctx context.Context, datname string) (Conn, error) { return f.conn(f.perDB), nil },
		ClockValue:      clk,
		PGVersionValue:  160000,
		PermTierValue:   1,
		Extensions:      map[string]bool{"pg_stat_statements": true},
		DatabaseValue:   "app",
		ClusterIDValue:  pgtype.ManualClusterID("c"),
		InstanceIDValue: uuid.New(),
	}
}

func statementRows(rows ...[]any) func(string, []any) (pgx.Rows, error) {
	return func(string, []any) (pgx.Rows, error) { return &mockRows{rows: rows}, nil }
}

func newStatementsCheck(opts cardinality.Options) *statStatementsCheck {
	return &statStatementsCheck{selectors: newScopedSelectors(opts), caches: make(map[string]*textCache)}
}

// TestStatStatements_AggregatesPerQueryID pins the query shape: one row per
// queryid, summed over every role and nesting level, scoped to the scraped
// database, with the view read only once.
func TestStatStatements_AggregatesPerQueryID(t *testing.T) {
	t.Parallel()
	f := &statementsFixture{perDB: statementRows()}
	_, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
	require.NoError(t, err)
	require.Len(t, f.sqls, 1)
	sql := f.sqls[0]
	require.Contains(t, sql, "GROUP BY queryid")
	require.Contains(t, sql, "sum(calls)")
	require.Contains(t, sql, "sum(temp_blks_written)")
	require.Contains(t, sql, "datname = current_database()")
	require.Equal(t, 1, strings.Count(sql, "FROM pg_stat_statements"), "every reference to the view re-reads the query-text file")
}

// TestStatStatements_FallsBackToMaintenanceDatabase covers the common setup
// where CREATE EXTENSION ran in one database only: the statistics of every
// other database are still readable through it.
func TestStatStatements_FallsBackToMaintenanceDatabase(t *testing.T) {
	t.Parallel()
	text := "SELECT $1"
	f := &statementsFixture{
		perDB: func(string, []any) (pgx.Rows, error) {
			return nil, &pgconn.PgError{Code: "42P01", Message: `relation "pg_stat_statements" does not exist`}
		},
		maint: statementRows([]any{int64(7), int64(3), float64(1.5), int64(3), int64(1), int64(2), int64(0), int64(0), &text, int64(1)}),
	}
	res, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
	require.NoError(t, err)
	require.NotEmpty(t, res.Metrics)
	require.Len(t, f.sqls, 2)
	require.Contains(t, f.sqls[1], "datname = $1")
	require.Equal(t, []any{"app"}, f.args[1], "the fallback must still read only the scraped database's rows")
}

// sourcedTarget is a target that found the pg_stat_statements view in
// another database than the maintenance one.
type sourcedTarget struct {
	*SimpleTarget
	source string
}

func (s sourcedTarget) StatementsDatabase() string { return s.source }

// TestStatStatements_ReadsThroughTheDatabaseTheTargetChose covers an
// extension created in an application database only: the maintenance
// database has no view to fall back to.
func TestStatStatements_ReadsThroughTheDatabaseTheTargetChose(t *testing.T) {
	t.Parallel()
	text := "SELECT $1"
	missing := func(string, []any) (pgx.Rows, error) {
		return nil, &pgconn.PgError{Code: "42P01", Message: `relation "pg_stat_statements" does not exist`}
	}
	f := &statementsFixture{}
	var datnames []string
	target := f.target()
	target.ConnForFunc = func(ctx context.Context, datname string) (Conn, error) {
		datnames = append(datnames, datname)
		if datname == "reporting" {
			return f.conn(statementRows([]any{int64(7), int64(3), float64(1.5), int64(3), int64(1), int64(2), int64(0), int64(0), &text, int64(1)})), nil
		}
		return f.conn(missing), nil
	}
	res, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), sourcedTarget{SimpleTarget: target, source: "reporting"})
	require.NoError(t, err)
	require.NotEmpty(t, res.Metrics)
	require.Equal(t, []string{"app", "reporting"}, datnames, "the maintenance connection is never opened")
	require.Equal(t, []any{"app"}, f.args[1], "the scraped database's rows only")

	target.ConnForFunc = func(ctx context.Context, datname string) (Conn, error) { return f.conn(missing), nil }
	_, err = newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), sourcedTarget{SimpleTarget: target, source: "reporting"})
	require.ErrorContains(t, err, `not installed in database "app" nor in database "reporting"`)
}

// TestStatStatements_MisconfigurationIsAnError covers the failures the old
// substring match swallowed into an empty, successful result.
func TestStatStatements_MisconfigurationIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code, want string
	}{
		{"not preloaded", "55000", "shared_preload_libraries"},
		{"old extension", "42703", "ALTER EXTENSION pg_stat_statements UPDATE"},
		{"permission", "42501", "permission denied for view pg_stat_statements"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &statementsFixture{perDB: func(string, []any) (pgx.Rows, error) {
				return nil, &pgconn.PgError{Code: tc.code, Message: "permission denied for view pg_stat_statements"}
			}}
			_, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestStatStatements_PartialIsNotTruncation keeps the ordinary top-N view
// from reading as a blown cardinality budget.
func TestStatStatements_PartialIsNotTruncation(t *testing.T) {
	t.Parallel()
	f := &statementsFixture{perDB: statementRows(
		[]any{int64(1), int64(10), float64(10), int64(1), int64(0), int64(0), int64(0), int64(0), (*string)(nil), int64(500)},
		[]any{int64(2), int64(20), float64(20), int64(1), int64(0), int64(0), int64(0), int64(0), (*string)(nil), int64(500)},
	)}
	res, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
	require.NoError(t, err)
	require.True(t, res.Partial, "2 of 500 statements reported")
	require.False(t, res.Truncated, "the MaxKeys cap was never reached")

	f = &statementsFixture{perDB: statementRows(
		[]any{int64(1), int64(10), float64(10), int64(1), int64(0), int64(0), int64(0), int64(0), (*string)(nil), int64(1)},
	)}
	res, err = newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
	require.NoError(t, err)
	require.False(t, res.Partial)
	require.False(t, res.Truncated)
}

func TestStatStatements_ReportsTempBlocksWritten(t *testing.T) {
	t.Parallel()
	f := &statementsFixture{perDB: statementRows(
		[]any{int64(9), int64(1), float64(1), int64(1), int64(0), int64(0), int64(0), int64(128), (*string)(nil), int64(1)},
	)}
	res, err := newStatementsCheck(cardinality.Options{TopN: 50}).Scrape(context.Background(), f.target())
	require.NoError(t, err)
	found := false
	for _, m := range res.Metrics {
		if m.Name == "pg_stat_statements_temp_blks_written_total" {
			found = true
			require.Equal(t, float64(128), m.Value)
			require.Equal(t, pgtype.KindCounter, m.Kind)
		}
	}
	require.True(t, found, "temp blocks written must be collected")
}

// TestStatStatements_PrefilterCoversTopN keeps a large configured top_n from
// being silently capped by the SQL pre-filter.
func TestStatStatements_PrefilterCoversTopN(t *testing.T) {
	t.Parallel()
	c := newStatementsCheck(cardinality.Options{TopN: 50})
	c.SetTopN(400)
	f := &statementsFixture{perDB: statementRows()}
	_, err := c.Scrape(context.Background(), f.target())
	require.NoError(t, err)
	require.Contains(t, f.sqls[0], "LIMIT 800")
}

// TestStatStatements_TextIsResent covers a text lost with its envelope: it
// is shipped again once the resend interval passes, and at once if the text
// itself changed.
func TestStatStatements_TextIsResent(t *testing.T) {
	t.Parallel()
	text := "SELECT 1"
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	f := &statementsFixture{clk: clk}
	f.perDB = func(string, []any) (pgx.Rows, error) {
		current := text
		return &mockRows{rows: [][]any{{int64(5), int64(1), float64(1), int64(1), int64(0), int64(0), int64(0), int64(0), &current, int64(1)}}}, nil
	}
	target := f.target()
	c := newStatementsCheck(cardinality.Options{TopN: 50})

	res, err := c.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Contains(t, res.QueryTexts, int64(5))

	res, err = c.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.NotContains(t, res.QueryTexts, int64(5))

	clk.Advance(textResendInterval)
	res, err = c.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Contains(t, res.QueryTexts, int64(5), "the text is resent after the resend interval")

	text = "SELECT 2"
	res, err = c.Scrape(context.Background(), target)
	require.NoError(t, err)
	require.Equal(t, "SELECT 2", res.QueryTexts[5], "a changed text is shipped immediately")
}
