package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type nodeView struct {
	nodeConfig
	probe
	Isolated bool
	Err      string
}

// electLeader: the leader is the only writable node on maxTimeline. Two or
// more such nodes are a split brain: no leader, writes refused.
func electLeader(nodes []nodeView, maxTimeline int) (leader string, splitBrain bool) {
	var writable []string
	for _, n := range nodes {
		if n.Reachable && !n.InRecovery && n.Timeline == maxTimeline {
			writable = append(writable, n.Name)
		}
	}
	switch len(writable) {
	case 0:
		return "", false
	case 1:
		return writable[0], false
	}
	return "", true
}

// chooseCandidate ranks standbys by receive LSN: pg_promote replays all WAL
// already received, so that is where the new timeline forks.
func chooseCandidate(nodes []nodeView, maxTimeline int) string {
	best := -1
	for i, n := range nodes {
		if !n.Reachable || !n.InRecovery || n.Timeline != maxTimeline {
			continue
		}
		if best < 0 || n.ReceiveLSN > nodes[best].ReceiveLSN ||
			(n.ReceiveLSN == nodes[best].ReceiveLSN && n.Name < nodes[best].Name) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return nodes[best].Name
}

type failureTracker struct {
	threshold int
	now       func() time.Time
	count     int
	since     time.Time
}

// Fail records one failed probe of the leader and reports whether the
// threshold of consecutive failures is reached.
func (f *failureTracker) Fail() bool {
	if f.count == 0 {
		f.since = f.now()
	}
	f.count++
	return f.count >= f.threshold
}

func (f *failureTracker) OK() {
	f.count = 0
	f.since = time.Time{}
}

var (
	errPromotionBusy = errors.New("a promotion is already running")
	errPrecondition  = errors.New("switchover needs writes blocked and failover paused")
)

type controller struct {
	nodes        []nodeConfig
	ops          pgOps
	store        stateSaver
	now          func() time.Time
	logf         func(string, ...any)
	probeTimeout time.Duration
	repointEvery time.Duration
	onRWChange   func()

	promoMu sync.Mutex

	mu        sync.Mutex
	st        persisted
	views     []nodeView
	leader    string
	split     bool
	since     time.Time
	tracker   failureTracker
	failover  string
	lastErr   string
	isolated  map[string]bool
	epoch     uint64
	leaderLSN uint64
	repointed map[string]time.Time
	rr        int
}

func newController(nodes []nodeConfig, ops pgOps, store stateSaver, st persisted, now func() time.Time, logf func(string, ...any)) *controller {
	return &controller{
		nodes: nodes, ops: ops, store: store, st: st, now: now, logf: logf,
		probeTimeout: 800 * time.Millisecond,
		repointEvery: 10 * time.Second,
		onRWChange:   func() {},
		tracker:      failureTracker{threshold: 5, now: now},
		failover:     "idle",
		isolated:     map[string]bool{},
		repointed:    map[string]time.Time{},
	}
}

func (c *controller) node(name string) (nodeConfig, bool) {
	for _, n := range c.nodes {
		if n.Name == name {
			return n, true
		}
	}
	return nodeConfig{}, false
}

func (c *controller) saveLocked() error {
	if err := c.store.Save(c.st); err != nil {
		c.lastErr = "state save: " + err.Error()
		c.logf("state save failed: %v", err)
		return err
	}
	return nil
}

func (c *controller) probeAll(ctx context.Context) []nodeView {
	c.mu.Lock()
	iso := make(map[string]bool, len(c.isolated))
	for k, v := range c.isolated {
		iso[k] = v
	}
	c.mu.Unlock()
	views := make([]nodeView, len(c.nodes))
	var wg sync.WaitGroup
	for i, n := range c.nodes {
		views[i] = nodeView{nodeConfig: n, Isolated: iso[n.Name]}
		if iso[n.Name] {
			views[i].Err = "isolated by fault injection"
			continue
		}
		wg.Add(1)
		go func(i int, n nodeConfig) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, c.probeTimeout)
			defer cancel()
			p, err := c.ops.Probe(pctx, n)
			if err != nil {
				views[i].Err = err.Error()
				return
			}
			views[i].probe = p
		}(i, n)
	}
	wg.Wait()
	return views
}

