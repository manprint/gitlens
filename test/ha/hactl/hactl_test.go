package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOps struct {
	mu       sync.Mutex
	probes   map[string]probe
	promoted []string
	slots    []string
	repoints []string
	detached []string
	gate     chan struct{}
	entered  chan struct{}
	// afterRead runs after a probe result is read, before it is returned.
	afterRead func(name string)
	onPromote func(n nodeConfig)
}

func newFakeOps(p map[string]probe) *fakeOps { return &fakeOps{probes: p} }

func (f *fakeOps) set(name string, p probe) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes[name] = p
}

func (f *fakeOps) Probe(_ context.Context, n nodeConfig) (probe, error) {
	f.mu.Lock()
	p := f.probes[n.Name]
	f.mu.Unlock()
	if f.afterRead != nil {
		f.afterRead(n.Name)
	}
	if !p.Reachable {
		return probe{}, errors.New("connection refused")
	}
	return p, nil
}

func (f *fakeOps) Detach(_ context.Context, s nodeConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detached = append(f.detached, s.Name)
	p := f.probes[s.Name]
	p.Upstream = ""
	f.probes[s.Name] = p
	return nil
}

func (f *fakeOps) Promote(_ context.Context, n nodeConfig) error {
	if f.onPromote != nil {
		f.onPromote(n)
	}
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promoted = append(f.promoted, n.Name)
	p := f.probes[n.Name]
	p.InRecovery = false
	p.Timeline++
	p.Upstream = ""
	f.probes[n.Name] = p
	return nil
}

func (f *fakeOps) EnsureSlot(_ context.Context, on nodeConfig, slot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.slots = append(f.slots, on.Name+":"+slot)
	return nil
}

func (f *fakeOps) Repoint(_ context.Context, s, l nodeConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repoints = append(f.repoints, s.Name+"->"+l.Name)
	p := f.probes[s.Name]
	p.Upstream = l.Name
	f.probes[s.Name] = p
	return nil
}

func (f *fakeOps) promotedCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.promoted...)
}

type memStore struct {
	mu    sync.Mutex
	saved []persisted
}

func (m *memStore) Save(p persisted) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.Promoting != nil {
		c := *p.Promoting
		p.Promoting = &c
	}
	m.saved = append(m.saved, p)
	return nil
}

var testNodes = []nodeConfig{
	{Name: "odoo-db", Addr: "odoo-db:5432", Slot: "ha_1"},
	{Name: "pg-ha-2", Addr: "pg-ha-2:5432", Slot: "ha_2"},
	{Name: "pg-ha-3", Addr: "pg-ha-3:5432", Slot: "ha_3"},
}

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func healthy() map[string]probe {
	return map[string]probe{
		"odoo-db": {Reachable: true, LSN: 1000, ReceiveLSN: 1000, Timeline: 1},
		"pg-ha-2": {Reachable: true, InRecovery: true, LSN: 900, ReceiveLSN: 950, Timeline: 1, Upstream: "odoo-db"},
		"pg-ha-3": {Reachable: true, InRecovery: true, LSN: 800, ReceiveLSN: 990, Timeline: 1, Upstream: "odoo-db"},
	}
}

func newTestController(ops pgOps, st persisted) (*controller, *memStore) {
	ms := &memStore{}
	clock := t0
	c := newController(testNodes, ops, ms, st, func() time.Time { clock = clock.Add(time.Second); return clock }, func(string, ...any) {})
	return c, ms
}

func view(name string, p probe) nodeView {
	return nodeView{nodeConfig: nodeConfig{Name: name}, probe: p}
}

func TestHACTL001_OneWritableNodeIsLeader(t *testing.T) {
	h := healthy()
	nodes := []nodeView{view("odoo-db", h["odoo-db"]), view("pg-ha-2", h["pg-ha-2"]), view("pg-ha-3", h["pg-ha-3"])}
	leader, split := electLeader(nodes, 1)
	assert.Equal(t, "odoo-db", leader)
	assert.False(t, split)
}

