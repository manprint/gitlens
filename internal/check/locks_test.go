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

// lockValues is one row of the locks query, in its column order. A session
// that is blocked waits since one second after its start and has waited for
// age seconds, in ShareLock mode on a lock whose type is its wait event, and a
// relation lock is on public.accounts; a session that is not blocked waits on
// no lock.
func lockValues(pid int32, wait string, blocked []int32, age float64, query string) []any {
	var waitStart, waitDuration, mode, lockType, relation any
	if len(blocked) > 0 {
		waitStart = lockBackendStart(pid).Add(time.Second)
		waitDuration = age
		mode, lockType = "ShareLock", wait
		if wait == "relation" {
			relation = "public.accounts"
		}
	}
	return []any{pid, "app", "app", "client", "127.0.0.1", "active", "Lock", wait, 1.0, age, query, blocked, 0.0, nil, nil, nil, nil, nil, lockBackendStart(pid), waitStart, waitDuration, mode, lockType, relation, false}
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
	// backend_start is the seventh column from the end: the wait start, the
	// wait duration, the lock mode, type and relation, and the cut flag follow it.
	unseen[len(unseen)-7] = nil
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
		rows = append(rows, []any{int32(i), "app", "app", "client", "127.0.0.1", "idle", "", "", float64(i), 0.0, "select 1", []int32(nil), 0.0, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false})
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

// A statement the locks check reads that carries a password is withheld in the
// blocking tree, and no text of any session reaches the payload with a
// password in it.
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
}

// The session list carries no query text, for a session in the tree or out of
// it, and the JSON of a session has no query key at all. A node keeps its
// text, shortened to the cap only when it is longer than the cap.
func TestLocks_QueryTextOnlyInNodes(t *testing.T) {
	long := strings.Repeat("x", 3000)
	result := scrapeLocks(t, []([]any){
		lockValues(1, "", nil, 0, long),
		lockValues(2, "transactionid", []int32{9}, 2, long),
		lockValues(9, "", nil, 0, "holder"),
	})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Sessions, 3)
	for _, s := range tree.Sessions {
		require.Empty(t, s.Query, "session %d", s.PID)
		require.NotEmpty(t, s.State, "session %d keeps its other fields", s.PID)
	}
	var raw struct {
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &raw))
	for _, s := range raw.Sessions {
		require.NotContains(t, s, "query")
	}
	require.Len(t, tree.Nodes, 2)
	require.Equal(t, 2048, len(tree.Nodes[0].Query))
}

// The session identity is the start time in microseconds since the epoch, in
// lower-case hex, a dot and the PID, so a PID reused by a later session gets
// another identity. A session whose start is not visible has none.
func TestLocks_SessionIDFormat(t *testing.T) {
	unseen := lockValues(3, "", nil, 0, "select 3")
	unseen[len(unseen)-7] = nil
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, "update"), lockValues(9, "", nil, 0, "holder"), unseen})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	ids := map[int32]string{}
	for _, s := range tree.Sessions {
		ids[s.PID] = s.SessionID
	}
	// 2026-10-23T09:00:00.000001Z is 1792746000000001 microseconds since the epoch.
	require.Equal(t, "65e7e3480a401.1", ids[1])
	require.Equal(t, "", ids[3])
	require.NotEqual(t, ids[1], lockSessionID(1, ptrTime(lockBackendStart(1).Add(time.Hour))), "a reused PID has another identity")
	require.Equal(t, ids[1], tree.Nodes[0].SessionID, "a node carries the identity of its session")
}

// A blocked session carries when its wait began and how long it has lasted,
// both from the server. A session that waits on no lock carries neither.
func TestLocks_WaitFieldsOnWaitingSessions(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, "update"), lockValues(9, "", nil, 0, "holder")})
	raw := string(result.Facts[0].ValueJSON)
	require.Contains(t, raw, `"wait_started_at":"2026-10-23T09:00:01.000001Z"`)
	require.Contains(t, raw, `"wait_duration":2`)
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Nodes, 2)
	waiter, holder := tree.Nodes[0], tree.Nodes[1]
	require.Equal(t, int32(1), waiter.PID)
	require.NotNil(t, waiter.WaitStartedAt)
	require.True(t, lockBackendStart(1).Add(time.Second).Equal(*waiter.WaitStartedAt))
	require.NotNil(t, waiter.WaitDuration)
	require.Equal(t, 2.0, *waiter.WaitDuration)
	require.Nil(t, holder.WaitStartedAt)
	require.Nil(t, holder.WaitDuration)
	require.NotContains(t, rawSession(t, result, 9), "wait_duration")
	require.NotNil(t, tree.Sessions[0].WaitStartedAt, "the session list keeps the wait fields")
}

