package check

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&locksCheck{}) }

// locksCheck samples the blocking tree and the session list. sessionQueryText
// is the opt-in (checks.locks.session_query_text) that ships the text of the
// listed sessions; it is off by default.
type locksCheck struct{ sessionQueryText bool }

func (c *locksCheck) SetSessionQueryText(enabled bool) { c.sessionQueryText = enabled }

// SessionQueryTextEnabled is the value the check runs with.
func (c *locksCheck) SessionQueryTextEnabled() bool { return c.sessionQueryText }

func (c *locksCheck) Name() string { return "locks" }
func (c *locksCheck) Requires() Requirements {
	return Requirements{Scope: ScopeInstance, PermTier: pgtype.TierReadOnly}
}
func (c *locksCheck) DefaultInterval() time.Duration { return 10 * time.Second }
func (c *locksCheck) Timeout() time.Duration         { return 10 * time.Second }

type lockRow struct {
	PID             int32
	Datname         string
	Username        string
	ApplicationName string
	ClientAddr      string
	State           string
	WaitEventType   string
	WaitEvent       string
	XactAge         float64
	StateAge        float64
	Query           string
	BlockedBy       []int32
	BackendStart    *time.Time
	// QueryAge is the seconds since query_start; the ages of backend_xid and
	// backend_xmin are in transactions, never ids. QueryID is the string of the
	// bigint, empty when NULL or 0 (compute_query_id off).
	QueryAge       float64
	BackendXidAge  *int64
	BackendXminAge *int64
	QueryID        string
	LeaderPID      *int32
	QueryStart     *time.Time
	// WaitStartedAt and WaitDuration describe the lock this session waits on,
	// read from pg_locks; both are nil when it waits on none.
	WaitStartedAt *time.Time
	WaitDuration  *float64
	// LockMode, LockType and LockRelation describe that lock too; nil when the
	// session waits on none. LockRelation is nil as well when the lock names no
	// relation, or one the agent's database cannot name (see locksQuery).
	LockMode     *string
	LockType     *string
	LockRelation *string
}

// lockSessionsLimit caps the session list, which the activity view shows
// oldest transaction first.
const lockSessionsLimit = 100

type lockTree struct {
	SampledAt time.Time  `json:"sampled_at"`
	Nodes     []lockNode `json:"nodes"`
	Sessions  []lockNode `json:"sessions"`
	// SessionsTruncated is set when Sessions was cut to lockSessionsLimit.
	SessionsTruncated bool `json:"sessions_truncated,omitempty"`
}

type lockNode struct {
	PID             int32   `json:"pid"`
	Datname         string  `json:"datname"`
	Username        string  `json:"usename"`
	ApplicationName string  `json:"application_name"`
	ClientAddr      string  `json:"client_addr"`
	State           string  `json:"state"`
	WaitEventType   string  `json:"wait_event_type"`
	WaitEvent       string  `json:"wait_event"`
	XactAge         float64 `json:"xact_age"`
	StateAge        float64 `json:"state_age"`
	// Query is set only on a node, a session of the blocking tree. The session
	// list leaves it empty, so it is omitted there.
	Query     string  `json:"query,omitempty"`
	BlockedBy []int32 `json:"blocked_by"`
	// BackendStart tells this session from a later one given the same PID;
	// the UI sends it back with a cancel or terminate. It is NULL when the
	// role may not see the session.
	BackendStart *time.Time `json:"backend_start,omitempty"`
	// SessionID names the session across PID reuse: the lower-hex of
	// BackendStart in microseconds since the Unix epoch, a dot, and the PID.
	// A session that started at 2026-10-23 09:00:00.000001 UTC with PID 1234
	// is "65e7e3480a401.1234". It is empty when BackendStart is NULL.
	SessionID string `json:"session_id,omitempty"`
	// WaitStartedAt is when the session began waiting for the lock it waits on
	// now, from pg_locks.waitstart on the server. WaitDuration is the seconds
	// from that moment to now() on the server, taken at the sample, so both
	// clocks are the server's and the agent's clock does not enter it.
	WaitStartedAt *time.Time `json:"wait_started_at,omitempty"`
	WaitDuration  *float64   `json:"wait_duration,omitempty"`
	// LockMode is the mode the session asks for on the lock it waits on, as
	// pg_locks.mode spells it (ShareLock, ExclusiveLock, ...). LockType is
	// pg_locks.locktype (transactionid, relation, tuple, ...). LockRelation is
	// the relation the lock is on, schema-qualified. The tree used to
	// carry none of them, so the console's Lock mode column read "—" for
	// every session, the two sessions of a deadlock included.
	LockMode     *string `json:"lock_mode,omitempty"`
	LockType     *string `json:"lock_type,omitempty"`
	LockRelation *string `json:"lock_relation,omitempty"`
	// QueryAge is the seconds since the current query started. BackendXidAge
	// and BackendXminAge are ages in transactions of the session's xid and
	// xmin (omitted when it has none). QueryID is the statement's query id as
	// a string (omitted when NULL or 0). LeaderPID is set on a parallel worker.
	QueryAge       float64    `json:"query_age"`
	BackendXidAge  *int64     `json:"backend_xid_age,omitempty"`
	BackendXminAge *int64     `json:"backend_xmin_age,omitempty"`
	QueryID        string     `json:"query_id,omitempty"`
	LeaderPID      *int32     `json:"leader_pid,omitempty"`
	QueryStart     *time.Time `json:"query_start,omitempty"`
}

