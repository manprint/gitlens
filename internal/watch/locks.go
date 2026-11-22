package watch

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// lockGraphQuery answers every session waiting on a lock and every session it
// waits for, as one JSON array. For each it gives the lock it waits for and
// the row lock it holds: a backend that waits for another's row update holds
// a tuple lock on that row (relation, page, tuple: the ctid) while it waits on
// the other's transaction ID, so that lock names the contested row.
//
// It also gives the tables each session writes (the relation locks of
// RowExclusiveLock or stronger it holds): a session waiting on the transaction
// of a conflicting INSERT holds no row lock, and these name its table. An
// index is named after its table.
//
// A relation OID is looked up in pg_class only when the lock was taken in the
// connection's own database or is shared: in another database the OID names
// nothing here, or an unrelated table, so it is sent with its database name
// for the resolver to name. waitstart (PostgreSQL 14) is read through to_jsonb
// so the query also runs on older servers. PostgreSQL 13 has none: the wait is
// then measured from the start of the waiting statement, an upper bound.
const lockGraphQuery = `
WITH me AS (SELECT oid FROM pg_database WHERE datname = current_database()),
waiting AS (
  SELECT a.pid, pg_blocking_pids(a.pid) AS blocked_by
    FROM pg_stat_activity a
   WHERE a.wait_event_type = 'Lock' AND a.pid <> pg_backend_pid()
),
involved AS (
  SELECT pid FROM waiting
  UNION
  SELECT unnest(blocked_by) FROM waiting
),
locks AS (
  SELECT l.pid, l.granted, l.locktype, l.mode, l.page, l.tuple,
         l.relation, d.datname AS database, l.transactionid::text AS transactionid,
         CASE WHEN l.relation IS NOT NULL AND l.database IN (0, (SELECT oid FROM me))
              THEN COALESCE(quote_ident(n.nspname) || '.' || quote_ident(c.relname), l.relation::text) END AS relname,
         EXTRACT(epoch FROM now() - (to_jsonb(l) ->> 'waitstart')::timestamptz) AS wait_seconds,
         NOT (to_jsonb(l) ? 'waitstart') AS no_waitstart
    FROM pg_locks l
    LEFT JOIN pg_index i ON i.indexrelid = l.relation AND l.database IN (0, (SELECT oid FROM me))
    LEFT JOIN pg_class c ON c.oid = COALESCE(i.indrelid, l.relation) AND l.database IN (0, (SELECT oid FROM me))
    LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_database d ON d.oid = l.database
   WHERE l.pid IN (SELECT pid FROM involved)
     AND (NOT l.granted OR l.locktype = 'tuple'
          OR (l.locktype = 'relation' AND l.mode IN ('RowExclusiveLock', 'ShareRowExclusiveLock', 'ExclusiveLock', 'AccessExclusiveLock')))
)
SELECT COALESCE(json_agg(s ORDER BY s.pid), '[]') FROM (
  SELECT a.pid,
         COALESCE(a.datname, '') AS datname,
         COALESCE(a.usename, '') AS usename,
         COALESCE(a.application_name, '') AS application_name,
         COALESCE(host(a.client_addr), '') AS client_addr,
         COALESCE(a.state, '') AS state,
         COALESCE(a.wait_event_type, '') AS wait_event_type,
         COALESCE(a.wait_event, '') AS wait_event,
         a.backend_start,
         a.xact_start,
         a.query_start,
         COALESCE(EXTRACT(epoch FROM now() - a.xact_start), 0) AS xact_seconds,
         COALESCE(EXTRACT(epoch FROM now() - a.query_start), 0) AS query_seconds,
         COALESCE(EXTRACT(epoch FROM now() - a.state_change), 0) AS state_seconds,
         to_jsonb(a) ->> 'query_id' AS query_id,
         COALESCE(a.query, '') AS query,
         COALESCE(octet_length(a.query), 0) >= (SELECT setting::int FROM pg_settings WHERE name = 'track_activity_query_size') - 4 AS query_cut,
         COALESCE(w.blocked_by, '{}') AS blocked_by,
         (SELECT json_build_object('locktype', l.locktype, 'mode', l.mode, 'relation', l.relname,
                                   'relation_oid', l.relation::bigint, 'database', l.database,
                                   'page', l.page, 'tuple', l.tuple, 'transactionid', l.transactionid,
                                   'wait_seconds', COALESCE(l.wait_seconds,
                                     CASE WHEN l.no_waitstart THEN EXTRACT(epoch FROM now() - a.query_start) END))
            FROM locks l WHERE l.pid = a.pid AND NOT l.granted LIMIT 1) AS wait,
         (SELECT json_build_object('locktype', l.locktype, 'mode', l.mode, 'relation', l.relname,
                                   'relation_oid', l.relation::bigint, 'database', l.database,
                                   'page', l.page, 'tuple', l.tuple)
            FROM locks l WHERE l.pid = a.pid AND l.granted AND l.locktype = 'tuple' LIMIT 1) AS "row",
         (SELECT json_agg(json_build_object('locktype', l.locktype, 'mode', l.mode, 'relation', l.relname,
                                            'relation_oid', l.relation::bigint, 'database', l.database) ORDER BY l.relation)
            FROM locks l WHERE l.pid = a.pid AND l.granted AND l.locktype = 'relation') AS writes
    FROM pg_stat_activity a
    LEFT JOIN waiting w ON w.pid = a.pid
   WHERE a.pid IN (SELECT pid FROM involved)
   ORDER BY a.pid
   LIMIT 60
) s`