// DL-LOCK-001: a waiting session carries the mode and type of the lock it waits
// on, in the tree and in the session list; a session that waits on none has
// neither key. The tree used to carry no mode at all, so the console showed
// "—" for both sessions of a deadlock.
func TestDLLOCK001_WaitingSessionCarriesLockModeAndType(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "transactionid", []int32{9}, 2, "update"), lockValues(9, "", nil, 0, "holder")})
	require.Contains(t, rawSession(t, result, 1), `"lock_mode":"ShareLock"`)
	require.Contains(t, rawSession(t, result, 1), `"lock_type":"transactionid"`)
	require.NotContains(t, rawSession(t, result, 1), "lock_relation", "a transaction ID lock names no relation")
	for _, key := range []string{"lock_mode", "lock_type", "lock_relation"} {
		require.NotContains(t, rawSession(t, result, 9), key)
	}
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	waiter := tree.Nodes[0]
	require.NotNil(t, waiter.LockMode)
	require.Equal(t, "ShareLock", *waiter.LockMode)
	require.NotNil(t, waiter.LockType)
	require.Equal(t, "transactionid", *waiter.LockType)
	require.Nil(t, waiter.LockRelation)
	require.Nil(t, tree.Nodes[1].LockMode)
	var listed *lockNode
	for i := range tree.Sessions {
		if tree.Sessions[i].PID == 1 {
			listed = &tree.Sessions[i]
		}
	}
	require.NotNil(t, listed)
	require.NotNil(t, listed.LockMode, "the session list keeps the lock fields")
}

// DL-LOCK-002: a relation lock names its relation as the query printed it.
func TestDLLOCK002_RelationLockNamesItsRelation(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "relation", []int32{9}, 2, "select"), lockValues(9, "", nil, 0, "holder")})
	raw := rawSession(t, result, 1)
	require.Contains(t, raw, `"lock_type":"relation"`)
	require.Contains(t, raw, `"lock_relation":"public.accounts"`)
}

// DL-LOCK-003: the query names a relation only in the agent's own database or
// on a shared catalog. The OID of a relation in another database would be read
// against the agent's pg_class, where it names nothing or another table. The
// name is schema-qualified from the catalogs, not printed by regclass, whose
// text depends on the agent's search_path.
func TestDLLOCK003_QueryNamesOnlyRelationsOfItsOwnDatabase(t *testing.T) {
	require.Contains(t, locksQuery, "min(l.mode) AS mode")
	require.Contains(t, locksQuery, "min(l.locktype) AS locktype")
	require.Contains(t, locksQuery, "AND l.database IN (0, (SELECT oid FROM pg_database WHERE datname = current_database()))")
	require.Contains(t, locksQuery, "THEN COALESCE(quote_ident(n.nspname) || '.' || quote_ident(c.relname), l.relation::text) END) AS relation")
	require.Contains(t, locksQuery, "WHERE NOT l.granted GROUP BY l.pid) w ON w.pid = a.pid")
	require.NotContains(t, locksQuery, "regclass", "a regclass name depends on the agent's search_path")
}

func rawSession(t *testing.T, result Result, pid int32) string {
	t.Helper()
	var raw struct {
		Nodes []map[string]any `json:"nodes"`
	}
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &raw))
	for _, n := range raw.Nodes {
		if n["pid"] == float64(pid) {
			b, err := json.Marshal(n)
			require.NoError(t, err)
			return string(b)
		}
	}
	t.Fatalf("no node %d", pid)
	return ""
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

func scrapeLocksWith(t *testing.T, c *locksCheck, rows []([]any)) Result {
	t.Helper()
	mock := &mockConn{queryFunc: func(context.Context, string, ...any) (pgx.Rows, error) { return &mockRows{rows: rows}, nil }}
	target := &SimpleTarget{ConnFunc: func(context.Context) (Conn, error) { return mock, nil }, ClockValue: clock.System()}
	result, err := c.Scrape(context.Background(), target)
	require.NoError(t, err)
	return result
}

// sessionValues is lockValues with the session-detail columns set: query age,
// the xid and xmin ages, query id, leader pid and query start.
func sessionValues(pid int32, query string, queryAge float64, xidAge, xminAge, queryID any, leader any, queryStart any) []any {
	v := lockValues(pid, "", nil, 0, query)
	v[12], v[13], v[14], v[15], v[16], v[17] = queryAge, xidAge, xminAge, queryID, leader, queryStart
	return v
}