func TestHACTL002_TwoWritableNodesAreSplitBrain(t *testing.T) {
	nodes := []nodeView{
		view("odoo-db", probe{Reachable: true, Timeline: 2}),
		view("pg-ha-2", probe{Reachable: true, Timeline: 2}),
	}
	leader, split := electLeader(nodes, 2)
	assert.Equal(t, "", leader)
	assert.True(t, split)
}

func TestHACTL003_CandidateIsHighestReceiveLSNTieLowestName(t *testing.T) {
	h := healthy()
	nodes := []nodeView{view("odoo-db", probe{}), view("pg-ha-2", h["pg-ha-2"]), view("pg-ha-3", h["pg-ha-3"])}
	assert.Equal(t, "pg-ha-3", chooseCandidate(nodes, 1), "receive LSN ranks, not replay LSN")
	nodes[1].ReceiveLSN = 990
	assert.Equal(t, "pg-ha-2", chooseCandidate(nodes, 1), "tie goes to the lowest name")
	nodes[1].Timeline = 0
	assert.Equal(t, "pg-ha-3", chooseCandidate(nodes, 1), "a standby off maxTimeline is not a candidate")
}

func TestHACTL004_NoReachableStandbyNoCandidate(t *testing.T) {
	nodes := []nodeView{view("odoo-db", probe{}), view("pg-ha-2", probe{}), view("pg-ha-3", probe{Reachable: true, Timeline: 1})}
	assert.Equal(t, "", chooseCandidate(nodes, 1))

	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	c.tick(context.Background())
	ops.set("odoo-db", probe{})
	ops.set("pg-ha-2", probe{})
	ops.set("pg-ha-3", probe{})
	for range 6 {
		c.tick(context.Background())
	}
	assert.Empty(t, ops.promotedCopy())
	assert.Equal(t, "no_candidate", c.status().Failover)
}

func TestHACTL005_TrackerNeedsFiveConsecutiveFailures(t *testing.T) {
	f := failureTracker{threshold: 5, now: func() time.Time { return t0 }}
	for range 4 {
		assert.False(t, f.Fail())
	}
	f.OK()
	for range 4 {
		assert.False(t, f.Fail())
	}
	assert.True(t, f.Fail())
	assert.Equal(t, t0, f.since)
}

func TestHACTL006_PromoteNotRepeatedWhileInFlight(t *testing.T) {
	ops := newFakeOps(healthy())
	ops.gate = make(chan struct{})
	ops.entered = make(chan struct{}, 1)
	c, ms := newTestController(ops, persisted{})
	var savedFirst atomic.Bool
	ops.onPromote = func(n nodeConfig) {
		ms.mu.Lock()
		defer ms.mu.Unlock()
		last := ms.saved[len(ms.saved)-1]
		savedFirst.Store(last.Promoting != nil && last.Promoting.Node == n.Name)
	}
	ctx := context.Background()
	c.tick(ctx)
	ops.set("odoo-db", probe{})
	for range 4 {
		c.tick(ctx)
	}
	assert.Equal(t, "counting", c.status().Failover)
	done := make(chan struct{})
	go func() { c.tick(ctx); close(done) }()
	<-ops.entered
	assert.ErrorIs(t, c.promote(ctx, "pg-ha-2"), errPromotionBusy)
	st := c.status()
	require.NotNil(t, st.Promoting)
	assert.Equal(t, "pg-ha-3", st.Promoting.Node)
	assert.Equal(t, 1, st.Promoting.FromTimeline)
	close(ops.gate)
	<-done
	for range 10 {
		c.tick(ctx)
	}
	assert.Equal(t, []string{"pg-ha-3"}, ops.promotedCopy())
	st = c.status()
	assert.Equal(t, "pg-ha-3", st.Leader)
	assert.Equal(t, 2, st.MaxTimeline)
	assert.Nil(t, st.Promoting)
	assert.Contains(t, ops.slots, "pg-ha-3:ha_2", "slot for the other standby before promotion")
	assert.Contains(t, ops.repoints, "pg-ha-2->pg-ha-3")
	first := ms.saved[0]
	for _, s := range ms.saved {
		if s.Promoting != nil {
			first = s
			break
		}
	}
	assert.Equal(t, "pg-ha-3", first.Promoting.Node, "promoting persisted before pg_promote")
	assert.True(t, savedFirst.Load(), "the state saved last before pg_promote records promoting of that node")
	assert.Contains(t, ops.detached, "pg-ha-2", "the other standby's WAL receiver is stopped before pg_promote")
}