// tick runs one probe cycle: elect, count failures, promote, re-point.
func (c *controller) tick(ctx context.Context) {
	c.mu.Lock()
	epoch := c.epoch
	c.mu.Unlock()
	views := c.probeAll(ctx)

	c.mu.Lock()
	if c.epoch != epoch {
		// A promotion finished while these probes ran: they predate it.
		c.mu.Unlock()
		return
	}
	dirty := false
	for i := range views {
		v := &views[i]
		switch {
		case !v.Reachable:
		case v.InRecovery:
			// A standby without a WAL receiver reports its checkpoint
			// timeline, which lags behind a promotion it already follows.
			if v.Timeline < c.st.StandbyTL[v.Name] {
				v.Timeline = c.st.StandbyTL[v.Name]
			}
			if c.st.StandbyTL[v.Name] != v.Timeline {
				if c.st.StandbyTL == nil {
					c.st.StandbyTL = map[string]int{}
				}
				c.st.StandbyTL[v.Name] = v.Timeline
				dirty = true
			}
		default:
			if _, ok := c.st.StandbyTL[v.Name]; ok {
				delete(c.st.StandbyTL, v.Name)
				dirty = true
			}
		}
	}
	c.views = views
	for _, v := range views {
		if v.Reachable && v.Timeline > c.st.MaxTimeline {
			c.st.MaxTimeline = v.Timeline
			dirty = true
		}
	}
	if dirty {
		_ = c.saveLocked()
	}
	elected, split := electLeader(views, c.st.MaxTimeline)
	leader := elected
	if elected == "" && !split && c.leader != "" && c.st.Promoting == nil &&
		!reachable(views, c.leader) && c.tracker.count+1 < c.tracker.threshold {
		// One failed probe is not an outage: keep routing to the leader
		// until the failure threshold, so RW connections are not dropped.
		leader = c.leader
	}
	changed := leader != c.leader || split != c.split
	if leader != c.leader {
		c.logf("leader %q -> %q (split_brain=%v)", c.leader, leader, split)
		c.leader = leader
		c.since = c.now()
	}
	c.split = split
	if elected != "" {
		for _, v := range views {
			if v.Name == elected {
				c.leaderLSN = v.LSN
			}
		}
		if c.st.LastLeader != elected {
			c.st.LastLeader = elected
			_ = c.saveLocked()
		}
	}
	var promote string
	switch {
	case c.st.Promoting != nil:
		c.failover = "promoting"
		promote = c.st.Promoting.Node
	case split:
		c.tracker.OK()
		c.failover = "split_brain"
	case elected != "":
		c.tracker.OK()
		c.failover = "idle"
	case c.st.LastLeader == "":
		c.failover = "no_leader"
	case !c.tracker.Fail():
		c.failover = "counting"
	case c.st.FailoverPaused:
		c.failover = "paused"
	default:
		if promote = chooseCandidate(views, c.st.MaxTimeline); promote == "" {
			c.failover = "no_candidate"
		}
	}
	repoints := c.repointTargetsLocked(elected)
	c.mu.Unlock()

	if changed {
		c.onRWChange()
	}
	if promote != "" {
		if err := c.promote(ctx, promote); err != nil && !errors.Is(err, errPromotionBusy) {
			c.logf("promotion of %s: %v", promote, err)
		}
	}
	if elected != "" {
		ln, _ := c.node(elected)
		for _, v := range repoints {
			c.repoint(ctx, v, ln)
		}
	}
}

func reachable(views []nodeView, name string) bool {
	for _, v := range views {
		if v.Name == name {
			return v.Reachable
		}
	}
	return false
}

func (c *controller) repointTargetsLocked(leader string) []nodeView {
	if leader == "" || c.st.Promoting != nil {
		return nil
	}
	now := c.now()
	var out []nodeView
	for _, v := range c.views {
		if !v.Reachable || !v.InRecovery || v.Name == leader {
			continue
		}
		if now.Sub(c.repointed[v.Name]) < c.repointEvery {
			continue
		}
		c.repointed[v.Name] = now
		out = append(out, v)
	}
	return out
}

// repoint makes sure the standby's slot exists on the leader and, when the
// standby streams from another node, points it at the leader.
func (c *controller) repoint(ctx context.Context, v nodeView, leader nodeConfig) {
	if err := c.ops.EnsureSlot(ctx, leader, v.Slot); err != nil {
		c.logf("ensure slot %s on %s: %v", v.Slot, leader.Name, err)
		return
	}
	if v.Upstream == leader.Name {
		return
	}
	if err := c.ops.Repoint(ctx, v.nodeConfig, leader); err != nil {
		c.logf("re-point %s to %s: %v", v.Name, leader.Name, err)
		return
	}
	c.logf("re-pointed %s from %q to %s", v.Name, v.Upstream, leader.Name)
}