// locksQuery tolerates every nullable column: client_addr is NULL for each
// Unix-socket client, and datname/usename/state are NULL for a backend still
// starting up. Scanning one NULL into a string used to fail the whole scrape,
// so the blocking tree went dark whenever a local client was connected.
//
// pg_blocking_pids() walks the lock manager; it is only asked about sessions
// that are actually waiting on a heavyweight lock instead of every backend
// every ten seconds.
//
// The wait columns come from the waiting lock of each session: pg_locks.waitstart
// is PostgreSQL 14 and later, so every supported version has it. The wait
// duration is measured on the server with now(), as is the transaction age, so
// the two clocks agree. A session that waits on no lock has NULL in both.
//
// The lock columns describe the same waiting lock. A backend waits on at most
// one heavyweight lock at a time, so the min() of each column is that lock's
// value; the aggregate only keeps the join one row per session. The relation is
// named only when the lock is in the agent's own database or on a shared
// catalog (pg_locks.database 0): the agent connects to one database, and the
// OID of a relation in another one would be looked up in the wrong pg_class,
// where it names nothing or, worse, an unrelated table. Elsewhere it is NULL,
// and the lock type and mode remain. The name is always schema-qualified, each
// part quoted where needed, from pg_class and pg_namespace: regclass would leave
// the schema out whenever it is on the agent's search_path, so the same table
// read differently from one agent configuration to the next. A relation dropped
// meanwhile has no pg_class row and is shown by its OID. Tuple, page and extend
// locks carry their relation too and are named the same way.
//
// The last column says whether the text is size minus four or more bytes long,
// the test the query below applies, and so whether it may have been cut. The
// byte count is taken on the server, where the cut happens. The cut keeps at
// most the size less one byte and stops at a
// character boundary, so a cut text can be as short as the size less four;
// the four-byte margin covers every cut, and a probe on PostgreSQL 18 cut a
// text to the size less two.
const locksQuery = `
SELECT a.pid,
       COALESCE(a.datname, ''),
       COALESCE(a.usename, ''),
       COALESCE(a.application_name, ''),
       COALESCE(a.client_addr::text, ''),
       COALESCE(a.state, ''),
       COALESCE(a.wait_event_type, ''),
       COALESCE(a.wait_event, ''),
       COALESCE(EXTRACT(epoch FROM now() - a.xact_start), 0),
       COALESCE(EXTRACT(epoch FROM now() - a.state_change), 0),
       COALESCE(a.query, ''),
       CASE WHEN a.wait_event_type = 'Lock' THEN pg_blocking_pids(a.pid) ELSE '{}'::int[] END,
       COALESCE(EXTRACT(epoch FROM now() - a.query_start), 0),
       age(a.backend_xid)::bigint,
       age(a.backend_xmin)::bigint,
       a.query_id,
       a.leader_pid,
       a.query_start,
       a.backend_start,
       w.waitstart,
       EXTRACT(epoch FROM now() - w.waitstart),
       w.mode,
       w.locktype,
       w.relation,
       COALESCE(octet_length(a.query), 0) >= (SELECT setting::int FROM pg_settings WHERE name = 'track_activity_query_size') - 4
FROM pg_stat_activity a
LEFT JOIN (SELECT l.pid,
                  min(l.waitstart) AS waitstart,
                  min(l.mode) AS mode,
                  min(l.locktype) AS locktype,
                  min(CASE WHEN l.relation IS NOT NULL
                            AND l.database IN (0, (SELECT oid FROM pg_database WHERE datname = current_database()))
                           THEN COALESCE(quote_ident(n.nspname) || '.' || quote_ident(c.relname), l.relation::text) END) AS relation
           FROM pg_locks l
           LEFT JOIN pg_class c ON c.oid = l.relation
           LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
           WHERE NOT l.granted GROUP BY l.pid) w ON w.pid = a.pid
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid()`