func TestHACTL006b_NoPromotionUnderSplitBrainOrPause(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	ops.set("pg-ha-2", probe{Reachable: true, Timeline: 1, LSN: 5})
	for range 8 {
		c.tick(ctx)
	}
	st := c.status()
	assert.True(t, st.SplitBrain)
	assert.Equal(t, "", st.Leader)
	assert.Empty(t, ops.promotedCopy())
	_, ok := c.routeRW()
	assert.False(t, ok, "writes refused under split brain")

	ops2 := newFakeOps(healthy())
	c2, _ := newTestController(ops2, persisted{})
	c2.tick(ctx)
	require.NoError(t, c2.setFailoverPaused(true))
	ops2.set("odoo-db", probe{})
	for range 8 {
		c2.tick(ctx)
	}
	assert.Empty(t, ops2.promotedCopy())
	assert.Equal(t, "paused", c2.status().Failover)
	require.NoError(t, c2.setFailoverPaused(false))
	c2.tick(ctx)
	assert.Equal(t, []string{"pg-ha-3"}, ops2.promotedCopy())
}

func TestHACTL007_StatusJSONGolden(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	c.tick(context.Background())
	require.NoError(t, c.setWritesBlocked(true))
	srv := httptest.NewServer(c.handler(context.Background()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/status")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	want := `{"leader":"odoo-db","timeline":1,"max_timeline":1,"writes_blocked":true,"failover_paused":false,"promoting":null,"split_brain":false,"since":"2026-11-06T12:00:01Z","failover":"idle","last_error":"","nodes":[` +
		`{"name":"odoo-db","role":"primary","healthy":true,"isolated":false,"upstream":"","timeline":1,"lag_bytes":null,"lsn":"0/3E8","receive_lsn":"0/3E8"},` +
		`{"name":"pg-ha-2","role":"standby","healthy":true,"isolated":false,"upstream":"odoo-db","timeline":1,"lag_bytes":100,"lsn":"0/384","receive_lsn":"0/3B6"},` +
		`{"name":"pg-ha-3","role":"standby","healthy":true,"isolated":false,"upstream":"odoo-db","timeline":1,"lag_bytes":200,"lsn":"0/320","receive_lsn":"0/3DE"}]}`
	assert.JSONEq(t, want, string(b))

	resp2, err := http.Post(srv.URL+"/status", "", nil)
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp2.StatusCode)
}

func echoServer(t *testing.T, tag string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					_, _ = fmt.Fprintf(c, "%s:%s", tag, line)
				}
			}(c)
		}
	}()
	return l.Addr().String()
}

func startProxy(t *testing.T, p *proxy) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = p.serve(ctx, l) }()
	return l.Addr().String()
}

func roundTrip(t *testing.T, addr string) (string, net.Conn) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(c, "ping\n"); err != nil {
		return "", c
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "", c
	}
	return line, c
}

func TestHACTL008_ProxyRoutesToLeaderAndDropsDemoted(t *testing.T) {
	a, b := echoServer(t, "A"), echoServer(t, "B")
	var mu sync.Mutex
	target := a
	p := newProxy(func() (string, bool) { mu.Lock(); defer mu.Unlock(); return target, true })
	addr := startProxy(t, p)
	got, conn := roundTrip(t, addr)
	assert.Equal(t, "A:ping\n", got)
	require.Eventually(t, func() bool { return p.open() == 1 }, time.Second, 10*time.Millisecond)

	mu.Lock()
	target = b
	mu.Unlock()
	p.closeAll()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Read(make([]byte, 1))
	assert.Error(t, err, "connection to the demoted node is closed")
	_ = conn.Close()
	got, conn2 := roundTrip(t, addr)
	_ = conn2.Close()
	assert.Equal(t, "B:ping\n", got)
}

