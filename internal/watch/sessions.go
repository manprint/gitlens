package watch

import (
	"strconv"
	"strings"
)

// sessionsQuery answers, as one JSON array, the client sessions running one
// statement for more than $1 seconds, those idle inside a transaction for more
// than $2 seconds and those whose transaction has been open for more than $3
// seconds, with the user tables they hold a lock on (the
// connection's own database only, where the OIDs can be named) and the age of
// the snapshot they keep, which holds back vacuum.
const sessionsQuery = `
SELECT COALESCE(json_agg(s ORDER BY s.xact_seconds DESC), '[]') FROM (
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
         COALESCE(age(a.backend_xmin), 0)::bigint AS xmin_age,
         (SELECT array_agg(DISTINCT quote_ident(n.nspname) || '.' || quote_ident(c.relname))
            FROM pg_locks l
            JOIN pg_class c ON c.oid = l.relation
            JOIN pg_namespace n ON n.oid = c.relnamespace
           WHERE l.pid = a.pid AND l.granted AND l.locktype = 'relation'
             AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
             AND c.relkind IN ('r', 'p', 'm')
             AND n.nspname NOT IN ('pg_catalog', 'information_schema')
             AND n.nspname NOT LIKE 'pg_toast%') AS held_relations
    FROM pg_stat_activity a
   WHERE a.backend_type = 'client backend'
     AND a.pid <> pg_backend_pid()
     AND COALESCE(a.application_name, '') NOT LIKE 'pglens/%'
     AND ((a.state = 'active' AND a.query_start < now() - make_interval(secs => $1))
       OR (a.state LIKE 'idle in transaction%' AND a.state_change < now() - make_interval(secs => $2))
       OR a.xact_start < now() - make_interval(secs => $3))
   ORDER BY a.xact_start NULLS LAST
   LIMIT 20
) s`

// maxHeldRelations caps the tables listed for one session.
const maxHeldRelations = 10

// sessionKind classifies a session of the probe: an idle transaction past its
// limit, else a statement past its own, else a transaction open too long
// through statements and pauses that are each short.
func (w *Watcher) sessionKind(s Session) string {
	switch {
	case strings.HasPrefix(s.State, "idle in transaction") && s.StateSeconds >= w.opts.IdleInTransaction.Seconds():
		return TypeIdleInXact
	case s.State == "active" && s.QuerySeconds >= w.opts.LongQuery.Seconds():
		return TypeLongQuery
	}
	return TypeLongXact
}

// sessionIncidents sends one incident per new long statement, idle
// transaction or long transaction.
func (w *Watcher) sessionIncidents(sessions []Session) []Incident {
	now := w.clk.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	present := map[string]bool{}
	var out []Incident
	for _, s := range sessions {
		kind := w.sessionKind(s)
		key := w.sessKey(kind, s)
		present[key] = true
		if w.sessSent[key] {
			continue
		}
		if len(s.HeldRelations) > maxHeldRelations {
			s.HeldRelations = s.HeldRelations[:maxHeldRelations]
		}
		inc := Incident{ID: incidentID(kind, now, s.PID), Type: kind, CapturedAt: now, Database: s.Datname, Sessions: []Session{s}}
		if len(s.HeldRelations) > 0 {
			inc.Relation = s.HeldRelations[0]
		}
		out = append(out, inc)
	}
	w.sessSent = present
	return out
}

// sessKey keys an episode on the session and the start of its statement,
// or of its transaction when idle or long.
func (w *Watcher) sessKey(kind string, s Session) string {
	id := s.SessionID
	if id == "" {
		id = "pid" + itoa(int64(s.PID))
	}
	start := s.QueryStart
	if kind != TypeLongQuery {
		start = s.XactStart
	}
	at := ""
	if start != nil {
		at = strconv.FormatInt(start.UnixMicro(), 16)
	}
	return kind + "|" + id + "|" + at
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
