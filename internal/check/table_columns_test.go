package check

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func scrapeTableColumnsForTest(t *testing.T, rows [][]any) Result {
	t.Helper()
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		require.Equal(t, tableColumnsQuery, sql)
		require.Equal(t, []any{tableColumnsFactLimit + 1}, args)
		return &mockRows{rows: rows}, nil
	}}
	r, err := (&tableColumnsCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func columnsRow(table, columns string) []any {
	return []any{"public." + table, "public", table, "r", 1250.0, int64(40), columns}
}

func TestTableColumns_Requirements(t *testing.T) {
	c := &tableColumnsCheck{}
	require.Equal(t, "table_columns", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	// The catalogs are enough: pg_stats adds to the answer when the role may read it.
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, time.Hour, c.DefaultInterval())
}

func TestTableColumns_ReportsOneFactPerTable(t *testing.T) {
	columns := `[{"name":"id","attnum":1,"type":"integer","typlen":4,"notnull":true,"n_distinct":-1,"null_frac":0,"avg_width":4},{"name":"note","attnum":2,"type":"text","typlen":-1,"notnull":false}]`
	r := scrapeTableColumnsForTest(t, [][]any{columnsRow("orders", columns)})
	require.False(t, r.Truncated)
	require.Len(t, r.Facts, 1)
	fact := r.Facts[0]
	require.Equal(t, tableColumnsFactKind, fact.Kind)
	require.Equal(t, "public.orders", fact.Key)
	require.Equal(t, map[string]string{
		"schemaname": "public", "relname": "orders", "relkind": "r",
		"reltuples": "1250", "relpages": "40",
	}, fact.Labels)
	require.Empty(t, fact.ValueText)
	// The value is the column list, as the query built it.
	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(fact.ValueJSON, &decoded))
	require.Len(t, decoded, 2)
	require.Equal(t, "note", decoded[1]["name"])
	// The kind is registered, so the agent does not drop the fact before sending it.
	kind, ok := wire.LookupFactKind(tableColumnsFactKind)
	require.True(t, ok)
	require.Equal(t, "table_columns", kind.Check)
	require.True(t, kind.Replace)
}

// A table nobody analysed reports -1 tuples: the label carries it as it is and
// the server treats it as unknown.
func TestTableColumns_KeepsAnUnanalysedTableOutOfTheEstimates(t *testing.T) {
	row := []any{"public.fresh", "public", "fresh", "r", -1.0, int64(0), "[]"}
	r := scrapeTableColumnsForTest(t, [][]any{row})
	require.Equal(t, "-1", r.Facts[0].Labels["reltuples"])
	require.JSONEq(t, "[]", string(r.Facts[0].ValueJSON))
}

// No table is a complete answer: the server removes the facts reported earlier.
func TestTableColumns_None(t *testing.T) {
	r := scrapeTableColumnsForTest(t, nil)
	require.False(t, r.Truncated)
	require.Empty(t, r.Facts)
}

func TestTableColumns_TruncatesAtTheLimit(t *testing.T) {
	rows := make([][]any, 0, tableColumnsFactLimit+1)
	for i := 0; i <= tableColumnsFactLimit; i++ {
		rows = append(rows, columnsRow("t", "[]"))
	}
	r := scrapeTableColumnsForTest(t, rows)
	require.True(t, r.Truncated)
	require.Len(t, r.Facts, tableColumnsFactLimit)
}
