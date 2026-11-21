package watch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/redact"
	"github.com/stretchr/testify/require"
)

// fakeRows is a cursor over one JSON value, as the probes answer.
type fakeRows struct {
	data    []byte
	done    bool
	scanErr error
	err     error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return pgtype.NewMap() }

func (r *fakeRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dest[0].(*[]byte) = r.data
	return nil
}

// fakeQ answers the lock probe and the sessions probe from canned JSON.
type fakeQ struct {
	mu        sync.Mutex
	locks     []byte
	sessions  []byte
	deadlocks []byte
	err       error
	rowsErr   error
	scanErr   error
	calls     map[string]int
	args      []any
}

func (f *fakeQ) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	name := "sessions"
	data := f.sessions
	switch sql {
	case lockGraphQuery:
		name, data = "locks", f.locks
	case deadlockQuery:
		name, data = "deadlocks", f.deadlocks
	default:
		f.args = args
	}
	f.calls[name]++
	if f.err != nil {
		return nil, f.err
	}
	return &fakeRows{data: data, scanErr: f.scanErr, err: f.rowsErr}, nil
}

func (f *fakeQ) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func asJSON(t *testing.T, sessions ...Session) []byte {
	t.Helper()
	if sessions == nil {
		sessions = []Session{}
	}
	raw, err := json.Marshal(sessions)
	require.NoError(t, err)
	return raw
}

func ptr[T any](v T) *T { return &v }

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

// harness is a watcher over a fake connection and clock, with the incidents
// of each tick collected.
type harness struct {
	q   *fakeQ
	clk *clock.Fake
	w   *Watcher
	got []Incident
}

func newHarness(opts Options, resolve Resolver) *harness {
	h := &harness{q: &fakeQ{}, clk: clock.NewFake(t0)}
	if opts.SessionEvery == 0 {
		opts.SessionEvery = 1 << 20
	}
	h.w = New(h.q, h.clk, opts, func(in []Incident) { h.got = append(h.got, in...) }, resolve)
	return h
}

func (h *harness) tick(lockWaiting bool) []Incident {
	h.got = nil
	h.w.Tick(context.Background(), lockWaiting)
	return h.got
}

// rowWait is a session waiting on the transaction of the session that
// updated a row, which holds the row's tuple lock.
func rowWait(pid, blocker int32, seconds float64) Session {
	return Session{
		PID: pid, Datname: "app", Username: "web", ApplicationName: "shop", State: "active",
		WaitEventType: "Lock", WaitEvent: "transactionid",
		Query:     "UPDATE orders SET status = 'paid' WHERE id = 42",
		BlockedBy: []int32{blocker},
		Wait:      &LockRef{LockType: "transactionid", Mode: "ShareLock", TransactionID: "1234", WaitSeconds: seconds},
		Row:       &LockRef{LockType: "tuple", Mode: "ExclusiveLock", Relation: "public.orders", Page: ptr[int64](3), Tuple: ptr[int64](7)},
	}
}

func holder(pid int32, xact float64) Session {
	return Session{
		PID: pid, Datname: "app", Username: "batch", State: "idle in transaction", XactSeconds: xact,
		Query: "UPDATE orders SET status = 'shipped' WHERE id = 42",
	}
}

func TestOptionsDefaults(t *testing.T) {
	require.Equal(t, DefaultOptions(), Options{}.withDefaults())
	custom := Options{LockWaitMin: time.Second, LongQuery: time.Hour, IdleInTransaction: 2 * time.Minute, LongTransaction: time.Hour, SessionEvery: 3, MaxPerMinute: 5, Repeat: time.Second, QueryTimeout: time.Second, DeadlockLookback: time.Minute}
	require.Equal(t, custom, custom.withDefaults())
}

func TestLockRefCTID(t *testing.T) {
	require.Empty(t, (*LockRef)(nil).CTID())
	require.Empty(t, (&LockRef{Page: ptr[int64](1)}).CTID())
	require.Equal(t, "(1,2)", (&LockRef{Page: ptr[int64](1), Tuple: ptr[int64](2)}).CTID())
}

