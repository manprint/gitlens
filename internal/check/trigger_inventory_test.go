package check

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func scrapeTriggersForTest(t *testing.T, counts, triggers [][]any) Result {
	t.Helper()
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		switch sql {
		case triggerCountQuery:
			return &mockRows{rows: counts}, nil
		case triggerInventoryQuery:
			require.Equal(t, []any{triggerFactLimit + 1}, args)
			return &mockRows{rows: triggers}, nil
		}
		t.Fatalf("unexpected query %q", sql)
		return nil, nil
	}}
	r, err := (&triggerInventoryCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

func triggerRow(name, state string, internal bool, tgtype int32, constraint, contype string) []any {
	return []any{"public.orders." + name, "public", "orders", name, state, internal, "public.audit_orders", tgtype, constraint, contype}
}

func TestTriggerInventory_Requirements(t *testing.T) {
	c := &triggerInventoryCheck{}
	require.Equal(t, "trigger_inventory", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, 5*time.Minute, c.DefaultInterval())
}

func TestTriggerInventory_ReportsTriggersAndCounts(t *testing.T) {
	r := scrapeTriggersForTest(t,
		[][]any{{false, "O", int64(3)}, {true, "D", int64(2)}},
		[][]any{
			triggerRow("RI_ConstraintTrigger_c_1", "D", true, 1|4, "orders_customer_fkey", "f"),
			triggerRow("audit", "O", false, 1|2|4|16, "", ""),
			triggerRow("refresh", "R", false, 8|32, "", ""),
		})
	require.False(t, r.Truncated)
	require.ElementsMatch(t, []pgtype.Metric{
		{Name: "pg_triggers", Value: 3, Kind: pgtype.KindGauge, Labels: map[string]string{"kind": "user", "state": "enabled"}},
		{Name: "pg_triggers", Value: 2, Kind: pgtype.KindGauge, Labels: map[string]string{"kind": "internal", "state": "disabled"}},
	}, r.Metrics)
	require.Len(t, r.Facts, 3)

	fk := r.Facts[0]
	require.Equal(t, triggerFactKind, fk.Kind)
	require.Equal(t, "public.orders.RI_ConstraintTrigger_c_1", fk.Key)
	require.Equal(t, "disabled", fk.ValueText)
	require.Equal(t, "internal", fk.Labels["kind"])
	require.Equal(t, "orders_customer_fkey", fk.Labels["constraint"])
	require.Equal(t, "foreign key", fk.Labels["constraint_type"])
	require.Equal(t, "AFTER", fk.Labels["timing"])
	require.Equal(t, "INSERT", fk.Labels["events"])
	require.Equal(t, "ROW", fk.Labels["level"])

	audit := r.Facts[1]
	require.Equal(t, "enabled", audit.ValueText)
	require.Equal(t, map[string]string{
		"schemaname": "public", "relname": "orders", "trigger": "audit",
		"kind": "user", "function": "public.audit_orders",
		"timing": "BEFORE", "events": "INSERT OR UPDATE", "level": "ROW",
		"constraint": "", "constraint_type": "",
	}, audit.Labels)

	refresh := r.Facts[2]
	require.Equal(t, "replica", refresh.ValueText)
	require.Equal(t, "DELETE OR TRUNCATE", refresh.Labels["events"])
	require.Equal(t, "STATEMENT", refresh.Labels["level"])
}

// A database with more triggers than the limit reports the first ones (the
// query puts disabled triggers first) and says it was truncated, so the
// server keeps the facts it did not see this time.
func TestTriggerInventory_TruncatesAtTheLimit(t *testing.T) {
	rows := make([][]any, 0, triggerFactLimit+1)
	for i := 0; i <= triggerFactLimit; i++ {
		rows = append(rows, triggerRow("t", "O", false, 1, "", ""))
	}
	r := scrapeTriggersForTest(t, nil, rows)
	require.True(t, r.Truncated)
	require.Len(t, r.Facts, triggerFactLimit)
}

// No trigger is a complete answer: no facts, and the server removes the
// facts of triggers reported earlier.
func TestTriggerInventory_NoTriggers(t *testing.T) {
	r := scrapeTriggersForTest(t, nil, nil)
	require.False(t, r.Truncated)
	require.Empty(t, r.Facts)
	require.Empty(t, r.Metrics)
}

func TestDecodeTriggerType(t *testing.T) {
	for _, tc := range []struct {
		tgtype                int32
		timing, events, level string
	}{
		{0, "AFTER", "", "STATEMENT"},
		{1 | 2 | 4, "BEFORE", "INSERT", "ROW"},
		{1 | 64 | 4 | 8 | 16, "INSTEAD OF", "INSERT OR UPDATE OR DELETE", "ROW"},
		{32, "AFTER", "TRUNCATE", "STATEMENT"},
	} {
		timing, events, level := decodeTriggerType(tc.tgtype)
		require.Equal(t, tc.timing, timing, tc.tgtype)
		require.Equal(t, tc.events, events, tc.tgtype)
		require.Equal(t, tc.level, level, tc.tgtype)
	}
}

func TestTriggerState(t *testing.T) {
	for in, want := range map[string]string{"O": "enabled", "D": "disabled", "R": "replica", "A": "always", "?": "unknown"} {
		require.Equal(t, want, triggerState(in))
	}
}
