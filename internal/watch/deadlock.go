package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"
)

// deadlockQuery reads the deadlocks PostgreSQL has counted in each database,
// as one JSON object. pg_stat_database takes no lock and scans no table.
const deadlockQuery = `SELECT COALESCE(json_object_agg(datname, deadlocks), '{}') FROM pg_stat_database WHERE datname IS NOT NULL`

// maxRecentPerDatabase caps the sessions remembered for one database.
const maxRecentPerDatabase = 40

// seenSession is the latest observation of a session in a lock probe.
type seenSession struct {
	s  Session
	at time.Time
}

// A deadlock lives for deadlock_timeout (1 s by default) before PostgreSQL
// breaks it, so the 1 Hz probe often misses the cycle itself. One of its
// sessions, though, waits for the whole of deadlock_timeout, and the probe
// sees that wait. The watcher remembers the sessions of every lock probe, per
// database, for DeadlockLookback, and while it holds any it reads the
// deadlock counters each tick: when one grows and no cycle was captured, it
// files a deadlock rebuilt from the waits it saw in that database.

// remember keeps the latest observation of every session of a lock probe,
// under its database.
func (w *Watcher) remember(now time.Time, sessions []Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range sessions {
		if s.Datname == "" {
			continue
		}
		db := w.recent[s.Datname]
		if db == nil {
			db = map[int32]seenSession{}
			w.recent[s.Datname] = db
		}
		db[s.PID] = seenSession{s: s, at: now}
		if len(db) > maxRecentPerDatabase {
			oldest, first := int32(0), true
			for pid, v := range db {
				if first || v.at.Before(db[oldest].at) || (v.at.Equal(db[oldest].at) && pid < oldest) {
					oldest, first = pid, false
				}
			}
			delete(db, oldest)
		}
	}
}

// pruneRecent drops what is older than the lookback; w.mu is held.
func (w *Watcher) pruneRecent(now time.Time) {
	cutoff := now.Add(-w.opts.DeadlockLookback)
	for name, db := range w.recent {
		for pid, v := range db {
			if v.at.Before(cutoff) {
				delete(db, pid)
			}
		}
		if len(db) == 0 {
			delete(w.recent, name)
		}
	}
	for name, at := range w.cycleAt {
		if at.Before(cutoff) {
			delete(w.cycleAt, name)
		}
	}
}

// deadlockIncidents reads the counters while lock waits were seen within the
// lookback, and rebuilds a deadlock for each database whose counter grew
// without a captured cycle. With no recent wait it reads nothing and forgets
// the baseline, so the next one starts from fresh counters.
func (w *Watcher) deadlockIncidents(ctx context.Context) ([]Incident, error) {
	w.mu.Lock()
	now := w.clk.Now()
	w.pruneRecent(now)
	active := len(w.recent) > 0
	if !active {
		w.deadlockBase = nil
	}
	w.mu.Unlock()
	if !active {
		return nil, nil
	}
	counts, err := w.deadlockCounts(ctx)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	base := w.deadlockBase
	w.deadlockBase = counts
	if base == nil {
		return nil, nil
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []Incident
	for _, name := range names {
		before, known := base[name]
		n := counts[name] - before
		if !known || n <= 0 {
			continue
		}
		_, cycle := w.cycleAt[name]
		delete(w.cycleAt, name)
		seen := w.recent[name]
		delete(w.recent, name)
		if cycle {
			n--
		}
		if n <= 0 || len(seen) == 0 {
			continue
		}
		out = append(out, rebuildDeadlock(now, name, n, seen))
	}
	return out, nil
}

func (w *Watcher) deadlockCounts(ctx context.Context) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, w.opts.QueryTimeout)
	defer cancel()
	rows, err := w.q.Query(ctx, deadlockQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raw []byte
	for rows.Next() {
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &counts); err != nil {
			return nil, fmt.Errorf("watch: decode deadlock counters: %w", err)
		}
	}
	return counts, nil
}

// rebuildDeadlock files n deadlocks of database name from the sessions seen
// waiting or blocking there: the longest waits first, the contested row or
// table after the longest one.
func rebuildDeadlock(now time.Time, name string, n int64, seen map[int32]seenSession) Incident {
	sessions := make([]Session, 0, len(seen))
	since := now
	for _, v := range seen {
		sessions = append(sessions, v.s)
		if v.at.Before(since) {
			since = v.at
		}
	}
	slices.SortFunc(sessions, func(a, b Session) int { return int(a.PID) - int(b.PID) })
	byPID := make(map[int32]int, len(sessions))
	pids := make([]int32, len(sessions))
	for i, s := range sessions {
		byPID[s.PID], pids[i] = i, s.PID
	}
	inc := buildLockIncident(TypeDeadlock, now, sessions, byPID, pids)
	inc.Database = name
	if inc.ID == "" {
		inc.ID = incidentID(TypeDeadlock, now, sessions[0].PID)
	}
	inc.Rebuilt, inc.Deadlocks, inc.Since = true, n, &since
	return inc
}
