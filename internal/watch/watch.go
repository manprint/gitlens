// Package watch captures short-lived incidents that a periodic check misses:
// a session waiting on a lock (with the table, the row and the session that
// holds it), a cycle of waits (a deadlock in the making), a statement running
// too long, a transaction left open while idle. It only reads pg_stat_activity
// and pg_locks, and runs on the ASH sampler's dedicated connection, after the
// sampler's own tick, so it opens no connection of its own.
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/redact"
)

// Incident types.
const (
	TypeLockWait   = "lock_wait"
	TypeDeadlock   = "deadlock"
	TypeLongQuery  = "long_query"
	TypeIdleInXact = "idle_in_transaction"
	TypeLongXact   = "long_transaction"
)

// FactKind is the wire fact kind an incident travels as.
const FactKind = "incident"

// Querier is the connection the watcher reads through.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Resolver names relations of another database, which the watcher's
// connection cannot read: datname, then the OIDs; the answer maps an OID to
// its schema-qualified name. It is best effort: a missing OID stays unnamed.
type Resolver func(ctx context.Context, datname string, oids []uint32) map[uint32]string

// Options tune the watcher. Zero values take the defaults.
type Options struct {
	// LockWaitMin is how long a lock wait must last before it is captured.
	LockWaitMin time.Duration
	// LongQuery is the run time past which an active statement is captured.
	LongQuery time.Duration
	// IdleInTransaction is the idle time past which an open transaction is captured.
	IdleInTransaction time.Duration
	// LongTransaction is the age past which an open transaction is captured,
	// busy or not.
	LongTransaction time.Duration
	// SessionEvery runs the long-query and idle probe every n ticks.
	SessionEvery int
	// MaxPerMinute caps the incidents sent per minute; the rest are counted.
	MaxPerMinute int
	// Repeat is how long a lock incident with the same sessions stays quiet.
	Repeat time.Duration
	// QueryTimeout bounds each probe.
	QueryTimeout time.Duration
	// DeadlockLookback is how long the sessions of a lock probe are kept to
	// rebuild a deadlock PostgreSQL counted but the probe did not see as a
	// cycle; while any are kept the deadlock counters are read every tick.
	DeadlockLookback time.Duration
}

// DefaultOptions are the watcher's defaults.
func DefaultOptions() Options {
	return Options{
		LockWaitMin:       500 * time.Millisecond,
		LongQuery:         5 * time.Minute,
		IdleInTransaction: time.Minute,
		LongTransaction:   10 * time.Minute,
		SessionEvery:      10,
		MaxPerMinute:      12,
		Repeat:            time.Minute,
		QueryTimeout:      450 * time.Millisecond,
		DeadlockLookback:  2 * time.Minute,
	}
}

func (o Options) withDefaults() Options {
	d := DefaultOptions()
	if o.LockWaitMin <= 0 {
		o.LockWaitMin = d.LockWaitMin
	}
	if o.LongQuery <= 0 {
		o.LongQuery = d.LongQuery
	}
	if o.IdleInTransaction <= 0 {
		o.IdleInTransaction = d.IdleInTransaction
	}
	if o.LongTransaction <= 0 {
		o.LongTransaction = d.LongTransaction
	}
	if o.SessionEvery <= 0 {
		o.SessionEvery = d.SessionEvery
	}
	if o.MaxPerMinute <= 0 {
		o.MaxPerMinute = d.MaxPerMinute
	}
	if o.Repeat <= 0 {
		o.Repeat = d.Repeat
	}
	if o.QueryTimeout <= 0 {
		o.QueryTimeout = d.QueryTimeout
	}
	if o.DeadlockLookback <= 0 {
		o.DeadlockLookback = d.DeadlockLookback
	}
	return o
}

// LockRef is a lock a session waits for, or the row lock it holds.
type LockRef struct {
	LockType      string  `json:"locktype"`
	Mode          string  `json:"mode"`
	Relation      string  `json:"relation,omitempty"`
	RelationOID   uint32  `json:"relation_oid,omitempty"`
	Database      string  `json:"database,omitempty"`
	Page          *int64  `json:"page,omitempty"`
	Tuple         *int64  `json:"tuple,omitempty"`
	TransactionID string  `json:"transactionid,omitempty"`
	WaitSeconds   float64 `json:"wait_seconds,omitempty"`
}

// CTID is the row address of a tuple lock, "(page,tuple)", or "".
func (l *LockRef) CTID() string {
	if l == nil || l.Page == nil || l.Tuple == nil {
		return ""
	}
	return fmt.Sprintf("(%d,%d)", *l.Page, *l.Tuple)
}

