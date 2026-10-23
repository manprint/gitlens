package check

import (
	"context"
	"encoding/json"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&locksCheck{}) }

type locksCheck struct{}

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
}

// lockSessionsLimit caps the session list, which the activity view shows
// oldest transaction first. lockIdleQueryLimit is the query text kept for a
// session that is not in the blocking tree; those keep the full text.
const (
	lockSessionsLimit  = 100
	lockIdleQueryLimit = 256
)

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
	Query           string  `json:"query"`
	BlockedBy       []int32 `json:"blocked_by"`
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
// The last column says whether the text reached the size PostgreSQL cuts a
// session's text to. The byte count is taken on the server, where the cut
// happens. A cut text is clipped at a character boundary, so it can be up to
// three bytes short of the size; the four-byte margin covers that, and a
// probe on PostgreSQL 18 cut a text to the size less two.
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
       COALESCE(octet_length(a.query), 0) >= (SELECT setting::int FROM pg_settings WHERE name = 'track_activity_query_size') - 4
FROM pg_stat_activity a
WHERE a.backend_type = 'client backend'
  AND a.pid <> pg_backend_pid()`

// withheldQuery stands for a session text the server cut. The cut text may
// hide the keyword that makes a credential, so it is not redacted, only
// withheld.
const withheldQuery = "[redacted]"

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
		if err := rows.Scan(&r.PID, &r.Datname, &r.Username, &r.ApplicationName, &r.ClientAddr, &r.State, &r.WaitEventType, &r.WaitEvent, &r.XactAge, &r.StateAge, &r.Query, &r.BlockedBy, &cut); err != nil {
			return Result{}, err
		}
		// A statement can carry a role password, and the tree is shipped to
		// the server and shown in the UI, so the credentials go before the cut.
		// A text the server already cut cannot be redacted safely, because the
		// keyword that makes its value a credential may lie past the cut.
		if cut {
			r.Query = withheldQuery
		} else {
			r.Query = redactQueryText(r.Query)
		}
		r.Query = truncateUTF8(r.Query, 2048)
		parsed = append(parsed, r)
	}
	return buildLocksResult(parsed, t.Clock().Now()), rows.Err()
}

func buildLocksResult(rows []lockRow, sampledAt time.Time) Result {
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
		nodes = append(nodes, lockNode(r))
		nodePIDs[r.PID] = struct{}{}
	}
	// The blocking session has no blocked_by values of its own, so the first
	// pass intentionally skips it. Keep it in the snapshot when PostgreSQL
	// returned the referenced PID; the UI needs that node to build the root of
	// the blocking tree instead of rendering only the blocked child.
	for _, r := range rows {
		if _, isBlocking := blocking[r.PID]; isBlocking {
			if _, alreadyIncluded := nodePIDs[r.PID]; !alreadyIncluded {
				nodes = append(nodes, lockNode(r))
				nodePIDs[r.PID] = struct{}{}
			}
		}
	}
	// Every backend used to be shipped with its full query on each sample,
	// blocked or not, which grows with the number of connections. Only the
	// sessions in the tree keep their full text; the list is capped instead.
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
		session := lockNode(r)
		if _, inTree := nodePIDs[r.PID]; !inTree {
			session.Query = truncateUTF8(session.Query, lockIdleQueryLimit)
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
