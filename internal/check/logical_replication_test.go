package check

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func TestLogicalReplication_Requirements(t *testing.T) {
	c := &logicalReplicationCheck{}
	require.Equal(t, "logical_replication", c.Name())
	require.Equal(t, ScopeInstance, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Equal(t, time.Minute, c.DefaultInterval())
}

// subconninfo holds the subscription's connection string, password
// included: the check never names it, and never reads pg_subscription as a
// whole row, which would include it.
func TestLogicalReplication_NeverReadsTheConnectionString(t *testing.T) {
	require.NotContains(t, subscriptionsQuery, "subconninfo")
	wholeRow := regexp.MustCompile(`(?i)(to_jsonb|row_to_json|to_json)\(\s*s\s*\)|\bs\.\*`)
	require.False(t, wholeRow.MatchString(subscriptionsQuery))
}

func scrapeLogicalReplicationForTest(t *testing.T, subs, slots [][]any) Result {
	t.Helper()
	conn := &mockConn{queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		switch sql {
		case subscriptionsQuery:
			require.Equal(t, []any{subscriptionFactLimit + 1}, args)
			return &mockRows{rows: subs}, nil
		case logicalSlotStatsQuery:
			return &mockRows{rows: slots}, nil
		}
		t.Fatalf("unexpected query %q", sql)
		return nil, nil
	}}
	r, err := (&logicalReplicationCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r
}

// Statistics columns differ by version: 15 to 18 count sync errors in one
// column, 19 in two; 18 adds conflict counters. Both shapes give the same
// metrics.
func TestLogicalReplication_StatsAcrossVersions(t *testing.T) {
	receipt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Unix()
	r := scrapeLogicalReplicationForTest(t, [][]any{
		{"old", "app", "postgres", true, "old", "p1,p2", true, int64(1), receipt,
			`{"subid": 1, "subname": "old", "apply_error_count": 2, "sync_error_count": 3, "stats_reset": null}`},
		{"new", "crm", "postgres", false, "", "p", false, int64(0), int64(0),
			`{"subid": 2, "subname": "new", "apply_error_count": 0, "sync_seq_error_count": 1, "sync_table_error_count": 4, "confl_insert_exists": 5, "confl_update_missing": 1}`},
	}, [][]any{
		{"old", `{"slot_name": "old", "spill_txns": 1, "spill_bytes": 4096, "stream_txns": 0, "stream_bytes": 0, "total_txns": 10, "total_bytes": 8192, "mem_exceeded_count": 2}`},
	})
	require.False(t, r.Truncated)
	require.Len(t, r.Facts, 2)
	old, fresh := r.Facts[0], r.Facts[1]
	require.Equal(t, "enabled", old.ValueText)
	require.Equal(t, map[string]string{
		"database": "app", "owner": "postgres", "slot_name": "old", "publications": "p1,p2",
		"apply_worker": "true", "sync_workers": "1", "last_msg_receipt_at": "2026-10-01T12:00:00Z",
		"apply_errors": "2", "sync_errors": "3", "conflicts": "",
	}, old.Labels)
	require.Equal(t, "disabled", fresh.ValueText)
	require.Equal(t, "5", fresh.Labels["sync_errors"])
	require.Equal(t, "6", fresh.Labels["conflicts"])
	require.Empty(t, fresh.Labels["last_msg_receipt_at"])

	metrics := map[string]float64{}
	for _, m := range r.Metrics {
		metrics[m.Name+"|"+pgtype.CanonicalLabels(m.Labels)] = m.Value
	}
	require.Equal(t, 3.0, metrics["pg_subscription_sync_errors_total|subscription=old"])
	require.Equal(t, 5.0, metrics["pg_subscription_sync_errors_total|subscription=new"])
	require.Equal(t, 5.0, metrics["pg_subscription_conflicts_total|"+pgtype.CanonicalLabels(map[string]string{"subscription": "new", "conflict": "insert_exists"})])
	require.Equal(t, 4096.0, metrics["pg_logical_slot_spill_bytes_total|slot_name=old"])
	require.Equal(t, 8192.0, metrics["pg_logical_slot_bytes_total|slot_name=old"])
	require.Equal(t, 2.0, metrics["pg_subscriptions|"])
	require.Equal(t, 1.0, metrics["pg_subscriptions_enabled|"])
	require.Equal(t, 1.0, metrics["pg_subscriptions_running|"])
}

func TestLogicalReplication_NoSubscriptionsIsNotAnError(t *testing.T) {
	r := scrapeLogicalReplicationForTest(t, nil, nil)
	require.Empty(t, r.Facts)
	require.Contains(t, r.Metrics, pgtype.Metric{Name: "pg_subscriptions", Value: 0, Kind: pgtype.KindGauge})
}
