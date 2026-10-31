package advisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

func windowAgo(d time.Duration) *time.Time {
	t := unusedTestNow.Add(-d)
	return &t
}

// WINDOW-001: PostgreSQL's statistics reset ten days ago cover more than two
// hours of pglens history, so they are the effective observation.
func TestWINDOW001_StatsOlderThanHistoryWin(t *testing.T) {
	got := EffectiveObservation(unusedTestNow.Add(-2*time.Hour), windowAgo(10*24*time.Hour), unusedTestNow)
	require.Equal(t, Observation{Duration: 10 * 24 * time.Hour, Source: ObservedByPostgresStats}, got)
}

// WINDOW-002: a reset an hour ago covers less than three hours of pglens
// history, so pglens's own history is the effective observation.
func TestWINDOW002_HistoryLongerThanStats(t *testing.T) {
	got := EffectiveObservation(unusedTestNow.Add(-3*time.Hour), windowAgo(time.Hour), unusedTestNow)
	require.Equal(t, Observation{Duration: 3 * time.Hour, Source: ObservedByPglens}, got)
}

// WINDOW-003: an unknown reset counts for nothing: pglens's history only, and
// nothing at all without it.
func TestWINDOW003_UnknownResetIsHistoryOnly(t *testing.T) {
	got := EffectiveObservation(unusedTestNow.Add(-5*time.Hour), nil, unusedTestNow)
	require.Equal(t, Observation{Duration: 5 * time.Hour, Source: ObservedByPglens}, got)
	require.Equal(t, Observation{Source: ObservedByPglens}, EffectiveObservation(time.Time{}, nil, unusedTestNow))
}

// WINDOW-004: with the default windows, an index PostgreSQL has not seen
// scanned since a reset eight days ago is an open finding at once, whatever
// pglens's own history, and its evidence says the statistics decided.
func TestWINDOW004_StatsAgeJudgesAtOnce(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 0.1, ScanTracked: true, StatsReset: windowAgo(8 * 24 * time.Hour)}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Empty(t, f[0].DegradedReason)
	require.Equal(t, ObservedByPostgresStats, f[0].Evidence["observed_source"])
	require.Equal(t, int64(8*24*3600), f[0].Evidence["observed_for_seconds"])
	require.Equal(t, int64(7*24*3600), f[0].Evidence["window_seconds"])
}

// WINDOW-005: a reset three days ago covers less than the window, so the
// index keeps the history rule and is degraded as before, with the same reason.
func TestWINDOW005_YoungStatsStayDegraded(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 0.1, ScanTracked: true, StatsReset: windowAgo(3 * 24 * time.Hour)}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "index usage history is shorter than 7 days", f[0].DegradedReason)
}

// WINDOW-006: a one-hour window judges an index with two hours of history,
// and the engine hands the source's windows to the rules, reading the source
// again only once its cache has expired.
func TestWINDOW006_ShortWindowJudges(t *testing.T) {
	s := unusedSnapshot(IndexStat{HistoryDays: 2.0 / 24}, reportingSibling(false))
	s.Windows = Windows{UnusedIndex: time.Hour}
	f := evaluateUnused(t, s)
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Equal(t, ObservedByPglens, f[0].Evidence["observed_source"])
	require.Equal(t, int64(3600), f[0].Evidence["window_seconds"])

	// The same index under a three-hour window is not judged yet, and the
	// reason names the configured window.
	s.Windows = Windows{UnusedIndex: 3 * time.Hour}
	f = evaluateUnused(t, s)
	require.Len(t, f, 1)
	require.Equal(t, "index usage history is shorter than 3 hours", f[0].DegradedReason)

	var seen []Windows
	probe := engineTestRule{id: "probe", severity: SeverityInfo, scope: ScopeInstance, eval: func(s *Snapshot) []Finding {
		seen = append(seen, s.windows())
		return nil
	}}
	e, _, _ := runWithRules(t, probe)
	var reads atomic.Int32
	e.SetWindowsSource(func(context.Context) Windows {
		reads.Add(1)
		return Windows{UnusedIndex: time.Hour}
	})
	require.NoError(t, e.Run(context.Background()))
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, int32(1), reads.Load(), "the windows are cached between passes")
	e.clock.(*clock.Fake).Advance(windowsCacheTTL)
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, int32(2), reads.Load(), "the cache expires after its TTL")
	want := Windows{UnusedIndex: time.Hour, BackupHistory: 7 * 24 * time.Hour}
	require.Equal(t, []Windows{want, want, want}, seen, "an unset window is its default")

	e.SetWindowsSource(nil)
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, DefaultWindows(), seen[len(seen)-1])
}

