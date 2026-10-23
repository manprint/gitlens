package advisor

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
)

const unusedTestKey = "app\x00public.i"

var unusedTestNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// unusedSnapshot is a primary that reports one large index, scanned on PostgreSQL
// 16 and later or not. The siblings are whatever the test gives.
func unusedSnapshot(idx IndexStat, siblings ...SiblingInfo) *Snapshot {
	idx.Datname = "app"
	idx.Name = "public.i"
	if idx.SizeBytes == 0 {
		idx.SizeBytes = unusedIndexBytes + 1
	}
	return &Snapshot{Now: unusedTestNow, Role: pgtype.RolePrimary, Indexes: []IndexStat{idx}, Siblings: siblings}
}

func sinceReset(days float64) *time.Time {
	t := unusedTestNow.Add(-time.Duration(days*24) * time.Hour)
	return &t
}

// A standby that reported the index, with or without a scan.
func reportingSibling(scanned bool) SiblingInfo {
	sib := SiblingInfo{InstanceID: uuid.New(), IndexHashes: map[string]string{unusedTestKey: "h"}, IndexScanned: map[string]bool{}}
	if scanned {
		sib.IndexScanned[unusedTestKey] = true
	}
	return sib
}

func evaluateUnused(t *testing.T, s *Snapshot) []Finding {
	t.Helper()
	r := relationRuleByID(t, "index.unused")
	f := r.Evaluate(s)
	assertRuleContract(t, r, s)
	return f
}

// On PostgreSQL 16 an index never scanned since a reset a week old is a candidate
// at once: the instance's own history is not needed.
func TestRule_index_unused_NeverScannedSinceOldResetIsCandidateAtOnce(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 1, ScanTracked: true, StatsReset: sinceReset(30)}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Equal(t, "Unused index", f[0].Title)
	require.Contains(t, f[0].Detail, "since the statistics were reset on 2026-09-23")
	require.Equal(t, "since statistics reset", f[0].Evidence["window"])
}

// A reset whose time is unknown is treated as a window already passed.
func TestRule_index_unused_NeverScannedWithUnknownResetIsCandidateAtOnce(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 0, ScanTracked: true}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Contains(t, f[0].Detail, "at an unknown time")
}

// A reset younger than the window keeps the instance's own seven-day history rule.
func TestRule_index_unused_RecentResetKeepsHistoryRule(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 2, ScanTracked: true, StatsReset: sinceReset(2)}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "index usage history is shorter than 7 days", f[0].DegradedReason)
}

// An index that was scanned since the reset is not a candidate by the reset window:
// the existing history rule decides, so a scan a week back keeps it in use.
func TestRule_index_unused_ScannedSinceResetFollowsHistory(t *testing.T) {
	last := unusedTestNow.Add(-8 * 24 * time.Hour)
	recentlyScanned := unusedSnapshot(IndexStat{HistoryDays: 10, ScanTracked: true, LastScan: &last, StatsReset: sinceReset(30), IdxScan: 0.5}, reportingSibling(false))
	require.Empty(t, evaluateUnused(t, recentlyScanned))
	idle := unusedSnapshot(IndexStat{HistoryDays: 10, ScanTracked: true, LastScan: &last, StatsReset: sinceReset(30)}, reportingSibling(false))
	require.Len(t, evaluateUnused(t, idle), 1)
}

// Before PostgreSQL 16 the scan is untracked: the rule keeps the seven-day history.
func TestRule_index_unused_PG15KeepsSevenDayHistory(t *testing.T) {
	short := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 3}, reportingSibling(false)))
	require.Len(t, short, 1)
	require.Equal(t, StateDegraded, short[0].State)
	require.Equal(t, "index usage history is shorter than 7 days", short[0].DegradedReason)

	long := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8, StatsReset: sinceReset(30)}, reportingSibling(false)))
	require.Len(t, long, 1)
	require.Equal(t, StateOpen, long[0].State)
	require.Equal(t, "no scan of this index was recorded in 8.0 days", long[0].Detail)
	require.NotContains(t, long[0].Evidence, "window")
}

// On 15 a standby with no sibling snapshot degrades the finding and keeps it.
func TestRule_index_unused_NoSiblingSnapshotDegradesButKeepsFinding(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8}))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "standby scans unknown: no sibling snapshot", f[0].DegradedReason)
	require.Equal(t, "Unused index", f[0].Title)
	require.NotEmpty(t, f[0].Detail)
}

// A sibling that reports the index but has no last scan of it is a known zero.
func TestRule_index_unused_SiblingWithoutScanLeavesFindingOpen(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Empty(t, f[0].DegradedReason)
}

// A sibling that scanned the index, by its last scan label or by its metrics
// history, means a standby uses it: no finding.
func TestRule_index_unused_SiblingScanSuppressesFinding(t *testing.T) {
	require.Empty(t, evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8}, reportingSibling(true))))
	metricsOnly := SiblingInfo{InstanceID: uuid.New(), IndexScanned: map[string]bool{unusedTestKey: true}}
	require.Empty(t, evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8}, metricsOnly)))
	require.Empty(t, evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 8}, reportingSibling(false), reportingSibling(true))))
}

// Small, primary and unique indexes stay out, on either path.
func TestRule_index_unused_SkipsSmallPrimaryAndUniqueOnTheResetPath(t *testing.T) {
	for _, idx := range []IndexStat{
		{ScanTracked: true, StatsReset: sinceReset(30), SizeBytes: unusedIndexBytes},
		{ScanTracked: true, StatsReset: sinceReset(30), IsPrimary: true},
		{ScanTracked: true, StatsReset: sinceReset(30), IsUnique: true},
	} {
		require.Empty(t, evaluateUnused(t, unusedSnapshot(idx, reportingSibling(false))))
	}
}

// The label contract: an absent label is untracked, an empty one is never scanned,
// and a label that does not parse is untracked, not never-scanned.
func TestScanLabels(t *testing.T) {
	tracked, last, reset := scanLabels(map[string]string{"def_hash": "h"})
	require.False(t, tracked)
	require.Nil(t, last)
	require.Nil(t, reset)

	tracked, last, reset = scanLabels(map[string]string{"last_idx_scan": "", "stats_reset": ""})
	require.True(t, tracked)
	require.Nil(t, last)
	require.Nil(t, reset)

	tracked, last, reset = scanLabels(map[string]string{"last_idx_scan": "2026-10-20T06:30:00.123Z", "stats_reset": "2026-10-01T00:00:00Z"})
	require.True(t, tracked)
	require.Equal(t, time.Date(2026, 10, 20, 6, 30, 0, 123000000, time.UTC), *last)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), *reset)

	tracked, last, reset = scanLabels(map[string]string{"last_idx_scan": "yesterday", "stats_reset": ""})
	require.False(t, tracked)
	require.Nil(t, last)
	require.Nil(t, reset)
}
