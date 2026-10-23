package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func scrapeFKIndexesForTest(t *testing.T, rows [][]any) Result {
	t.Helper()
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		require.Equal(t, fkUnindexedQuery, sql)
		require.Equal(t, []any{fkUnindexedFactLimit + 1}, args)
		return &mockRows{rows: rows}, nil
	}}
	r, err := (&fkIndexesCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func fkRow(constraint string, bytes int64) []any {
	return []any{"public.orders." + constraint, "public", "orders", constraint, "public.customers", "customer_id", "customer_id", bytes}
}

func TestFKIndexes_Requirements(t *testing.T) {
	c := &fkIndexesCheck{}
	require.Equal(t, "fk_indexes", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 15*time.Minute, c.DefaultInterval())
}

func TestFKIndexes_ReportsUnindexedForeignKeys(t *testing.T) {
	r := scrapeFKIndexesForTest(t, [][]any{fkRow("orders_customer_fkey", 81920)})
	require.False(t, r.Truncated)
	require.Equal(t, []Fact{{
		Kind: fkUnindexedFactKind, Key: "public.orders.orders_customer_fkey", ValueText: "customer_id",
		Labels: map[string]string{
			"schemaname": "public", "relname": "orders", "constraint": "orders_customer_fkey",
			"referenced": "public.customers", "columns_sql": "customer_id", "table_bytes": "81920",
		},
	}}, r.Facts)
	require.Equal(t, []pgtype.Metric{{Name: "pg_fk_unindexed", Value: 1, Kind: pgtype.KindGauge}}, r.Metrics)
}

// No unindexed foreign key is a complete answer: the gauge says zero and
// the server removes the facts reported earlier.
func TestFKIndexes_None(t *testing.T) {
	r := scrapeFKIndexesForTest(t, nil)
	require.False(t, r.Truncated)
	require.Empty(t, r.Facts)
	require.Equal(t, []pgtype.Metric{{Name: "pg_fk_unindexed", Value: 0, Kind: pgtype.KindGauge}}, r.Metrics)
}

func TestFKIndexes_TruncatesAtTheLimit(t *testing.T) {
	rows := make([][]any, 0, fkUnindexedFactLimit+1)
	for i := 0; i <= fkUnindexedFactLimit; i++ {
		rows = append(rows, fkRow("fk", 1))
	}
	r := scrapeFKIndexesForTest(t, rows)
	require.True(t, r.Truncated)
	require.Len(t, r.Facts, fkUnindexedFactLimit)
}