func TestSessionRowWithholdsAndTrims(t *testing.T) {
	started := t0.Add(-time.Hour)
	base := Session{PID: 9, BackendStart: &started, Query: "SELECT 1"}

	s := sessionRow{Session: base, QueryID: ptr("0")}.session()
	require.Empty(t, s.QueryID, "query id 0 means none")
	require.Equal(t, "SELECT 1", s.Query)
	require.Equal(t, sessionID(9, &started), s.SessionID)
	require.Contains(t, s.SessionID, ".9")

	s = sessionRow{Session: base, QueryID: ptr("-77")}.session()
	require.Equal(t, "-77", s.QueryID)

	cut := sessionRow{Session: base, QueryCut: true}.session()
	require.Equal(t, redact.Withheld, cut.Query)

	cred := base
	cred.Query = "ALTER ROLE app PASSWORD 'x'"
	require.Equal(t, redact.Withheld, sessionRow{Session: cred}.session().Query)

	long := base
	long.Query = strings.Repeat("é", maxQueryBytes)
	got := sessionRow{Session: long}.session().Query
	require.LessOrEqual(t, len(got), maxQueryBytes)
	require.True(t, strings.HasPrefix(long.Query, got))
	require.Equal(t, 0, len(got)%2, "cut on a rune boundary")

	require.Empty(t, sessionID(1, nil))
}

func TestTruncateUTF8(t *testing.T) {
	require.Equal(t, "abc", truncateUTF8("abc", 5))
	require.Equal(t, "ab", truncateUTF8("abc", 2))
	require.Equal(t, "a", truncateUTF8("aé", 2))
}

// A row lock wait past LockWaitMin is sent once, naming the table, the row,
// the lock mode and both sessions, waiter first; the same wait is sent again
// only after Repeat.
func TestLockWaitCapturedOnceThenAfterRepeat(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 30), rowWait(200, 100, 0.8))

	got := h.tick(true)
	require.Len(t, got, 1)
	inc := got[0]
	require.Equal(t, TypeLockWait, inc.Type)
	require.Equal(t, "app", inc.Database)
	require.Equal(t, "public.orders", inc.Relation, "transactionid wait: the table comes from the row lock")
	require.Equal(t, "(3,7)", inc.CTID)
	require.Equal(t, "ShareLock", inc.LockMode)
	require.InDelta(t, 0.8, inc.WaitSeconds, 1e-9)
	require.Equal(t, t0, inc.CapturedAt)
	require.True(t, strings.HasPrefix(inc.ID, "lock_wait-"))
	require.True(t, strings.HasSuffix(inc.ID, "-200"))
	require.Len(t, inc.Sessions, 2)
	require.Equal(t, int32(200), inc.Sessions[0].PID)
	require.Equal(t, int32(100), inc.Sessions[1].PID)
	require.Zero(t, inc.Suppressed)

	h.clk.Advance(30 * time.Second)
	require.Empty(t, h.tick(true))

	h.clk.Advance(31 * time.Second)
	require.Len(t, h.tick(true), 1)
}

// The relation the wait itself names wins over the row lock's.
func TestLockWaitOnRelationNamesIt(t *testing.T) {
	h := newHarness(Options{}, nil)
	w := rowWait(200, 100, 2)
	w.Wait = &LockRef{LockType: "relation", Mode: "AccessExclusiveLock", Relation: "public.items", WaitSeconds: 2}
	w.Row = nil
	h.q.locks = asJSON(t, holder(100, 10), w)
	got := h.tick(true)
	require.Len(t, got, 1)
	require.Equal(t, "public.items", got[0].Relation)
	require.Empty(t, got[0].CTID)
	require.Equal(t, "AccessExclusiveLock", got[0].LockMode)
}

func TestShortLockWaitIsNotCaptured(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 1), rowWait(200, 100, 0.1))
	require.Empty(t, h.tick(true))
}

// Without waitstart (PostgreSQL before 14) a wait counts once two probes in a
// row have seen it.
func TestUnknownWaitCapturedOnSecondProbe(t *testing.T) {
	h := newHarness(Options{}, nil)
	h.q.locks = asJSON(t, holder(100, 1), rowWait(200, 100, 0))
	require.Empty(t, h.tick(true))
	require.Len(t, h.tick(true), 1)

	// A tick with no lock wait forgets what was seen.
	h2 := newHarness(Options{}, nil)
	h2.q.locks = h.q.locks
	require.Empty(t, h2.tick(true))
	require.Empty(t, h2.tick(false))
	require.Equal(t, 1, h2.q.count("locks"), "no lock probe without a lock wait")
	require.Empty(t, h2.tick(true))
}

