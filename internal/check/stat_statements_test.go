package check

import (
	"context"
	"fmt"
	"strconv"
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
					{int64(42), int64(100), float64(100), int64(1), int64(0), int64(0), int64(0), &text, false},
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
