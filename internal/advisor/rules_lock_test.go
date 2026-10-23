package advisor

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/require"
)

func lockSample(now time.Time, nodes ...LockTreeNode) *Snapshot {
	return &Snapshot{Now: now, PermTier: pgtype.TierReadOnly, LockTree: &LockTreeSample{SampledAt: now, Nodes: nodes}}
}

// A waiter is reported with the root of its blocking chain, its wait and no
// query text: the evidence carries the three numbers and nothing else.
func TestLockLongWaitRule_ReportsTheLongestWait(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "lock.long_wait")
	s := lockSample(now,
		LockTreeNode{PID: 10, BlockedBy: []int32{20}, WaitDuration: 320},
		LockTreeNode{PID: 20, BlockedBy: []int32{30}},
		LockTreeNode{PID: 30},
		LockTreeNode{PID: 11, BlockedBy: []int32{30}, WaitDuration: 400.5},
	)
	s.LockTree.Nodes[3].WaitDuration = 90
	assertRuleContract(t, rule, s)
	got := rule.Evaluate(s)
	require.Len(t, got, 1)
	require.Equal(t, ScopeInstance, rule.Scope())
	require.Equal(t, SeverityWarning, got[0].Severity)
	require.Equal(t, "Session 10 has waited 5 minutes 20 seconds for a lock", got[0].Title)
	require.Contains(t, got[0].Detail, "chain reaches session 30")
	require.Contains(t, got[0].Remediation, "session 30")
	require.Equal(t, map[string]any{"waiting_pid": int32(10), "root_blocker_pid": int32(30), "wait_seconds": 320.0}, got[0].Evidence)
}

func TestLockLongWaitRule_QuietBelowThreshold(t *testing.T) {
	now := time.Now()
	rule := ruleByID(t, "lock.long_wait")
	require.Empty(t, rule.Evaluate(lockSample(now, LockTreeNode{PID: 10, BlockedBy: []int32{20}, WaitDuration: 299.9}, LockTreeNode{PID: 20})))
	require.Empty(t, rule.Evaluate(lockSample(now, LockTreeNode{PID: 20, WaitDuration: 900})), "a session that waits on no lock is not a waiter")
}

// A sample older than the freshness window says nothing about the waits now.
func TestLockLongWaitRule_IgnoresAStaleSample(t *testing.T) {
	now := time.Now()
	s := lockSample(now, LockTreeNode{PID: 10, BlockedBy: []int32{20}, WaitDuration: 600}, LockTreeNode{PID: 20})
	s.LockTree.SampledAt = now.Add(-lockSampleFresh - time.Second)
	require.Empty(t, ruleByID(t, "lock.long_wait").Evaluate(s))
	s.LockTree.SampledAt = now.Add(-lockSampleFresh + time.Second)
	require.Len(t, ruleByID(t, "lock.long_wait").Evaluate(s), 1, "a sample inside the window is read")
}

// A waiter behind a diamond is reported with the root, not with a blocker that
// is itself waiting, and a session already visited is not walked twice.
func TestLockRootBlocker_FollowsTheNearestRoot(t *testing.T) {
	sample := &LockTreeSample{Nodes: []LockTreeNode{
		{PID: 10, BlockedBy: []int32{21, 20}},
		{PID: 20, BlockedBy: []int32{30}},
		{PID: 21, BlockedBy: []int32{20}},
		{PID: 30},
	}}
	require.Equal(t, int32(30), lockRootBlocker(sample, 10))
	// A blocker the sample does not hold ends the walk: it is the root.
	require.Equal(t, int32(99), lockRootBlocker(&LockTreeSample{Nodes: []LockTreeNode{{PID: 10, BlockedBy: []int32{99}}}}, 10))
	// A cycle has no root; the lowest blocker of the waiter is named.
	require.Equal(t, int32(20), lockRootBlocker(&LockTreeSample{Nodes: []LockTreeNode{
		{PID: 10, BlockedBy: []int32{20}},
		{PID: 20, BlockedBy: []int32{10}},
	}}, 10))
}

// A lock tree the loader cannot decode degrades the rule instead of reading as
// an empty sample, and a session list entry never reaches the rule.
func TestLoadLockTree_UnreadableTreeDegradesTheRule(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	id := uuid.New()
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("FROM lock_snapshots")).WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"ts", "tree"}).AddRow(now, []byte(`{"nodes":"not a list"}`)))
	s := &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, SkippedChecks: map[string]string{}}
	require.NoError(t, loadLockTree(context.Background(), mock, s))
	require.Nil(t, s.LockTree)
	require.Contains(t, s.SkippedChecks["LockTree"], "lock tree unreadable")
	got, panicked := (&Engine{}).evaluateRule(ruleByID(t, "lock.long_wait"), s)
	require.False(t, panicked)
	require.Len(t, got, 1)
	require.Equal(t, StateDegraded, got[0].State)
	require.Equal(t, "lock.long_wait", got[0].RuleID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// The loader keeps the wait fields and drops everything else it does not read,
// the query text included.
func TestLoadLockTree_KeepsOnlyTheWaitFields(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	id := uuid.New()
	now := time.Now()
	tree := `{"sampled_at":"2026-10-23T09:00:00Z","nodes":[{"pid":10,"query":"SELECT 1","blocked_by":[20],"wait_duration":301.5}],"sessions":[]}`
	mock.ExpectQuery(regexp.QuoteMeta("FROM lock_snapshots")).WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"ts", "tree"}).AddRow(now, []byte(tree)))
	s := &Snapshot{Now: now, InstanceID: id, SkippedChecks: map[string]string{}}
	require.NoError(t, loadLockTree(context.Background(), mock, s))
	require.Equal(t, []LockTreeNode{{PID: 10, BlockedBy: []int32{20}, WaitDuration: 301.5}}, s.LockTree.Nodes)
	require.Equal(t, now, s.LockTree.SampledAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

// No stored tree is an empty sample, not a degraded input: a locks check that
// has not run yet says nothing about waits.
func TestLoadLockTree_NoRowIsAnEmptySample(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	id := uuid.New()
	mock.ExpectQuery(regexp.QuoteMeta("FROM lock_snapshots")).WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"ts", "tree"}))
	s := &Snapshot{InstanceID: id, SkippedChecks: map[string]string{}}
	require.NoError(t, loadLockTree(context.Background(), mock, s))
	require.NotNil(t, s.LockTree)
	require.True(t, s.LockTree.SampledAt.IsZero())
	require.Empty(t, ruleByID(t, "lock.long_wait").Evaluate(s))
	require.NoError(t, mock.ExpectationsWereMet())
}