func rawSessions(t *testing.T, result Result) []map[string]any {
	t.Helper()
	var raw struct {
		Sessions []map[string]any `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &raw))
	return raw.Sessions
}

// SESS-001
func TestSESS001_SessionCarriesDetailFields(t *testing.T) {
	started := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	result := scrapeLocks(t, []([]any){sessionValues(7, "select 1", 5.5, int64(12), int64(34), int64(-42), int32(99), started)})
	var tree lockTree
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &tree))
	require.Len(t, tree.Sessions, 1)
	s := tree.Sessions[0]
	require.Equal(t, 5.5, s.QueryAge)
	require.Equal(t, int64(12), *s.BackendXidAge)
	require.Equal(t, int64(34), *s.BackendXminAge)
	require.Equal(t, "-42", s.QueryID, "the string of the bigint, sign included")
	require.Equal(t, int32(99), *s.LeaderPID)
	require.True(t, started.Equal(*s.QueryStart))
	// A session with no xid, no xmin and no leader omits them.
	raw := rawSessions(t, scrapeLocks(t, []([]any){sessionValues(8, "select 1", 0, nil, nil, nil, nil, nil)}))[0]
	for _, k := range []string{"backend_xid_age", "backend_xmin_age", "leader_pid", "query_start", "query_id"} {
		require.NotContains(t, raw, k)
	}
	require.Contains(t, raw, "query_age")
}

// SESS-002
func TestSESS002_QueryIDOmittedWhenNullOrZero(t *testing.T) {
	for name, id := range map[string]any{"null": nil, "zero": int64(0)} {
		t.Run(name, func(t *testing.T) {
			result := scrapeLocks(t, []([]any){sessionValues(7, "select 1", 1, nil, nil, id, nil, nil)})
			require.NotContains(t, rawSessions(t, result)[0], "query_id")
		})
	}
}

// SESS-003: the raw text of the session list is opt-in and always passes
// through the credential handling.
func TestSESS003_RawSessionTextIsOptInAndRedacted(t *testing.T) {
	cut := sessionValues(3, "DO $$ DECLARE p text := 'hunter3'; BEGIN /*", 0, nil, nil, nil, nil, nil)
	cut[len(cut)-1] = true
	rows := func() []([]any) {
		return []([]any){
			sessionValues(1, "select name from accounts", 0, nil, nil, nil, nil, nil),
			sessionValues(2, "ALTER ROLE tmp PASSWORD 'hunter2'", 0, nil, nil, nil, nil, nil),
			cut,
		}
	}
	for _, s := range rawSessions(t, scrapeLocksWith(t, &locksCheck{}, rows())) {
		require.NotContains(t, s, "query", "off by default")
	}

	on := &locksCheck{}
	on.SetSessionQueryText(true)
	require.True(t, on.SessionQueryTextEnabled())
	result := scrapeLocksWith(t, on, rows())
	require.NotContains(t, string(result.Facts[0].ValueJSON), "hunter")
	byPID := map[float64]string{}
	for _, s := range rawSessions(t, result) {
		byPID[s["pid"].(float64)], _ = s["query"].(string)
	}
	require.Equal(t, "select name from accounts", byPID[1])
	require.Equal(t, withheldQuery, byPID[2], "a credential-looking text is withheld")
	require.Equal(t, withheldQuery, byPID[3], "a text the server cut is withheld")
}

// SESS-004: the fields the session shape had keep their names; the new ones
// are additive.
func TestSESS004_ExistingSessionFieldsUnchanged(t *testing.T) {
	result := scrapeLocks(t, []([]any){lockValues(1, "relation", []int32{9}, 2, "update"), lockValues(9, "", nil, 0, "holder")})
	var raw struct {
		Sessions []map[string]json.RawMessage `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(result.Facts[0].ValueJSON, &raw))
	var blocked map[string]json.RawMessage
	for _, s := range raw.Sessions {
		if string(s["pid"]) == "1" {
			blocked = s
		}
	}
	got := map[string]string{}
	for k, v := range blocked {
		got[k] = string(v)
	}
	require.Equal(t, map[string]string{
		"pid": "1", "datname": `"app"`, "usename": `"app"`, "application_name": `"client"`, "client_addr": `"127.0.0.1"`,
		"state": `"active"`, "wait_event_type": `"Lock"`, "wait_event": `"relation"`, "xact_age": "1", "state_age": "2",
		"blocked_by":    "[9]",
		"backend_start": `"2026-10-23T09:00:00.000001Z"`, "session_id": `"` + lockSessionID(1, ptrTime(lockBackendStart(1))) + `"`,
		"wait_started_at": `"2026-10-23T09:00:01.000001Z"`, "wait_duration": "2",
		"lock_mode": `"ShareLock"`, "lock_type": `"relation"`, "lock_relation": `"public.accounts"`,
		"query_age": "0",
	}, got)
}

func ptrTime(t time.Time) *time.Time { return &t }