// Session is one backend in an incident.
type Session struct {
	PID             int32      `json:"pid"`
	SessionID       string     `json:"session_id,omitempty"`
	Datname         string     `json:"datname,omitempty"`
	Username        string     `json:"usename,omitempty"`
	ApplicationName string     `json:"application_name,omitempty"`
	ClientAddr      string     `json:"client_addr,omitempty"`
	State           string     `json:"state,omitempty"`
	WaitEventType   string     `json:"wait_event_type,omitempty"`
	WaitEvent       string     `json:"wait_event,omitempty"`
	BackendStart    *time.Time `json:"backend_start,omitempty"`
	XactStart       *time.Time `json:"xact_start,omitempty"`
	QueryStart      *time.Time `json:"query_start,omitempty"`
	XactSeconds     float64    `json:"xact_seconds,omitempty"`
	QuerySeconds    float64    `json:"query_seconds,omitempty"`
	StateSeconds    float64    `json:"state_seconds,omitempty"`
	QueryID         string     `json:"query_id,omitempty"`
	Query           string     `json:"query,omitempty"`
	BlockedBy       []int32    `json:"blocked_by,omitempty"`
	Wait            *LockRef   `json:"wait,omitempty"`
	Row             *LockRef   `json:"row,omitempty"`
	HeldRelations   []string   `json:"held_relations,omitempty"`
	XminAge         int64      `json:"xmin_age,omitempty"`

	// writes are the relation locks of RowExclusiveLock or stronger the lock
	// probe saw, named into HeldRelations once other databases are resolved.
	writes []LockRef
}

// Incident is one capture, sent to the server as a fact of kind "incident".
type Incident struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	CapturedAt  time.Time `json:"captured_at"`
	Database    string    `json:"database,omitempty"`
	Relation    string    `json:"relation,omitempty"`
	CTID        string    `json:"ctid,omitempty"`
	LockMode    string    `json:"lock_mode,omitempty"`
	WaitSeconds float64   `json:"wait_seconds,omitempty"`
	Sessions    []Session `json:"sessions"`
	// SessionsOmitted counts the sessions left out past the per-incident cap.
	SessionsOmitted int `json:"sessions_omitted,omitempty"`
	// Suppressed counts the incidents dropped by the rate cap before this one.
	Suppressed int `json:"suppressed_before,omitempty"`
	// Rebuilt marks a deadlock whose cycle the probe did not see: PostgreSQL
	// counted Deadlocks of them in Database and broke them within
	// deadlock_timeout, and Sessions are those seen waiting on a lock, or
	// blocking one, there since Since.
	Rebuilt   bool       `json:"rebuilt,omitempty"`
	Deadlocks int64      `json:"deadlocks,omitempty"`
	Since     *time.Time `json:"since,omitempty"`
}

// Watcher keeps the state of one target. Tick is called from one goroutine.
type Watcher struct {
	q       Querier
	clk     clock.Clock
	opts    Options
	emit    func([]Incident)
	resolve Resolver

	mu        sync.Mutex
	ticks     int
	waitSeen  map[string]bool                  // waiting session keys seen on the last lock probe
	lockSent  map[string]time.Time             // lock incident keys and when they were sent
	cycleSent map[string]bool                  // cycle keys still present
	sessSent  map[string]bool                  // long-query and idle episodes still present
	recent    map[string]map[int32]seenSession // sessions of recent lock probes, by database
	cycleAt   map[string]time.Time             // databases with a captured cycle, and when
	// deadlockBase is the deadlock counters at the previous read, nil when
	// no lock wait was seen within the lookback.
	deadlockBase map[string]int64
	minute       time.Time
	sentMinute   int
	suppressed   int
	lastErr      error
}

// New returns a watcher reading through q; emit receives each batch of
// incidents and must not block.
func New(q Querier, clk clock.Clock, opts Options, emit func([]Incident), resolve Resolver) *Watcher {
	return &Watcher{
		q: q, clk: clk, opts: opts.withDefaults(), emit: emit, resolve: resolve,
		waitSeen: map[string]bool{}, lockSent: map[string]time.Time{},
		cycleSent: map[string]bool{}, sessSent: map[string]bool{},
		recent: map[string]map[int32]seenSession{}, cycleAt: map[string]time.Time{},
	}
}

// LastError is the error of the latest probe, nil when it succeeded.
func (w *Watcher) LastError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastErr
}