func (c *locksCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.Conn(ctx)
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, locksQuery)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var parsed []lockRow
	for rows.Next() {
		var r lockRow
		var cut bool
		var queryID *int64
		if err := rows.Scan(&r.PID, &r.Datname, &r.Username, &r.ApplicationName, &r.ClientAddr, &r.State, &r.WaitEventType, &r.WaitEvent, &r.XactAge, &r.StateAge, &r.Query, &r.BlockedBy, &r.QueryAge, &r.BackendXidAge, &r.BackendXminAge, &queryID, &r.LeaderPID, &r.QueryStart, &r.BackendStart, &r.WaitStartedAt, &r.WaitDuration, &r.LockMode, &r.LockType, &r.LockRelation, &cut); err != nil {
			return Result{}, err
		}
		if queryID != nil && *queryID != 0 {
			r.QueryID = strconv.FormatInt(*queryID, 10)
		}
		// PostgreSQL's clock can lead the collector's by microseconds.
		if r.QueryAge < 0 {
			r.QueryAge = 0
		}
		// A statement can carry a role password, and the tree is shipped to
		// the server and shown in the UI, so a text that may hold one is
		// withheld before the cut. A text the server already cut is withheld
		// too, because the sign of a credential may lie past the cut.
		if cut {
			r.Query = withheldQuery
		} else {
			r.Query = redactQueryText(r.Query)
		}
		r.Query = truncateUTF8(r.Query, 2048)
		parsed = append(parsed, r)
	}
	return buildLocksResultText(parsed, t.Clock().Now(), c.sessionQueryText), rows.Err()
}