func TestHACTL009_ReadOnlyRoundRobinAndFallback(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	c.tick(context.Background())
	seen := map[string]int{}
	for range 4 {
		addr, ok := c.routeRO()
		require.True(t, ok)
		seen[addr]++
	}
	assert.Equal(t, map[string]int{"pg-ha-2:5432": 2, "pg-ha-3:5432": 2}, seen)
	ops.set("pg-ha-2", probe{})
	ops.set("pg-ha-3", probe{})
	c.tick(context.Background())
	addr, ok := c.routeRO()
	assert.True(t, ok)
	assert.Equal(t, "odoo-db:5432", addr, "falls back to the primary")
}

func TestHACTL010_RecoveredOldPrimaryIsDivergedNeverRouted(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	ops.set("odoo-db", probe{})
	for range 6 {
		c.tick(ctx)
	}
	require.Equal(t, "pg-ha-3", c.status().Leader)
	ops.set("odoo-db", probe{Reachable: true, Timeline: 1, LSN: 2000, ReceiveLSN: 2000})
	for range 3 {
		c.tick(ctx)
	}
	st := c.status()
	assert.Equal(t, "pg-ha-3", st.Leader)
	assert.False(t, st.SplitBrain)
	assert.Equal(t, "diverged_or_stale", st.Nodes[0].Role)
	for range 6 {
		addr, ok := c.routeRW()
		require.True(t, ok)
		assert.Equal(t, "pg-ha-3:5432", addr)
		addr, _ = c.routeRO()
		assert.NotEqual(t, "odoo-db:5432", addr)
	}
	assert.NotContains(t, ops.repoints, "odoo-db->pg-ha-3", "never auto-rejoined")
}

func TestHACTL011_MaxTimelineSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hactl.json")
	store := stateStore{path: path}
	require.NoError(t, store.Save(persisted{LastLeader: "pg-ha-3", MaxTimeline: 2}))
	st, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, 2, st.MaxTimeline)

	ops := newFakeOps(map[string]probe{
		"odoo-db": {Reachable: true, Timeline: 1, LSN: 5000},
		"pg-ha-2": {},
		"pg-ha-3": {},
	})
	c := newController(testNodes, ops, store, st, func() time.Time { return t0 }, func(string, ...any) {})
	for range 6 {
		c.tick(context.Background())
	}
	s := c.status()
	assert.Equal(t, "", s.Leader, "a writable node below maxTimeline is never the leader")
	assert.Equal(t, "diverged_or_stale", s.Nodes[0].Role)
	_, ok := c.routeRW()
	assert.False(t, ok)
	assert.Empty(t, ops.promotedCopy())

	bad := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, stateStore{path: bad}.Save(persisted{}))
	require.NoError(t, writeFile(bad, "{"))
	_, err = stateStore{path: bad}.Load()
	assert.Error(t, err, "a corrupt state file refuses to start")
	st, err = stateStore{path: filepath.Join(t.TempDir(), "none.json")}.Load()
	require.NoError(t, err)
	assert.Equal(t, persisted{}, st)
}

func TestHACTL012_BlockWritesDropsAndRefusesUnblockRestores(t *testing.T) {
	a := echoServer(t, "A")
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	c.nodes = []nodeConfig{{Name: "odoo-db", Addr: a, Slot: "ha_1"}, testNodes[1], testNodes[2]}
	c.tick(context.Background())
	rw := newProxy(c.routeRW)
	c.onRWChange = rw.closeAll
	addr := startProxy(t, rw)
	got, conn := roundTrip(t, addr)
	require.Equal(t, "A:ping\n", got)

	require.NoError(t, c.setWritesBlocked(true))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Read(make([]byte, 1))
	assert.Error(t, err, "existing RW connection dropped")
	_ = conn.Close()
	got, conn2 := roundTrip(t, addr)
	_ = conn2.Close()
	assert.Equal(t, "", got, "new RW connection refused")

	require.NoError(t, c.setWritesBlocked(false))
	got, conn3 := roundTrip(t, addr)
	_ = conn3.Close()
	assert.Equal(t, "A:ping\n", got)
}