// A cycle of waits is sent once as a deadlock with every session linked to
// it, and no lock_wait for the same group; once gone and back, it is sent
// again.
func TestDeadlockCycleCaptured(t *testing.T) {
	h := newHarness(Options{}, nil)
	a := rowWait(10, 20, 3)
	b := rowWait(20, 10, 2)
	b.Row = &LockRef{LockType: "tuple", Mode: "ExclusiveLock", Relation: "public.stock", Page: ptr[int64](0), Tuple: ptr[int64](1)}
	c := rowWait(30, 10, 1)
	h.q.locks = asJSON(t, a, b, c)

	got := h.tick(true)
	require.Len(t, got, 1)
	inc := got[0]
	require.Equal(t, TypeDeadlock, inc.Type)
	require.Len(t, inc.Sessions, 3)
	require.Equal(t, []int32{10, 20, 30}, []int32{inc.Sessions[0].PID, inc.Sessions[1].PID, inc.Sessions[2].PID}, "longest wait first")
	require.Equal(t, "public.orders", inc.Relation)

	require.Empty(t, h.tick(true), "a cycle still present is not sent again")
	require.Empty(t, h.tick(false))
	require.Len(t, h.tick(true), 1)
}

func TestFindCyclesFromLowestPID(t *testing.T) {
	sessions := []Session{
		{PID: 7, BlockedBy: []int32{3}},
		{PID: 3, BlockedBy: []int32{5}},
		{PID: 5, BlockedBy: []int32{7}},
		{PID: 9, BlockedBy: []int32{3, 99}},
		{PID: 11},
	}
	byPID := map[int32]int{}
	for i, s := range sessions {
		byPID[s.PID] = i
	}
	require.Equal(t, [][]int32{{3, 5, 7}}, findCycles(sessions, byPID))
	require.Equal(t, []int32{3, 5, 7, 9}, component(sessions, byPID, 9))
	require.Equal(t, []int32{11}, component(sessions, byPID, 11))
	require.Equal(t, []int32{3, 5, 7}, roots(sessions, byPID, []int32{3, 5, 7}), "a group with no root keys on every member")
	require.Equal(t, "pid3,pid5", groupKey(sessions, byPID, []int32{5, 3}))
}

func TestIncidentCapsSessions(t *testing.T) {
	h := newHarness(Options{}, nil)
	sessions := []Session{holder(1, 100)}
	for pid := int32(2); pid <= 26; pid++ {
		sessions = append(sessions, rowWait(pid, 1, float64(pid)))
	}
	h.q.locks = asJSON(t, sessions...)
	got := h.tick(true)
	require.Len(t, got, 1)
	require.Len(t, got[0].Sessions, maxIncidentSessions)
	require.Equal(t, 6, got[0].SessionsOmitted)
	require.Equal(t, int32(26), got[0].Sessions[0].PID)
	require.InDelta(t, 26, got[0].WaitSeconds, 1e-9)
}

// A lock taken in another database is named through the resolver.
func TestOtherDatabaseRelationResolved(t *testing.T) {
	var asked []string
	resolve := func(_ context.Context, db string, oids []uint32) map[uint32]string {
		asked = append(asked, db)
		require.Equal(t, []uint32{4242}, oids)
		return map[uint32]string{4242: "sales.invoices"}
	}
	h := newHarness(Options{}, resolve)
	w := rowWait(200, 100, 1)
	w.Datname = "erp"
	w.Wait = &LockRef{LockType: "relation", Mode: "RowExclusiveLock", RelationOID: 4242, Database: "erp", WaitSeconds: 1}
	w.Row = nil
	h.q.locks = asJSON(t, holder(100, 5), w)
	got := h.tick(true)
	require.Equal(t, []string{"erp"}, asked)
	require.Len(t, got, 1)
	require.Equal(t, "sales.invoices", got[0].Relation)
	require.Equal(t, "erp", got[0].Database)
}

func TestRateCapCarriesSuppressedCount(t *testing.T) {
	h := newHarness(Options{MaxPerMinute: 1}, nil)
	h.q.locks = asJSON(t,
		holder(1, 9), rowWait(2, 1, 1),
		holder(3, 9), rowWait(4, 3, 1),
		holder(5, 9), rowWait(6, 5, 1),
	)
	got := h.tick(true)
	require.Len(t, got, 1)
	require.Zero(t, got[0].Suppressed)

	h.clk.Advance(61 * time.Second)
	got = h.tick(true)
	require.Len(t, got, 1)
	require.Equal(t, 2, got[0].Suppressed, "the incidents dropped before ride on the next one")
}