func (c *controller) fail(err error) error {
	c.mu.Lock()
	c.lastErr = err.Error()
	c.mu.Unlock()
	return err
}

// promote is the only promotion path (automatic failover and switchover).
// promoting is persisted first, so after a restart only that node is retried.
func (c *controller) promote(ctx context.Context, target string) error {
	if !c.promoMu.TryLock() {
		return errPromotionBusy
	}
	defer c.promoMu.Unlock()
	n, ok := c.node(target)
	if !ok {
		return fmt.Errorf("unknown node %q", target)
	}
	c.mu.Lock()
	if p := c.st.Promoting; p != nil && p.Node != target {
		c.mu.Unlock()
		return fmt.Errorf("promotion of %s is in flight", p.Node)
	}
	fresh := c.st.Promoting == nil
	if fresh {
		c.st.Promoting = &promotingState{Node: target, TS: c.now(), FromTimeline: c.st.MaxTimeline}
		if err := c.saveLocked(); err != nil {
			c.st.Promoting = nil
			c.mu.Unlock()
			return err
		}
		c.logf("promoting %s (from timeline %d)", target, c.st.MaxTimeline)
	}
	from := c.st.Promoting.FromTimeline
	var others []nodeView
	for _, v := range c.views {
		if v.Name != target && v.Reachable && v.InRecovery {
			others = append(others, v)
		}
	}
	c.failover = "promoting"
	c.mu.Unlock()

	p, err := c.ops.Probe(ctx, n)
	if err != nil {
		return c.fail(fmt.Errorf("probe %s: %w", target, err))
	}
	if p.InRecovery {
		if err := c.detachOthers(ctx, n, others, fresh); err != nil {
			return err
		}
		for _, o := range others {
			if err := c.ops.EnsureSlot(ctx, n, o.Slot); err != nil {
				return c.fail(fmt.Errorf("ensure slot %s on %s: %w", o.Slot, target, err))
			}
		}
		if err := c.ops.Promote(ctx, n); err != nil {
			return c.fail(fmt.Errorf("pg_promote %s: %w", target, err))
		}
		if p, err = c.ops.Probe(ctx, n); err != nil {
			return c.fail(fmt.Errorf("probe %s after promote: %w", target, err))
		}
		if p.InRecovery {
			return c.fail(fmt.Errorf("%s still in recovery after pg_promote", target))
		}
	} else if p.Timeline <= from {
		return c.fail(fmt.Errorf("%s is writable on timeline %d, not above %d: stale, not promoted", target, p.Timeline, from))
	}

	c.mu.Lock()
	if p.Timeline > c.st.MaxTimeline {
		c.st.MaxTimeline = p.Timeline
	}
	c.st.LastLeader = target
	c.st.Promoting = nil
	err = c.saveLocked()
	c.epoch++
	c.leader, c.split, c.since = target, false, c.now()
	c.leaderLSN = p.LSN
	c.tracker.OK()
	c.failover = "idle"
	if err == nil {
		c.lastErr = ""
	}
	c.mu.Unlock()
	c.logf("promoted %s, timeline %d", target, p.Timeline)
	c.onRWChange()
	for _, o := range others {
		if err := c.ops.Repoint(ctx, o.nodeConfig, n); err != nil {
			c.logf("re-point %s to %s: %v (the re-point loop retries)", o.Name, target, err)
		}
	}
	return err
}