func TestHACTL013_SwitchoverPreconditionsWaitAndAbort(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	assert.ErrorIs(t, c.switchover(ctx, "pg-ha-2", 0, time.Second), errPrecondition)

	require.NoError(t, c.setFailoverPaused(true))
	require.NoError(t, c.setWritesBlocked(true))
	ops.set("odoo-db", probe{})
	c.tick(ctx)
	err := c.switchover(ctx, "pg-ha-2", 1000, 300*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aborted")
	assert.Empty(t, ops.promotedCopy(), "no promotion when the target lacks the final LSN")
	st := c.status()
	assert.True(t, st.FailoverPaused)
	assert.True(t, st.WritesBlocked)

	ops.set("pg-ha-2", probe{Reachable: true, InRecovery: true, LSN: 1000, ReceiveLSN: 1000, Timeline: 1, Upstream: "odoo-db"})
	ops.set("pg-ha-3", probe{Reachable: true, InRecovery: true, LSN: 1000, ReceiveLSN: 1000, Timeline: 1, Upstream: "odoo-db"})
	c.tick(ctx)
	require.NoError(t, c.switchover(ctx, "pg-ha-2", 1000, time.Second))
	assert.Equal(t, []string{"pg-ha-2"}, ops.promotedCopy())
	assert.Equal(t, "pg-ha-2", c.status().Leader)
	assert.Contains(t, ops.repoints, "pg-ha-3->pg-ha-2")
}

func TestHACTL014_PromotingResumesOnSameNodeOnly(t *testing.T) {
	h := healthy()
	h["odoo-db"] = probe{}
	ops := newFakeOps(h)
	c, _ := newTestController(ops, persisted{LastLeader: "odoo-db", MaxTimeline: 1,
		Promoting: &promotingState{Node: "pg-ha-2", TS: t0, FromTimeline: 1}})
	c.tick(context.Background())
	assert.Equal(t, []string{"pg-ha-2"}, ops.promotedCopy(), "the recorded node, not the best candidate")
	assert.Equal(t, "pg-ha-2", c.status().Leader)

	h2 := healthy()
	h2["odoo-db"] = probe{}
	h2["pg-ha-2"] = probe{Reachable: true, Timeline: 2, LSN: 3000}
	ops2 := newFakeOps(h2)
	c2, _ := newTestController(ops2, persisted{LastLeader: "odoo-db", MaxTimeline: 2,
		Promoting: &promotingState{Node: "pg-ha-2", TS: t0, FromTimeline: 1}})
	c2.tick(context.Background())
	assert.Empty(t, ops2.promotedCopy(), "already promoted before the crash: finish, no second pg_promote")
	st := c2.status()
	assert.Nil(t, st.Promoting)
	assert.Equal(t, "pg-ha-2", st.Leader)

	h3 := healthy()
	h3["odoo-db"] = probe{}
	h3["pg-ha-2"] = probe{}
	ops3 := newFakeOps(h3)
	c3, _ := newTestController(ops3, persisted{LastLeader: "odoo-db", MaxTimeline: 1,
		Promoting: &promotingState{Node: "pg-ha-2", TS: t0, FromTimeline: 1}})
	for range 6 {
		c3.tick(context.Background())
	}
	assert.Empty(t, ops3.promotedCopy(), "never another candidate while promoting is set")
	assert.Error(t, c3.clearPromoting(), "clear needs failover paused")
	require.NoError(t, c3.setFailoverPaused(true))
	require.NoError(t, c3.clearPromoting())
	assert.Nil(t, c3.status().Promoting)
}

func TestHACTL015_ConninfoQuotingRoundTrip(t *testing.T) {
	ci := primaryConninfo("pg-ha-2", 5432, "replicator", `p a'ss\w`, "odoo-db")
	assert.Equal(t, "pg-ha-2", conninfoValue(ci, "host"))
	assert.Equal(t, `p a'ss\w`, conninfoValue(ci, "password"))
	assert.Equal(t, "odoo-db", conninfoValue(ci, "application_name"))
	assert.Equal(t, "odoo-db", conninfoValue("user=replicator passfile='/x y' host=odoo-db port=5432", "host"))
	assert.Equal(t, "", conninfoValue("", "host"))
	assert.Equal(t, `'it''s'`, quoteLiteral("it's"))
	v, err := parseLSN("1/3E8")
	require.NoError(t, err)
	assert.Equal(t, uint64(1)<<32|1000, v)
	assert.Equal(t, "1/3E8", formatLSN(v))
}

func TestHACTL016_RepointLoopFollowsLeaderAndEnsuresSlots(t *testing.T) {
	h := healthy()
	h["pg-ha-2"] = probe{Reachable: true, InRecovery: true, LSN: 900, ReceiveLSN: 900, Timeline: 1, Upstream: "pg-ha-3"}
	ops := newFakeOps(h)
	c, _ := newTestController(ops, persisted{})
	c.repointEvery = time.Hour
	c.tick(context.Background())
	assert.Equal(t, []string{"pg-ha-2->odoo-db"}, ops.repoints)
	assert.ElementsMatch(t, []string{"odoo-db:ha_2", "odoo-db:ha_3"}, ops.slots)
	c.tick(context.Background())
	assert.Len(t, ops.repoints, 1, "rate-limited")
}

func TestHACTL017_IsolatedNodeCountsAsFailed(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	require.NoError(t, c.setIsolated("odoo-db", true))
	for range 6 {
		c.tick(ctx)
	}
	st := c.status()
	assert.Equal(t, "pg-ha-3", st.Leader)
	assert.Equal(t, "unreachable", st.Nodes[0].Role)
	assert.True(t, st.Nodes[0].Isolated)
	require.NoError(t, c.setIsolated("", false))
	c.tick(ctx)
	assert.Equal(t, "diverged_or_stale", c.status().Nodes[0].Role)
	assert.Error(t, c.setIsolated("nope", true))

	b, err := json.Marshal(c.status())
	require.NoError(t, err)
	assert.NotContains(t, string(b), "password")
}

func TestHACTL018_StaleWritableTargetIsNotPromoted(t *testing.T) {
	h := healthy()
	h["odoo-db"] = probe{}
	h["pg-ha-2"] = probe{Reachable: true, Timeline: 1, LSN: 3000, ReceiveLSN: 3000}
	ops := newFakeOps(h)
	c, _ := newTestController(ops, persisted{LastLeader: "odoo-db", MaxTimeline: 1,
		Promoting: &promotingState{Node: "pg-ha-2", TS: t0, FromTimeline: 1}})
	c.tick(context.Background())
	assert.Empty(t, ops.promotedCopy())
	st := c.status()
	require.NotNil(t, st.Promoting, "promoting stays set")
	assert.Contains(t, st.LastError, "stale")
	_, ok := c.routeRW()
	assert.False(t, ok, "no writes while promoting is set")
}

func TestHACTL019_OtherStandbyReceivedMoreAbortsFreshPromotion(t *testing.T) {
	ops := newFakeOps(healthy())
	c, ms := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	err := c.promote(ctx, "pg-ha-2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pg-ha-3 received more WAL")
	assert.Empty(t, ops.promotedCopy())
	assert.Contains(t, ops.detached, "pg-ha-3", "detached before the comparison")
	assert.Nil(t, c.status().Promoting, "a fresh promotion is cleared")
	assert.Nil(t, ms.saved[len(ms.saved)-1].Promoting, "and the clearing is persisted")
}

func TestHACTL020_SwitchoverFinalLSNIsAtLeastTheLeaderLSN(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	require.NoError(t, c.setFailoverPaused(true))
	require.NoError(t, c.setWritesBlocked(true))
	err := c.switchover(ctx, "pg-ha-3", 0, 300*time.Millisecond)
	require.Error(t, err, "pg-ha-3 received 990 < leader LSN 1000 even when the caller passes 0")
	assert.Contains(t, err.Error(), "aborted")
	assert.Empty(t, ops.promotedCopy())
}

func TestHACTL021_ProxyDropsConnectionRoutedBeforeCloseAll(t *testing.T) {
	a := echoServer(t, "A")
	var p *proxy
	var calls atomic.Int32
	p = newProxy(func() (string, bool) {
		if calls.Add(1) == 1 {
			p.closeAll()
		}
		return a, true
	})
	addr := startProxy(t, p)
	got, conn := roundTrip(t, addr)
	_ = conn.Close()
	assert.Equal(t, "", got, "a route decided before closeAll is dropped after the dial")
	got, conn = roundTrip(t, addr)
	_ = conn.Close()
	assert.Equal(t, "A:ping\n", got)
}

func TestHACTL022_TickDiscardsProbesOlderThanAPromotion(t *testing.T) {
	h := healthy()
	h["pg-ha-3"] = probe{Reachable: true, InRecovery: true, LSN: 1000, ReceiveLSN: 1000, Timeline: 1, Upstream: "odoo-db"}
	ops := newFakeOps(h)
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	require.NoError(t, c.setFailoverPaused(true))
	require.NoError(t, c.setWritesBlocked(true))
	ops.set("odoo-db", probe{})
	var rwChanges atomic.Int32
	c.onRWChange = func() { rwChanges.Add(1) }
	var fired atomic.Bool
	var swErr error
	ops.afterRead = func(name string) {
		if name == "pg-ha-3" && fired.CompareAndSwap(false, true) {
			swErr = c.switchover(ctx, "pg-ha-3", 1000, time.Second)
		}
	}
	c.tick(ctx)
	require.NoError(t, swErr)
	assert.Equal(t, "pg-ha-3", c.status().Leader, "the stale probe of pg-ha-3 (still a standby) does not undo the promotion")
	assert.Equal(t, int32(1), rwChanges.Load(), "RW connections dropped once, by the promotion")
}

func TestHACTL023_OneFailedProbeKeepsTheLeader(t *testing.T) {
	ops := newFakeOps(healthy())
	c, _ := newTestController(ops, persisted{})
	ctx := context.Background()
	c.tick(ctx)
	var rwChanges atomic.Int32
	c.onRWChange = func() { rwChanges.Add(1) }
	ops.set("odoo-db", probe{})
	c.tick(ctx)
	ops.set("odoo-db", healthy()["odoo-db"])
	c.tick(ctx)
	assert.Equal(t, "odoo-db", c.status().Leader)
	assert.Equal(t, int32(0), rwChanges.Load(), "a transient probe failure drops no RW connection")

	ops.set("odoo-db", probe{})
	for range 4 {
		c.tick(ctx)
		addr, ok := c.routeRW()
		require.True(t, ok)
		assert.Equal(t, "odoo-db:5432", addr)
	}
	assert.Equal(t, "counting", c.status().Failover)
	c.tick(ctx)
	assert.Equal(t, "pg-ha-3", c.status().Leader, "the threshold still triggers the failover")
}

func TestHACTL024_StandbyTimelineSurvivesRestart(t *testing.T) {
	store := stateStore{path: filepath.Join(t.TempDir(), "hactl.json")}
	require.NoError(t, store.Save(persisted{LastLeader: "pg-ha-3", MaxTimeline: 2, StandbyTL: map[string]int{"pg-ha-2": 2}}))
	st, err := store.Load()
	require.NoError(t, err)
	ops := newFakeOps(map[string]probe{
		"odoo-db": {},
		"pg-ha-2": {Reachable: true, InRecovery: true, LSN: 900, ReceiveLSN: 900, Timeline: 1, Upstream: "pg-ha-3"},
		"pg-ha-3": {Reachable: true, Timeline: 2, LSN: 1000, ReceiveLSN: 1000},
	})
	c := newController(testNodes, ops, store, st, func() time.Time { return t0 }, func(string, ...any) {})
	c.tick(context.Background())
	s := c.status()
	assert.Equal(t, "standby", s.Nodes[1].Role)
	assert.Equal(t, 2, s.Nodes[1].Timeline, "checkpoint timeline 1 does not hide the followed timeline 2")
	addr, ok := c.routeRO()
	require.True(t, ok)
	assert.Equal(t, "pg-ha-2:5432", addr)
}
