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
	return []any{pid, "app", "app", "client", "127.0.0.1", "active", "Lock", wait, 1.0, age, query, blocked, lockBackendStart(pid), false}
}

func lockBackendStart(pid int32) time.Time {
	return time.Date(2026, 10, 23, 9, 0, 0, int(pid)*1000, time.UTC)
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

// TestLocks_TreeCarriesBackendStart: every node and session carries the start
// time of its session at microsecond precision, which a cancel or terminate
// sends back to tell the session from a later one with the same PID. A
// session whose start time the role may not see has none.
func TestLocks_TreeCarriesBackendStart(t *testing.T) {
	unseen := lockValues(3, "", nil, 0, "select 3")
	unseen[len(unseen)-2] = nil
	result := scrapeLocks(t, []([]any){lockValues(9, "", nil, 0, "holder"), lockValues(1, "transactionid", []int32{9}, 2, "update"), unseen})
	raw := string(result.Facts[0].ValueJSON)
	require.Contains(t, raw, `"backend_start":"2026-10-23T09:00:00.000001Z"`)
	require.Contains(t, raw, `"backend_start":"2026-10-23T09:00:00.000009Z"`)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Nodes, 2)
	for _, n := range tree.Nodes {
		require.NotNil(t, n.BackendStart, "node %d", n.PID)
		require.True(t, lockBackendStart(n.PID).Equal(*n.BackendStart), "node %d", n.PID)
	}
	for _, s := range tree.Sessions {
		if s.PID == 3 {
			require.Nil(t, s.BackendStart)
			continue
		}
		require.NotNil(t, s.BackendStart, "session %d", s.PID)
	}
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

// Sessions is the list the activity view shows: capped, oldest transaction
// first, and the cut is flagged so a reader knows the list is not complete.
func TestLocks_SessionListIsCappedOldestFirst(t *testing.T) {
	rows := make([][]any, 0, 105)
	for i := 1; i <= 105; i++ {
		rows = append(rows, []any{int32(i), "app", "app", "client", "127.0.0.1", "idle", "", "", float64(i), 0.0, "select 1", []int32(nil), nil, false})
	}
	result := scrapeLocks(t, rows)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Sessions, 100)
	require.True(t, tree.SessionsTruncated)
	require.Equal(t, int32(105), tree.Sessions[0].PID)
	require.Empty(t, tree.Nodes)
	// The cut list is a partial view, so the result says so to the server.
	require.True(t, result.Partial)
}

func TestLocks_SessionListNotFlaggedWhenComplete(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "", nil, 0, "select 1"), lockValues(2, "", nil, 0, "select 2")})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Sessions, 2)
	require.False(t, tree.SessionsTruncated)
	require.False(t, result.Partial)
}

// A session outside the blocking tree keeps only a short prefix of its query;
// a session in the tree keeps the full text, since the tree is what the UI
// shows for a wait.
// A statement the locks check reads that carries a password is withheld in the
// blocking tree and in the session list alike, and the text the UI shows never
// holds it.
func TestLocks_RedactsCredentialsInEveryQuery(t *testing.T) {
	result := scrapeLocks(t, []([]any){
		lockValues(1, "transactionid", []int32{9}, 2, "ALTER ROLE tmp_secret PASSWORD 'hunter2'"),
		lockValues(9, "", nil, 0, "CREATE ROLE holder LOGIN PASSWORD 'hunter3'"),
		lockValues(2, "", nil, 0, "ALTER ROLE idle PASSWORD 'hunter4'"),
	})
	require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter")
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Equal(t, withheldQuery, tree.Nodes[0].Query)
	require.Equal(t, withheldQuery, tree.Nodes[1].Query)
	queries := map[int32]string{}
	for _, s := range tree.Sessions {
		queries[s.PID] = s.Query
	}
	require.Equal(t, withheldQuery, queries[2])
}

// A long session text full of PASSWORD keywords inside one comment is read in
// bounded time: the redactor reads the text a fixed number of times, so the
// cost is linear.
func TestLocks_LongSessionTextIsRedactedInBoundedTime(t *testing.T) {
	long := "ALTER ROLE u PASSWORD /*" + strings.Repeat("password/*", 60000) + strings.Repeat("*/", 60001)
	start := time.Now()
	result := scrapeLocks(t, []([]any){
		lockValues(1, "transactionid", []int32{9}, 2, long),
		lockValues(9, "", nil, 0, "holder"),
	})
	require.Less(t, time.Since(start), 2*time.Second)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Equal(t, withheldQuery, tree.Nodes[0].Query)
}

// A credential named early in a long DO body is withheld even though the
// keyword that makes it a credential lies past the first 8192 bytes. Cutting
// the text before redaction would drop that keyword and leave the value.
func TestLocks_CredentialNamedEarlierInALongBodyIsRedacted(t *testing.T) {
	long := "DO $$ DECLARE p text := 'hunter2'; BEGIN /*" + strings.Repeat("x", 9000) + "*/ EXECUTE 'ALTER ROLE u PASSWORD ' || quote_literal(p); END $$"
	result := scrapeLocks(t, []([]any){
		lockValues(1, "transactionid", []int32{9}, 2, long),
		lockValues(9, "", nil, 0, "holder"),
	})
	require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter")
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Equal(t, withheldQuery, tree.Nodes[0].Query)
}

// A session text the server cut is withheld whole in the blocking tree and in
// the session list. It is not redacted, because the keyword that makes its
// value a credential may lie past the cut.
func TestLocks_TextTheServerCutIsWithheld(t *testing.T) {
	cutLocked := lockValues(1, "transactionid", []int32{9}, 2, "DO $$ DECLARE p text := 'hunter2'; BEGIN /*")
	cutLocked[len(cutLocked)-1] = true
	cutIdle := lockValues(2, "", nil, 0, "DO $$ DECLARE p text := 'hunter2'; BEGIN /*")
	cutIdle[len(cutIdle)-1] = true
	result := scrapeLocks(t, []([]any){cutLocked, cutIdle, lockValues(9, "", nil, 0, "holder")})
	require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter")
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Equal(t, withheldQuery, tree.Nodes[0].Query)
	queries := map[int32]string{}
	for _, s := range tree.Sessions {
		queries[s.PID] = s.Query
	}
	require.Equal(t, withheldQuery, queries[2])
}

func TestLocks_IdleSessionQueryIsShortened(t *testing.T) {
	long := strings.Repeat("x", 3000)
	result := scrapeLocks(t, []([]any){
		lockValues(1, "", nil, 0, long),
		lockValues(2, "transactionid", []int32{9}, 2, long),
		lockValues(9, "", nil, 0, "holder"),
	})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	queryLen := map[int32]int{}
	for _, s := range tree.Sessions {
		queryLen[s.PID] = len(s.Query)
	}
	require.Equal(t, lockIdleQueryLimit, queryLen[1])
	require.Equal(t, 2048, queryLen[2])
	require.Len(t, tree.Nodes, 2)
	require.Equal(t, 2048, len(tree.Nodes[0].Query))
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