// Long statements and idle transactions are probed every SessionEvery ticks
// and sent once per episode, with the tables they hold.
func TestSessionIncidents(t *testing.T) {
	h := newHarness(Options{SessionEvery: 2, LongQuery: time.Minute, IdleInTransaction: 30 * time.Second}, nil)
	xs := t0.Add(-10 * time.Minute)
	qs := t0.Add(-5 * time.Minute)
	held := []string{}
	for i := 0; i < 12; i++ {
		held = append(held, "public.t"+itoa(int64(i)))
	}
	idle := Session{PID: 50, Datname: "app", State: "idle in transaction", XactStart: &xs, QueryStart: &qs, StateSeconds: 300, HeldRelations: held, XminAge: 1000}
	long := Session{PID: 60, Datname: "app", State: "active", XactStart: &qs, QueryStart: &qs, QuerySeconds: 300, Query: "SELECT pg_sleep(600)"}
	xs2 := t0.Add(-20 * time.Minute)
	qs2 := t0.Add(-time.Second)
	busy := Session{PID: 70, Datname: "app", State: "active", XactStart: &xs2, QueryStart: &qs2, QuerySeconds: 1, XactSeconds: 1200, Query: "UPDATE t SET a = 1"}
	pause := Session{PID: 80, Datname: "app", State: "idle in transaction", XactStart: &xs2, StateSeconds: 5, XactSeconds: 1200}
	h.q.sessions = asJSON(t, idle, long, busy, pause)

	require.Empty(t, h.tick(false))
	require.Zero(t, h.q.count("sessions"), "probed every second tick only")

	got := h.tick(false)
	require.Equal(t, []any{60.0, 30.0, 600.0}, h.q.args)
	require.Len(t, got, 4)
	require.Equal(t, TypeIdleInXact, got[0].Type)
	require.Equal(t, "public.t0", got[0].Relation)
	require.Len(t, got[0].Sessions[0].HeldRelations, maxHeldRelations)
	require.Equal(t, TypeLongQuery, got[1].Type)
	require.Empty(t, got[1].Relation)
	require.Equal(t, "app", got[1].Database)
	require.Equal(t, TypeLongXact, got[2].Type)
	require.Equal(t, TypeLongXact, got[3].Type, "a short pause inside a long transaction is not an idle one")

	h.tick(false)
	require.Empty(t, h.tick(false), "same episodes are not sent again")

	next := t0.Add(-2 * time.Minute)
	long.QueryStart = &next
	busy.QueryStart = &next
	h.q.sessions = asJSON(t, idle, long, busy, pause)
	h.tick(false)
	got = h.tick(false)
	require.Len(t, got, 1, "a new statement of the same session is a new episode")
	require.Equal(t, TypeLongQuery, got[0].Type)
}

func TestProbeErrors(t *testing.T) {
	h := newHarness(Options{SessionEvery: 1}, nil)
	h.q.err = errors.New("conn busy")
	require.Empty(t, h.tick(true))
	require.ErrorContains(t, h.w.LastError(), "conn busy")

	h.q.err = nil
	h.q.scanErr = errors.New("scan")
	h.tick(false)
	require.ErrorContains(t, h.w.LastError(), "scan")

	h.q.scanErr = nil
	h.q.rowsErr = errors.New("rows")
	h.tick(false)
	require.ErrorContains(t, h.w.LastError(), "rows")

	h.q.rowsErr = nil
	h.q.sessions = []byte(`{"not":"an array"}`)
	h.tick(false)
	require.ErrorContains(t, h.w.LastError(), "decode probe")

	h.q.sessions = nil
	h.tick(false)
	require.NoError(t, h.w.LastError())
}

func TestWaitKeyAndSessKey(t *testing.T) {
	s := rowWait(5, 1, 1)
	require.Equal(t, "pid5|transactionid|ShareLock|||1234", waitKey(s))
	s.Wait = nil
	require.Equal(t, "pid5", waitKey(s))

	h := newHarness(Options{}, nil)
	at := time.UnixMicro(0x10)
	idle := Session{PID: 5, SessionID: "a.5", XactStart: &at}
	require.Equal(t, "idle_in_transaction|a.5|10", h.w.sessKey(TypeIdleInXact, idle))
	require.Equal(t, "long_query|pid7|", h.w.sessKey(TypeLongQuery, Session{PID: 7}))
}
