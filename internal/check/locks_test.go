package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

func scrapeLocks(t *testing.T, rows []([]any)) Result {
	t.Helper()
	mock := &mockConn{queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) { return &mockRows{rows: rows}, nil }}
	target := &SimpleTarget{ConnFunc: func(context.Context) (Conn, error) { return mock, nil }, ClockValue: clock.System()}
	result, err := (&locksCheck{}).Scrape(context.Background(), target)
	require.NoError(t, err)
	return result
}

func lockValues(pid int32, wait string, blocked []int32, age float64, query string) []any {
	return []any{pid, "app", "app", "client", "127.0.0.1", "active", "Lock", wait, 1.0, age, query, blocked}
}

func TestLocks_NoBlockedSessionsEmitsEmptyFact(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "", nil, 0, "select 1")})
	require.Len(t, result.Facts, 1)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Empty(t, tree.Nodes)
	require.Equal(t, 0.0, metricValue(result, "pg_blocked_sessions", nil))
}

func TestLocks_CountsBlockedAndBlocking(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, "update"), lockValues(2, "relation", []int32{9, 10}, 3, "select")})
	require.Equal(t, 2.0, metricValue(result, "pg_blocked_sessions", nil))
	require.Equal(t, 2.0, metricValue(result, "pg_blocking_sessions", nil))
}

func TestLocks_MaxBlockAgeIsTheGreatest(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "a", []int32{9}, 2, "a"), lockValues(2, "b", []int32{10}, 7, "b")})
	require.Equal(t, 7.0, metricValue(result, "pg_max_block_age_seconds", nil))
}

func TestLocks_WaitEventLabelIsBounded(t *testing.T) {
	rows := make([][]any, 30)
	for i := range rows {
		rows[i] = lockValues(int32(i+1), string(rune('a'+i)), []int32{100}, 1, "q")
	}
	result := scrapeLocks(t, rows)
	count, other := 0, false
	for _, metric := range result.Metrics {
		if metric.Name == "pg_lock_waits" {
			count++
			if metric.Labels["wait_event"] == "other" {
				other = true
			}
		}
	}
	require.Equal(t, 11, count)
	require.True(t, other)
}

func TestLocks_TreeJSONShape(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, "update")})
	require.Len(t, result.Facts, 1)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Nodes, 1)
	require.Equal(t, int32(9), tree.Nodes[0].BlockedBy[0])
}

func TestLocks_TreeIncludesBlockingSession(t *testing.T) {
	result := scrapeLocks(t, []([]any){
		lockValues(9, "", nil, 0, "holder"),
		lockValues(1, "transactionid", []int32{9}, 2, "waiter"),
	})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Nodes, 2)
	require.Equal(t, int32(1), tree.Nodes[0].PID)
	require.Equal(t, int32(9), tree.Nodes[1].PID)
}

func TestLocks_QueryIsTruncated(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, strings.Repeat("x", 3000))})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Nodes[0].Query, 2048)
}

func TestLocks_RequiresTierT0(t *testing.T) {
	require.Equal(t, Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}, (&locksCheck{}).Requires())
	require.Equal(t, 10*time.Second, (&locksCheck{}).DefaultInterval())
	require.Equal(t, 10*time.Second, (&locksCheck{}).Timeout())
}

func metricValue(result Result, name string, labels map[string]string) float64 {
	for _, metric := range result.Metrics {
		if metric.Name == name && labelsEqual(metric.Labels, labels) {
			return metric.Value
		}
	}
	return -1
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range b {
		if a[key] != value {
			return false
		}
	}
	return true
}