// WINDOW-007: PostgreSQL 16 saw the index scanned two days ago, inside the
// seven-day window: the index is in use, with a short history or a long one.
func TestWINDOW007_RecentLastScanIsNotUnused(t *testing.T) {
	last := windowAgo(2 * 24 * time.Hour)
	for _, history := range []float64{1, 10} {
		s := unusedSnapshot(IndexStat{HistoryDays: history, ScanTracked: true, LastScan: last, StatsReset: windowAgo(30 * 24 * time.Hour)}, reportingSibling(false))
		require.Empty(t, evaluateUnused(t, s), "history %.0f days", history)
	}
}

// WINDOW-008: the backup rules compare the monitored history with the
// configured window, and their degraded text names it.
func TestWINDOW008_BackupHistoryWindow(t *testing.T) {
	strategy := ruleByID(t, "backup.no_strategy")
	seen := ruleByID(t, "backup.no_basebackup_seen")

	noStrategy := configBase()
	noStrategy.Settings["archive_mode"] = "off"
	delete(noStrategy.Facts, "backup")
	noStrategy.FirstSeen = configNow.Add(-2 * time.Hour)
	f := strategy.Evaluate(noStrategy)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "the instance has been monitored for less than 7 days and no base backup has been seen yet", f[0].Detail)
	noStrategy.Windows = Windows{BackupHistory: time.Hour}
	f = strategy.Evaluate(noStrategy)
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	noStrategy.Windows = Windows{BackupHistory: 3 * time.Hour}
	f = strategy.Evaluate(noStrategy)
	require.Len(t, f, 1)
	require.Equal(t, "the instance has been monitored for less than 3 hours and no base backup has been seen yet", f[0].Detail)

	noBackup := configBase()
	delete(noBackup.Facts, "backup")
	noBackup.FirstSeen = configNow.Add(-2 * time.Hour)
	f = seen.Evaluate(noBackup)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	noBackup.Windows = Windows{BackupHistory: time.Hour}
	f = seen.Evaluate(noBackup)
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Contains(t, f[0].Detail, "in 0 days of monitoring")
}

// WINDOW-011: before PostgreSQL 16 (the scan is untracked) two hours of
// pglens history keep the finding degraded, whatever the statistics' age.
func TestWINDOW011_PG15HasNoStatsShortcut(t *testing.T) {
	f := evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 2.0 / 24, StatsReset: windowAgo(30 * 24 * time.Hour)}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "index usage history is shorter than 7 days", f[0].DegradedReason)
	require.Equal(t, Observation{Duration: 2 * time.Hour, Source: ObservedByPglens}, indexObservation(unusedTestNow, IndexStat{HistoryDays: 2.0 / 24, StatsReset: windowAgo(30 * 24 * time.Hour)}))
}