// Tick runs after a successful ASH tick. lockWaiting says whether that tick
// saw a session waiting on a lock: only then is pg_locks read.
func (w *Watcher) Tick(ctx context.Context, lockWaiting bool) {
	w.mu.Lock()
	w.ticks++
	sessionsDue := w.ticks%w.opts.SessionEvery == 0
	w.mu.Unlock()

	var out []Incident
	var firstErr error
	if lockWaiting {
		sessions, err := w.probe(ctx, lockGraphQuery)
		if err == nil {
			w.nameOtherDatabases(ctx, sessions)
			heldFromWrites(sessions)
			out = append(out, w.lockIncidents(sessions)...)
			w.remember(w.clk.Now(), sessions)
		} else {
			firstErr = err
		}
	} else {
		w.mu.Lock()
		clear(w.waitSeen)
		clear(w.cycleSent)
		w.mu.Unlock()
	}
	if rebuilt, err := w.deadlockIncidents(ctx); err == nil {
		out = append(out, rebuilt...)
	} else if firstErr == nil {
		firstErr = err
	}
	if sessionsDue {
		sessions, err := w.probe(ctx, sessionsQuery, w.opts.LongQuery.Seconds(), w.opts.IdleInTransaction.Seconds(), w.opts.LongTransaction.Seconds())
		if err == nil {
			out = append(out, w.sessionIncidents(sessions)...)
		} else if firstErr == nil {
			firstErr = err
		}
	}
	w.mu.Lock()
	w.lastErr = firstErr
	w.mu.Unlock()
	if out = w.limit(out); len(out) > 0 && w.emit != nil {
		w.emit(out)
	}
}

// probe runs one query that answers a single JSON array of sessions.
func (w *Watcher) probe(ctx context.Context, sql string, args ...any) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, w.opts.QueryTimeout)
	defer cancel()
	rows, err := w.q.Query(ctx, sql, args...)
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
	var sessions []sessionRow
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &sessions); err != nil {
			return nil, fmt.Errorf("watch: decode probe: %w", err)
		}
	}
	out := make([]Session, 0, len(sessions))
	for _, r := range sessions {
		out = append(out, r.session())
	}
	return out, nil
}

// sessionRow is a session as the probe queries return it.
type sessionRow struct {
	Session
	QueryID  *string   `json:"query_id"`
	QueryCut bool      `json:"query_cut"`
	Writes   []LockRef `json:"writes"`
}

func (r sessionRow) session() Session {
	s := r.Session
	if r.QueryID != nil && *r.QueryID != "0" {
		s.QueryID = *r.QueryID
	}
	// The text reaches the server and the notifications, so a statement that
	// may carry a credential is withheld, and so is one the server cut, since
	// the sign of a credential may lie past the cut.
	if r.QueryCut || redact.MayHoldCredential(s.Query) {
		s.Query = redact.Withheld
	}
	s.Query = truncateUTF8(s.Query, maxQueryBytes)
	s.SessionID = sessionID(s.PID, s.BackendStart)
	s.writes = r.Writes
	return s
}

// nameOtherDatabases names, through the resolver, the relations of locks taken
// in a database other than the watcher's own.
func (w *Watcher) nameOtherDatabases(ctx context.Context, sessions []Session) {
	if w.resolve == nil {
		return
	}
	want := map[string]map[uint32]bool{}
	for _, s := range sessions {
		for _, l := range lockRefs(&s) {
			if l != nil && l.Relation == "" && l.RelationOID != 0 && l.Database != "" {
				if want[l.Database] == nil {
					want[l.Database] = map[uint32]bool{}
				}
				want[l.Database][l.RelationOID] = true
			}
		}
	}
	for db, set := range want {
		oids := make([]uint32, 0, len(set))
		for oid := range set {
			oids = append(oids, oid)
		}
		names := w.resolve(ctx, db, oids)
		for i := range sessions {
			for _, l := range lockRefs(&sessions[i]) {
				if l != nil && l.Relation == "" && l.Database == db {
					l.Relation = names[l.RelationOID]
				}
			}
		}
	}
}

// limit applies the per-minute cap and carries the count of dropped
// incidents on the next one sent.
func (w *Watcher) limit(in []Incident) []Incident {
	if len(in) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.clk.Now()
	if now.Sub(w.minute) >= time.Minute {
		w.minute, w.sentMinute = now, 0
	}
	out := make([]Incident, 0, len(in))
	for _, inc := range in {
		if w.sentMinute >= w.opts.MaxPerMinute {
			w.suppressed++
			continue
		}
		inc.Suppressed, w.suppressed = w.suppressed, 0
		w.sentMinute++
		out = append(out, inc)
	}
	return out
}

func incidentID(kind string, at time.Time, pid int32) string {
	return kind + "-" + strconv.FormatInt(at.UnixMicro(), 16) + "-" + strconv.FormatInt(int64(pid), 10)
}

// sessionID is the identity of a session that survives PID reuse, the same
// as the locks check's: start time in microseconds, hex, a dot, the PID.
func sessionID(pid int32, started *time.Time) string {
	if started == nil {
		return ""
	}
	return strconv.FormatInt(started.UnixMicro(), 16) + "." + strconv.FormatInt(int64(pid), 10)
}

const maxQueryBytes = 2048

func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}