// maxIncidentSessions caps the sessions one incident lists.
const maxIncidentSessions = 20

// waitKey identifies one wait of one session: the same session waiting for
// the same thing on two probes is one wait.
func waitKey(s Session) string {
	id := s.SessionID
	if id == "" {
		id = "pid" + itoa(int64(s.PID))
	}
	if s.Wait == nil {
		return id
	}
	return strings.Join([]string{id, s.Wait.LockType, s.Wait.Mode, s.Wait.Relation, s.Wait.CTID(), s.Wait.TransactionID}, "|")
}

// lockIncidents turns one lock probe into incidents: one per group of
// sessions that wait on each other, when one of its waits lasted long enough
// and the group was not reported in the last Repeat; a deadlock incident,
// instead, as soon as the group holds a cycle of waits.
func (w *Watcher) lockIncidents(sessions []Session) []Incident {
	now := w.clk.Now()
	byPID := make(map[int32]int, len(sessions))
	for i, s := range sessions {
		byPID[s.PID] = i
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	seen := map[string]bool{}
	eligible := map[int32]bool{}
	for _, s := range sessions {
		if len(s.BlockedBy) == 0 {
			continue
		}
		key := waitKey(s)
		seen[key] = true
		waited := s.Wait != nil && s.Wait.WaitSeconds >= w.opts.LockWaitMin.Seconds()
		// Without waitstart a wait seen on two probes in a row has lasted at
		// least one tick.
		unknown := s.Wait == nil || s.Wait.WaitSeconds == 0
		if waited || (unknown && w.waitSeen[key]) {
			eligible[s.PID] = true
		}
	}
	w.waitSeen = seen

	cycles := findCycles(sessions, byPID)
	inCycle := map[int32]bool{}
	cyclePresent := map[string]bool{}
	var out []Incident
	for _, cycle := range cycles {
		key := groupKey(sessions, byPID, cycle)
		cyclePresent[key] = true
		for _, pid := range cycle {
			inCycle[pid] = true
		}
		if w.cycleSent[key] {
			continue
		}
		inc := buildLockIncident(TypeDeadlock, now, sessions, byPID, component(sessions, byPID, cycle[0]))
		w.cycleAt[inc.Database] = now
		out = append(out, inc)
	}
	w.cycleSent = cyclePresent

	for k, at := range w.lockSent {
		if now.Sub(at) > 2*w.opts.Repeat {
			delete(w.lockSent, k)
		}
	}
	done := map[int32]bool{}
	for _, s := range sessions {
		if !eligible[s.PID] || done[s.PID] || inCycle[s.PID] {
			continue
		}
		group := component(sessions, byPID, s.PID)
		cyclic := false
		for _, pid := range group {
			done[pid] = true
			cyclic = cyclic || inCycle[pid]
		}
		if cyclic {
			continue
		}
		key := groupKey(sessions, byPID, roots(sessions, byPID, group))
		if at, ok := w.lockSent[key]; ok && now.Sub(at) < w.opts.Repeat {
			continue
		}
		w.lockSent[key] = now
		out = append(out, buildLockIncident(TypeLockWait, now, sessions, byPID, group))
	}
	return out
}

// buildLockIncident lists the group's waiters, longest wait first, then the
// sessions that block them, and names the contested object after the longest
// wait.
func buildLockIncident(kind string, now time.Time, sessions []Session, byPID map[int32]int, group []int32) Incident {
	var waiters, holders []Session
	for _, pid := range group {
		s := sessions[byPID[pid]]
		if len(s.BlockedBy) > 0 {
			waiters = append(waiters, s)
		} else {
			holders = append(holders, s)
		}
	}
	sort.SliceStable(waiters, func(i, j int) bool { return waitSeconds(waiters[i]) > waitSeconds(waiters[j]) })
	sort.SliceStable(holders, func(i, j int) bool { return holders[i].XactSeconds > holders[j].XactSeconds })
	all := append(waiters, holders...)
	inc := Incident{Type: kind, CapturedAt: now}
	if len(all) > maxIncidentSessions {
		inc.SessionsOmitted = len(all) - maxIncidentSessions
		all = all[:maxIncidentSessions]
	}
	inc.Sessions = all
	if len(waiters) > 0 {
		first := waiters[0]
		inc.ID = incidentID(kind, now, first.PID)
		inc.Database = first.Datname
		inc.WaitSeconds = waitSeconds(first)
		if first.Wait != nil {
			inc.LockMode = first.Wait.Mode
			inc.Relation = first.Wait.Relation
			inc.CTID = first.Wait.CTID()
		}
		if first.Row != nil {
			if inc.Relation == "" {
				inc.Relation = first.Row.Relation
			}
			if inc.CTID == "" {
				inc.CTID = first.Row.CTID()
			}
		}
		if inc.Relation == "" {
			inc.Relation = writtenTable(first)
		}
	}
	return inc
}

// lockRefs are the locks of s a relation name may be missing from.
func lockRefs(s *Session) []*LockRef {
	refs := make([]*LockRef, 0, 2+len(s.writes))
	for _, l := range []*LockRef{s.Wait, s.Row} {
		if l != nil {
			refs = append(refs, l)
		}
	}
	for i := range s.writes {
		refs = append(refs, &s.writes[i])
	}
	return refs
}

// heldFromWrites names the tables each session of a lock probe writes, once
// each and without the system catalogs.
func heldFromWrites(sessions []Session) {
	for i := range sessions {
		s := &sessions[i]
		seen := map[string]bool{}
		for _, l := range s.writes {
			name := l.Relation
			if name == "" || seen[name] || systemRelation(name) || len(s.HeldRelations) == maxHeldRelations {
				continue
			}
			seen[name] = true
			s.HeldRelations = append(s.HeldRelations, name)
		}
	}
}

func systemRelation(name string) bool {
	for _, prefix := range []string{"pg_catalog.", "information_schema.", "pg_toast"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// writtenTable is the table a waiting session is writing, for a wait that
// names none (a transaction ID): the one its statement names, else the first.
func writtenTable(s Session) string {
	query := strings.ToLower(s.Query)
	for _, name := range s.HeldRelations {
		bare := strings.Trim(name[strings.LastIndexByte(name, '.')+1:], `"`)
		if bare != "" && containsIdent(query, strings.ToLower(bare)) {
			return name
		}
	}
	if len(s.HeldRelations) > 0 {
		return s.HeldRelations[0]
	}
	return ""
}

func waitSeconds(s Session) float64 {
	if s.Wait == nil {
		return 0
	}
	return s.Wait.WaitSeconds
}

// component is every session linked to pid by a wait, in either direction.
func component(sessions []Session, byPID map[int32]int, pid int32) []int32 {
	adj := map[int32][]int32{}
	for _, s := range sessions {
		for _, b := range s.BlockedBy {
			if _, ok := byPID[b]; ok {
				adj[s.PID] = append(adj[s.PID], b)
				adj[b] = append(adj[b], s.PID)
			}
		}
	}
	seen := map[int32]bool{pid: true}
	queue := []int32{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	out := make([]int32, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// roots are the sessions of a group that wait for nobody: the ones holding
// the locks. A group with none is all cycle and keys on every member.
func roots(sessions []Session, byPID map[int32]int, group []int32) []int32 {
	var out []int32
	for _, pid := range group {
		if len(sessions[byPID[pid]].BlockedBy) == 0 {
			out = append(out, pid)
		}
	}
	if len(out) == 0 {
		return group
	}
	return out
}

func groupKey(sessions []Session, byPID map[int32]int, pids []int32) string {
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		s := sessions[byPID[pid]]
		id := s.SessionID
		if id == "" {
			id = "pid" + itoa(int64(pid))
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}

// findCycles returns each cycle of the wait graph once, as its members in
// the order of the waits, starting from the lowest PID.
func findCycles(sessions []Session, byPID map[int32]int) [][]int32 {
	const (
		white = iota
		grey
		black
	)
	color := map[int32]int{}
	var stack []int32
	found := map[string]bool{}
	var cycles [][]int32
	var visit func(pid int32)
	visit = func(pid int32) {
		color[pid] = grey
		stack = append(stack, pid)
		for _, next := range sessions[byPID[pid]].BlockedBy {
			if _, ok := byPID[next]; !ok {
				continue
			}
			switch color[next] {
			case white:
				visit(next)
			case grey:
				start := slices.Index(stack, next)
				cycle := slices.Clone(stack[start:])
				low := slices.Index(cycle, slices.Min(cycle))
				cycle = append(cycle[low:], cycle[:low]...)
				key := ""
				for _, p := range cycle {
					key += itoa(int64(p)) + ","
				}
				if !found[key] {
					found[key] = true
					cycles = append(cycles, cycle)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[pid] = black
	}
	pids := make([]int32, 0, len(sessions))
	for _, s := range sessions {
		pids = append(pids, s.PID)
	}
	slices.Sort(pids)
	for _, pid := range pids {
		if color[pid] == white {
			visit(pid)
		}
	}
	return cycles
}

// containsIdent reports whether ident occurs in query as a whole identifier.
func containsIdent(query, ident string) bool {
	for from := 0; ; {
		i := strings.Index(query[from:], ident)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(ident)
		if (start == 0 || !identByte(query[start-1])) && (end == len(query) || !identByte(query[end])) {
			return true
		}
		from = start + 1
	}
}

func identByte(b byte) bool {
	return b == '_' || b == '$' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}