// WINDOW-012: a one-hour backup window does not make a three-day-old base
// backup stale: the freshness of a backup keeps its fixed seven days.
func TestWINDOW012_ShortWindowKeepsBackupFreshness(t *testing.T) {
	s := configBase()
	s.Facts["backup"]["basebackup_running"] = Fact{Key: "basebackup_running", ObservedAt: configNow.Add(-3 * 24 * time.Hour)}
	s.Windows = Windows{BackupHistory: time.Hour}
	require.Empty(t, ruleByID(t, "backup.no_basebackup_seen").Evaluate(s))
	s.Facts["backup"]["basebackup_running"] = Fact{Key: "basebackup_running", ObservedAt: configNow.Add(-8 * 24 * time.Hour)}
	require.Len(t, ruleByID(t, "backup.no_basebackup_seen").Evaluate(s), 1)
}

// windowText names a window in whole units.
func TestWINDOW006_WindowText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		7 * 24 * time.Hour: "7 days", 24 * time.Hour: "1 day", time.Hour: "1 hour", 36 * time.Hour: "36 hours",
		90 * time.Minute: "90 minutes", 3601 * time.Second: "3601 seconds",
	} {
		require.Equal(t, want, windowText(d))
	}
}

// WINDOW-013: PostgreSQL saw the index scanned 20 days ago, after a reset 30
// days ago: its statistics prove non-use for 20 days, not 30, which is still
// longer than pglens's 10 days of history.
func TestWINDOW013_LastScanAnchorsTheStatistics(t *testing.T) {
	i := IndexStat{HistoryDays: 10, ScanTracked: true, LastScan: windowAgo(20 * 24 * time.Hour), StatsReset: windowAgo(30 * 24 * time.Hour)}
	require.Equal(t, Observation{Duration: 20 * 24 * time.Hour, Source: ObservedByPostgresStats}, indexObservation(unusedTestNow, i))

	f := evaluateUnused(t, unusedSnapshot(i, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Equal(t, int64(20*24*3600), f[0].Evidence["observed_for_seconds"])

	// A scan made after pglens's history began leaves pglens the longer one.
	i.LastScan = windowAgo(5 * 24 * time.Hour)
	require.Equal(t, Observation{Duration: 10 * 24 * time.Hour, Source: ObservedByPglens}, indexObservation(unusedTestNow, i))
}

// WINDOW-014 (R-P13 #5, D22b): PostgreSQL 16 last scanned the index ten days
// ago and pglens, with an hour of history, saw no scan since: the statistics
// prove the seven-day window, so the index is an open candidate at once.
func TestWINDOW014_LastScanBeforeWindowJudgesAtOnce(t *testing.T) {
	i := IndexStat{HistoryDays: 1.0 / 24, ScanTracked: true, LastScan: windowAgo(10 * 24 * time.Hour), StatsReset: windowAgo(30 * 24 * time.Hour)}
	f := evaluateUnused(t, unusedSnapshot(i, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Empty(t, f[0].DegradedReason)
	require.Equal(t, ObservedByPostgresStats, f[0].Evidence["observed_source"])
	require.Equal(t, int64(10*24*3600), f[0].Evidence["observed_for_seconds"])
	require.Equal(t, int64(7*24*3600), f[0].Evidence["window_seconds"])
	require.Equal(t, "2026-10-13T12:00:00Z", f[0].Evidence["last_scan"])
	require.Equal(t, "no scan of this index since 2026-10-13, more than 7 days ago", f[0].Detail)
	require.NotContains(t, f[0].Evidence, "window", "not the never-scanned-since-reset path")

	// A scan pglens recorded inside the window wins over the older label.
	i.IdxScan = 0.2
	require.Empty(t, evaluateUnused(t, unusedSnapshot(i, reportingSibling(false))))

	// The same history on PostgreSQL 15 (no last_idx_scan) stays degraded.
	f = evaluateUnused(t, unusedSnapshot(IndexStat{HistoryDays: 1.0 / 24}, reportingSibling(false)))
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "index usage history is shorter than 7 days", f[0].DegradedReason)
}

// WINDOW-015 (R-P13 #7): under a 60-day backup window, a base backup seen 40
// days ago is no backup strategy, and the rule says so at once rather than
// claiming that no base backup has been seen yet.
func TestWINDOW015_OldBackupIsNotDegradedByALongWindow(t *testing.T) {
	strategy := ruleByID(t, "backup.no_strategy")
	s := configBase()
	s.Settings["archive_mode"] = "off"
	s.Facts["backup"] = map[string]Fact{"basebackup_running": {Key: "basebackup_running", ObservedAt: configNow.Add(-40 * 24 * time.Hour)}}
	s.FirstSeen = configNow.Add(-45 * 24 * time.Hour)
	s.Windows = Windows{BackupHistory: 60 * 24 * time.Hour}
	f := strategy.Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Equal(t, "No visible backup strategy", f[0].Title)

	// Without any base backup seen, the same window still degrades.
	delete(s.Facts, "backup")
	f = strategy.Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "the instance has been monitored for less than 60 days and no base backup has been seen yet", f[0].Detail)

	// R-P13b #3: monitored for 10 days, past a 7-day window, with no base
	// backup ever seen: the detail names the 10 days pglens watched, not 30.
	s.Windows = Windows{BackupHistory: 7 * 24 * time.Hour}
	s.FirstSeen = configNow.Add(-10 * 24 * time.Hour)
	f = strategy.Evaluate(s)
	require.Len(t, f, 1)
	require.Equal(t, StateOpen, f[0].State)
	require.Equal(t, "no member of the cluster archives WAL and no base backup has been seen streaming from the cluster in 10 days of monitoring", f[0].Detail)
	require.Equal(t, 10.0, f[0].Evidence["monitored_days"])
}

// WINDOW-016 (R-P13b #1): the engine hands the loader the windows its source
// answers, so the index section reads its scan rates over the configured
// window, not the default.
func TestWINDOW016_LoaderReceivesTheConfiguredWindows(t *testing.T) {
	e, _, id := runWithRules(t)
	var got []Windows
	e.load = func(_ context.Context, _ uuid.UUID, now time.Time, windows Windows) (*Snapshot, error) {
		got = append(got, windows)
		return &Snapshot{Now: now, InstanceID: id, PermTier: pgtype.TierReadOnly, Metrics: map[string]map[string]float64{}, Settings: map[string]string{}}, nil
	}
	e.SetWindowsSource(func(context.Context) Windows { return Windows{UnusedIndex: 5 * 24 * time.Hour} })
	require.NoError(t, e.Run(context.Background()))
	require.Equal(t, []Windows{{UnusedIndex: 5 * 24 * time.Hour, BackupHistory: 7 * 24 * time.Hour}}, got)
}

// WINDOW-017 (R-P13b #2): when the store keeps less index history than the
// window, pglens's own history never covers it, and the degraded reason says
// so; otherwise, or when the retention is unknown, the reason is unchanged.
func TestWINDOW017_ReasonNamesTheKeptIndexHistory(t *testing.T) {
	s := unusedSnapshot(IndexStat{HistoryDays: 20}, reportingSibling(false))
	s.Windows = Windows{UnusedIndex: 60 * 24 * time.Hour}
	s.IndexHistoryKept = 30 * 24 * time.Hour
	f := evaluateUnused(t, s)
	require.Len(t, f, 1)
	require.Equal(t, StateDegraded, f[0].State)
	require.Equal(t, "the unused-index window (60 days) is longer than the kept index metrics history (30 days)", f[0].DegradedReason)

	s.IndexHistoryKept = 0
	f = evaluateUnused(t, s)
	require.Equal(t, "index usage history is shorter than 60 days", f[0].DegradedReason)

	s = unusedSnapshot(IndexStat{HistoryDays: 2}, reportingSibling(false))
	s.IndexHistoryKept = 30 * 24 * time.Hour
	f = evaluateUnused(t, s)
	require.Equal(t, "index usage history is shorter than 7 days", f[0].DegradedReason)
}