// buildLocksResultText builds the sample. sessionText keeps the text on the
// session list; the rows already went through redactQueryText/withheldQuery in
// Scrape, so a text that may hold a credential never gets here raw.
func buildLocksResultText(rows []lockRow, sampledAt time.Time, sessionText bool) Result {
	blocked := 0
	blocking := make(map[int32]struct{})
	waits := make(map[string]int)
	maxAge := 0.0
	nodes := make([]lockNode, 0, len(rows))
	nodePIDs := make(map[int32]struct{}, len(rows))
	for _, r := range rows {
		if len(r.BlockedBy) == 0 {
			continue
		}
		blocked++
		for _, pid := range r.BlockedBy {
			blocking[pid] = struct{}{}
		}
		waits[r.WaitEvent]++
		if r.StateAge > maxAge {
			maxAge = r.StateAge
		}
		nodes = append(nodes, lockNodeOf(r))
		nodePIDs[r.PID] = struct{}{}
	}
	// The blocking session has no blocked_by values of its own, so the first
	// pass intentionally skips it. Keep it in the snapshot when PostgreSQL
	// returned the referenced PID; the UI needs that node to build the root of
	// the blocking tree instead of rendering only the blocked child.
	for _, r := range rows {
		if _, isBlocking := blocking[r.PID]; isBlocking {
			if _, alreadyIncluded := nodePIDs[r.PID]; !alreadyIncluded {
				nodes = append(nodes, lockNodeOf(r))
				nodePIDs[r.PID] = struct{}{}
			}
		}
	}
	// Every backend used to be shipped with its query text on each sample,
	// blocked or not, which grows with the number of connections. The text is
	// shipped for the sessions in the tree only, in nodes; the session list
	// carries every other field but no text, and is capped instead.
	byAge := make([]lockRow, len(rows))
	copy(byAge, rows)
	sort.SliceStable(byAge, func(i, j int) bool { return byAge[i].XactAge > byAge[j].XactAge })
	sessions := make([]lockNode, 0, min(len(byAge), lockSessionsLimit))
	sessionsTruncated := false
	for i, r := range byAge {
		if i == lockSessionsLimit {
			sessionsTruncated = true
			break
		}
		session := lockNodeOf(r)
		if !sessionText {
			session.Query = ""
		}
		sessions = append(sessions, session)
	}
	metrics := []pgtype.Metric{
		{Name: "pg_blocked_sessions", Value: float64(blocked), Kind: pgtype.KindGauge},
		{Name: "pg_blocking_sessions", Value: float64(len(blocking)), Kind: pgtype.KindGauge},
		{Name: "pg_max_block_age_seconds", Value: maxAge, Kind: pgtype.KindGauge},
	}
	type waitCount struct {
		name  string
		count int
	}
	ordered := make([]waitCount, 0, len(waits))
	for name, count := range waits {
		ordered = append(ordered, waitCount{name, count})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].count == ordered[j].count {
			return ordered[i].name < ordered[j].name
		}
		return ordered[i].count > ordered[j].count
	})
	other := 0
	for i, w := range ordered {
		if i < 10 {
			metrics = append(metrics, pgtype.Metric{Name: "pg_lock_waits", Value: float64(w.count), Kind: pgtype.KindGauge, Labels: map[string]string{"wait_event": w.name}})
		} else {
			other += w.count
		}
	}
	if other > 0 {
		metrics = append(metrics, pgtype.Metric{Name: "pg_lock_waits", Value: float64(other), Kind: pgtype.KindGauge, Labels: map[string]string{"wait_event": "other"}})
	}
	// A session cut by the cap was observed, not gone, but the list the UI shows
	// is not exhaustive, so the result is Partial. The metrics and the blocking
	// tree are complete either way.
	result := Result{Metrics: metrics, Partial: sessionsTruncated}
	// Persist an empty tree as well as a populated one. Without the empty
	// snapshot, a released blocker remains visible in the API until the
	// fifteen-minute retention cleanup, rather than disappearing on the next
	// locks scrape.
	if raw, err := json.Marshal(lockTree{SampledAt: sampledAt, Nodes: nodes, Sessions: sessions, SessionsTruncated: sessionsTruncated}); err == nil {
		result.Facts = []Fact{{Kind: "lock_tree", Key: "current", ValueJSON: raw}}
	}
	return result
}

// lockNodeOf converts a row to the node shape. The query text is kept here;
// the caller decides whether the node is shipped with it.
func lockNodeOf(r lockRow) lockNode {
	return lockNode{
		PID:             r.PID,
		Datname:         r.Datname,
		Username:        r.Username,
		ApplicationName: r.ApplicationName,
		ClientAddr:      r.ClientAddr,
		State:           r.State,
		WaitEventType:   r.WaitEventType,
		WaitEvent:       r.WaitEvent,
		XactAge:         r.XactAge,
		StateAge:        r.StateAge,
		Query:           r.Query,
		BlockedBy:       r.BlockedBy,
		BackendStart:    r.BackendStart,
		SessionID:       lockSessionID(r.PID, r.BackendStart),
		WaitStartedAt:   r.WaitStartedAt,
		WaitDuration:    r.WaitDuration,
		LockMode:        r.LockMode,
		LockType:        r.LockType,
		LockRelation:    r.LockRelation,
		QueryAge:        r.QueryAge,
		BackendXidAge:   r.BackendXidAge,
		BackendXminAge:  r.BackendXminAge,
		QueryID:         r.QueryID,
		LeaderPID:       r.LeaderPID,
		QueryStart:      r.QueryStart,
	}
}

// lockSessionID is the identity of a session that survives PID reuse: the
// start time in microseconds since the Unix epoch, in lower-case hex, then a
// dot and the PID. A reused PID starts later, so it gets a new identity. An
// empty result means the start time is not visible to the role.
func lockSessionID(pid int32, started *time.Time) string {
	if started == nil {
		return ""
	}
	return strconv.FormatInt(started.UnixMicro(), 16) + "." + strconv.FormatInt(int64(pid), 10)
}

// truncateUTF8 cuts s to at most limit bytes without splitting a multi-byte
// character, which would otherwise reach the server as invalid UTF-8.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
