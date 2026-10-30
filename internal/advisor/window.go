package advisor

import (
	"context"
	"fmt"
	"time"
)

// Observation windows (13.2, D22b, D22c). Two rules need a stretch of history
// before their verdict means anything: index.unused (no scan of an index for a
// window) and the backup rules (no base backup seen for a window). Both windows
// are set from the console; the server stores them and hands them to the
// engine through a WindowsSource, so this package never reads their table.

// Windows are the observation windows the rules judge with.
type Windows struct {
	// UnusedIndex is how long an index must go unscanned to be a candidate
	// for index.unused.
	UnusedIndex time.Duration
	// BackupHistory is how long an instance must have been monitored before
	// "no base backup seen" means something.
	BackupHistory time.Duration
}

// DefaultWindows are the windows without a console setting: seven days each.
func DefaultWindows() Windows {
	return Windows{UnusedIndex: unusedWindow, BackupHistory: backupHistory}
}

// orDefault replaces an unset window with its default, so a snapshot built
// without windows (a test, or a pass before the source answered) judges as the
// rules always did.
func (w Windows) orDefault() Windows {
	def := DefaultWindows()
	if w.UnusedIndex <= 0 {
		w.UnusedIndex = def.UnusedIndex
	}
	if w.BackupHistory <= 0 {
		w.BackupHistory = def.BackupHistory
	}
	return w
}

// WindowsSource reports the windows set from the console. It never fails: a
// source that cannot read its store answers the windows it last read, or the
// defaults. A nil source means DefaultWindows.
type WindowsSource func(ctx context.Context) Windows

// windowsCacheTTL is how long the engine keeps the windows it read: a change
// from the console reaches the rules within this time, without a read per
// evaluation.
const windowsCacheTTL = 30 * time.Second

// Observation sources of the effective observation window.
const (
	ObservedByPglens        = "pglens"
	ObservedByPostgresStats = "postgres_stats"
)

// Observation is how long something has been observed, and by what.
type Observation struct {
	Duration time.Duration
	// Source is ObservedByPglens when pglens's own history is the longer
	// one, ObservedByPostgresStats when PostgreSQL's statistics, kept since
	// their last reset, cover more.
	Source string
}

// EffectiveObservation is the effective observation window (D22b): the longer
// of pglens's own history (since pglensFirstSeen) and the age of PostgreSQL's
// statistics (since statsReset). A nil statsReset is an unknown reset and
// counts for nothing; so does a zero pglensFirstSeen. A tie goes to pglens.
func EffectiveObservation(pglensFirstSeen time.Time, statsReset *time.Time, now time.Time) Observation {
	out := Observation{Source: ObservedByPglens}
	if !pglensFirstSeen.IsZero() && now.After(pglensFirstSeen) {
		out.Duration = now.Sub(pglensFirstSeen)
	}
	if statsReset != nil && now.Sub(*statsReset) > out.Duration {
		out = Observation{Duration: now.Sub(*statsReset), Source: ObservedByPostgresStats}
	}
	return out
}

// windowText names a window in a finding's text: "7 days", "1 hour",
// "36 hours", "90 minutes", or seconds when nothing coarser divides it.
func windowText(d time.Duration) string {
	unit := func(n int64, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	switch {
	case d > 0 && d%(24*time.Hour) == 0:
		return unit(int64(d/(24*time.Hour)), "day", "days")
	case d > 0 && d%time.Hour == 0:
		return unit(int64(d/time.Hour), "hour", "hours")
	case d > 0 && d%time.Minute == 0:
		return unit(int64(d/time.Minute), "minute", "minutes")
	default:
		return unit(int64(d/time.Second), "second", "seconds")
	}
}