// detachOthers stops the WAL receivers of the other standbys, then refuses the
// promotion when one of them has received more WAL than the target (a fresh
// promotion is then cleared, so the next tick ranks the candidates again; a
// resumed one keeps its recorded node and only logs it). A standby that is
// unreachable after the attempt is skipped: rejoin.sh brings it back.
func (c *controller) detachOthers(ctx context.Context, target nodeConfig, others []nodeView, fresh bool) error {
	abort := func(err error) error {
		if fresh {
			c.mu.Lock()
			c.st.Promoting = nil
			_ = c.saveLocked()
			c.mu.Unlock()
		}
		return c.fail(err)
	}
	type received struct {
		name string
		lsn  uint64
	}
	var after []received
	for _, o := range others {
		derr := c.ops.Detach(ctx, o.nodeConfig)
		op, perr := c.ops.Probe(ctx, o.nodeConfig)
		if perr != nil || !op.Reachable {
			continue
		}
		if derr != nil {
			return abort(fmt.Errorf("detach %s: %w", o.Name, derr))
		}
		if op.InRecovery {
			after = append(after, received{o.Name, op.ReceiveLSN})
		}
	}
	tp, err := c.ops.Probe(ctx, target)
	if err != nil {
		return c.fail(fmt.Errorf("probe %s: %w", target.Name, err))
	}
	for _, r := range after {
		if r.lsn <= tp.ReceiveLSN {
			continue
		}
		err := fmt.Errorf("%s received more WAL (%s) than %s (%s)", r.name, formatLSN(r.lsn), target.Name, formatLSN(tp.ReceiveLSN))
		if !fresh {
			c.logf("resumed promotion of %s: %v; %s needs rejoin.sh", target.Name, err, r.name)
			continue
		}
		return abort(err)
	}
	return nil
}

// switchoverReady: the target is a standby that has received the old
// leader's final LSN and no less than any other standby.
func switchoverReady(views []nodeView, target string, final uint64) (bool, string) {
	var t *nodeView
	for i := range views {
		if views[i].Name == target {
			t = &views[i]
		}
	}
	if t == nil || !t.Reachable || !t.InRecovery {
		return false, target + " is not a reachable standby"
	}
	if t.ReceiveLSN < final {
		return false, fmt.Sprintf("%s received %s < final %s", target, formatLSN(t.ReceiveLSN), formatLSN(final))
	}
	for _, v := range views {
		if v.Name != target && v.Reachable && v.InRecovery && v.ReceiveLSN > t.ReceiveLSN {
			return false, fmt.Sprintf("%s received more than %s", v.Name, target)
		}
	}
	return true, ""
}

func (c *controller) switchover(ctx context.Context, target string, lsn uint64, wait time.Duration) error {
	c.mu.Lock()
	if !c.st.WritesBlocked || !c.st.FailoverPaused {
		c.mu.Unlock()
		return errPrecondition
	}
	if _, ok := c.node(target); !ok {
		c.mu.Unlock()
		return fmt.Errorf("unknown node %q", target)
	}
	final := max(lsn, c.leaderLSN)
	c.mu.Unlock()
	deadline := time.Now().Add(wait)
	for {
		c.mu.Lock()
		ready, reason := switchoverReady(c.views, target, final)
		c.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return c.fail(fmt.Errorf("switchover aborted after %s: %s; failover stays paused, writes stay blocked", wait, reason))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return c.promote(ctx, target)
}

func (c *controller) setWritesBlocked(v bool) error {
	c.mu.Lock()
	c.st.WritesBlocked = v
	err := c.saveLocked()
	c.mu.Unlock()
	if v {
		c.onRWChange()
	}
	return err
}

func (c *controller) setFailoverPaused(v bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.FailoverPaused = v
	return c.saveLocked()
}

func (c *controller) clearPromoting() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.st.FailoverPaused {
		return errors.New("clear-promoting needs failover paused")
	}
	c.st.Promoting = nil
	return c.saveLocked()
}

func (c *controller) setIsolated(node string, v bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if node == "" {
		c.isolated = map[string]bool{}
		return nil
	}
	if _, ok := c.node(node); !ok {
		return fmt.Errorf("unknown node %q", node)
	}
	c.isolated[node] = v
	return nil
}

func (c *controller) routeRW() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.leader == "" || c.split || c.st.WritesBlocked || c.st.Promoting != nil {
		return "", false
	}
	n, _ := c.node(c.leader)
	return n.Addr, true
}

func (c *controller) routeRO() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var standbys []nodeView
	for _, v := range c.views {
		if v.Reachable && v.InRecovery && v.Timeline == c.st.MaxTimeline {
			standbys = append(standbys, v)
		}
	}
	if len(standbys) > 0 {
		sort.Slice(standbys, func(i, j int) bool { return standbys[i].Name < standbys[j].Name })
		c.rr++
		return standbys[c.rr%len(standbys)].Addr, true
	}
	if c.leader != "" && !c.split {
		n, _ := c.node(c.leader)
		return n.Addr, true
	}
	return "", false
}
