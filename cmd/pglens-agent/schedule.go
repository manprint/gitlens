package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/manprint/pglens/internal/agent"
	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
)

// scheduleReconcileInterval is how often the agent re-reads what each target's
// checks are gated on. A promotion, a target that became reachable after
// start, an extension installed later or a new database is picked up within
// this interval, and the next push cycle reports it.
const scheduleReconcileInterval = 30 * time.Second

// scheduleReadTimeout bounds one target's gate read, so an unreachable target
// cannot hold up the reconcile of the others.
const scheduleReadTimeout = 10 * time.Second

// targetGate is what one target's checks are gated on: the inputs
// Requirements.Supports reads, and the databases its database-scoped checks run
// for.
type targetGate struct {
	role      pgtype.Role
	version   pgtype.PGVersion
	tier      pgtype.PermTier
	exts      map[string]bool
	databases map[string]bool
	// statementsUnknown reports that pg_stat_statements could not be looked for
	// in every monitored database, so an absent view means nothing. nextGate
	// keeps the last answer then. It is not part of key.
	statementsUnknown bool
}

// key is the part of a gate that a change rebuilds the schedule for. The
// database set is compared separately, because it only grows.
func (g targetGate) key() string {
	return fmt.Sprintf("%s/%d/%s/%v", g.role, g.version, g.tier, g.exts)
}

// readTargetGate reads a target's gate values and its database discovery. ok is
// false when the target cannot be read in full this time: discovery failed, or
// its version or role is still unknown.
func readTargetGate(ctx context.Context, mgr *agent.Manager) (targetGate, bool) {
	ctx, cancel := context.WithTimeout(ctx, scheduleReadTimeout)
	defer cancel()

	dbs, err := mgr.Discover(ctx) //nolint:contextcheck // Discover takes the reconcile context
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: discover databases for %s: %v\n", mgr.Database(), err)
	}
	// A read that ran out of time, or whose context ended at shutdown, stops
	// here. The accessors below take no context and each wait out their own
	// timeout on an unreachable target, so calling them would hold the read up
	// for the timeouts again after the caller had already given up.
	if ctx.Err() != nil {
		return targetGate{role: pgtype.RoleUnknown, tier: pgtype.TierReadOnly, exts: map[string]bool{}, databases: map[string]bool{}}, false
	}
	// PGVersion fills the cache that Role and PermTier read from. When it cannot
	// be filled, they report RoleUnknown and TierReadOnly, so they are only read
	// once it has succeeded.
	version := mgr.PGVersion() //nolint:contextcheck // Manager caches this; the accessor takes no context
	g := targetGate{
		role:      pgtype.RoleUnknown,
		version:   version,
		tier:      pgtype.TierReadOnly,
		exts:      map[string]bool{},
		databases: map[string]bool{},
	}
	if version != 0 {
		g.role = mgr.Role()     //nolint:contextcheck // same as above
		g.tier = mgr.PermTier() //nolint:contextcheck // same as above
	}
	// pg_stat_statements counts as present when any monitored database has the
	// view, not only the maintenance database: the check reads the others
	// through it. LocateStatements also records the database the check reads
	// through, so it only runs on a complete discovery.
	g.exts["pg_stat_statements"] = false
	if err == nil {
		found, statementsErr := mgr.LocateStatements(ctx, dbs)
		g.exts["pg_stat_statements"] = found
		g.statementsUnknown = statementsErr != nil
	}
	for _, db := range dbs {
		if db.Monitored {
			g.databases[db.Name] = true
		}
	}
	return g, err == nil && g.version != 0 && g.role != pgtype.RoleUnknown
}

// nextGate picks the gate a target is scheduled against after one read, and
// reports whether the schedule has to be rebuilt for it.
//
// A target that cannot be read keeps the gate it was last scheduled against, so
// a dropped connection does not tear down its checks. The database set only
// grows: the budget ranks databases by commit count, so it can push a database
// out and bring it back, and rebuilding for every such swap would reset the
// circuit breakers and the jitter on each pass.
func nextGate(last targetGate, seen bool, read targetGate, readOK bool) (targetGate, bool) {
	switch {
	case !seen:
		return read, true
	case !readOK:
		return last, false
	}
	// A database that could not be checked may be the one holding the view, so
	// its absence is not evidence. The answer the schedule was built with stays,
	// and a database that is unreachable for a while does not rebuild the checks
	// each time. The other gate values are still current.
	if read.statementsUnknown {
		exts := make(map[string]bool, len(read.exts))
		for name, present := range read.exts {
			exts[name] = present
		}
		exts["pg_stat_statements"] = last.exts["pg_stat_statements"]
		read.exts = exts
	}
	read.databases = unionDatabases(last.databases, read.databases)
	return read, read.key() != last.key() || len(read.databases) != len(last.databases)
}

func unionDatabases(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for name := range a {
		out[name] = true
	}
	for name := range b {
		out[name] = true
	}
	return out
}

// scheduleSet owns the check scheduler and keeps it in step with the targets.
//
// The scheduler takes its entries only before Start, so a change to what the
// checks are gated on is applied by building a fresh scheduler from the current
// gates, starting it, and stopping the one it replaces. Every target shares the
// new scheduler, so the worker bound is the same as with one schedule.
type scheduleSet struct {
	ctx          context.Context
	cfg          *agent.Config
	clk          clock.Clock
	managers     []*agent.Manager
	mu           *sync.Mutex // guards pending and scheduledDBs, which the flush also reads
	pending      map[string][]wire.Result
	scheduledDBs map[string]map[string]bool // target -> databases its database-scoped checks run for

	// reconcileMu serialises start and reconcile, so two passes never build from
	// the same gates. It is held across the reads, which wait on the targets, so
	// stop does not take it.
	reconcileMu sync.Mutex
	// schedMu guards the fields below and serialises builds and stops. It is
	// never held across a read, so stop does not wait behind one.
	schedMu sync.Mutex
	current *agent.Scheduler
	gates   map[string]targetGate // target -> gate the current scheduler was built from
	stopped bool
	drains  sync.WaitGroup // one per scheduler, until its results are forwarded
}

func newScheduleSet(ctx context.Context, cfg *agent.Config, clk clock.Clock, managers []*agent.Manager, mu *sync.Mutex, pending map[string][]wire.Result, scheduledDBs map[string]map[string]bool) *scheduleSet {
	return &scheduleSet{
		ctx:          ctx,
		cfg:          cfg,
		clk:          clk,
		managers:     managers,
		mu:           mu,
		pending:      pending,
		scheduledDBs: scheduledDBs,
	}
}

// start reads every target and starts the first schedule. readCtx is the context
// the discovery and statement lookups run under; the gate accessors bound their
// own waits. A target that cannot be read yet gets its checks from the first
// reconcile that reads it in full.
func (s *scheduleSet) start(readCtx context.Context) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	gates, _ := s.readGates(readCtx, nil)
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	s.gates = gates
	return s.buildLocked()
}

// reconcile reads every target again and rebuilds the schedule when a gate
// changed. It does nothing once stop has run.
func (s *scheduleSet) reconcile() {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.schedMu.Lock()
	if s.stopped {
		s.schedMu.Unlock()
		return
	}
	prev := s.gates
	s.schedMu.Unlock()

	// An unreachable target holds its read for the whole timeout, so the read
	// runs without schedMu. The revoke path and the shutdown can stop the
	// schedule while it is in flight.
	gates, changed := s.readGates(s.ctx, prev)
	if len(changed) == 0 {
		return
	}

	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	if s.stopped {
		return
	}
	for _, name := range changed {
		fmt.Fprintf(os.Stderr, "info: check schedule for %s rebuilt: %s\n", name, gates[name].key())
	}
	s.stopCurrentLocked()
	s.gates = gates
	if err := s.buildLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: rebuild check schedule: %v\n", err)
	}
}

// run reconciles every scheduleReconcileInterval until the process context ends.
func (s *scheduleSet) run() {
	ticker := s.clk.NewTicker(scheduleReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C():
			s.reconcile()
		}
	}
}

// stop ends the schedule for good. The revoke path and the shutdown both call
// it, and no rebuild runs after it.
func (s *scheduleSet) stop() {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	s.stopped = true
	s.stopCurrentLocked()
}

// drained is closed once every scheduler has forwarded its last result. Call it
// after stop, when no new scheduler can start.
func (s *scheduleSet) drained() <-chan struct{} {
	done := make(chan struct{})
	go func() {
		s.drains.Wait()
		close(done)
	}()
	return done
}

// readGates reads every target against prev, the gates the running schedule was
// built from, and returns the new gates with the names of the targets whose
// gate changed. The targets are read together: an unreachable one holds its
// read for the whole timeout, and read one after another, every target after it
// would wait that timeout too.
func (s *scheduleSet) readGates(ctx context.Context, prev map[string]targetGate) (map[string]targetGate, []string) {
	reads := make([]targetGate, len(s.managers))
	oks := make([]bool, len(s.managers))
	var wg sync.WaitGroup
	for i, mgr := range s.managers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reads[i], oks[i] = readTargetGate(ctx, mgr)
		}()
	}
	wg.Wait()

	next := make(map[string]targetGate, len(s.managers))
	var changed []string
	for i, mgr := range s.managers {
		name := mgr.Database()
		last, seen := prev[name]
		g, differs := nextGate(last, seen, reads[i], oks[i])
		next[name] = g
		if differs {
			changed = append(changed, name)
		}
	}
	return next, changed
}

// buildLocked builds, starts and begins draining a schedule for s.gates. The
// caller holds schedMu.
func (s *scheduleSet) buildLocked() error {
	sched := agent.NewScheduler(agent.DefaultScheduleOptions())
	s.mu.Lock()
	for _, mgr := range s.managers {
		s.scheduledDBs[mgr.Database()] = s.gates[mgr.Database()].databases
	}
	s.mu.Unlock()
	for _, mgr := range s.managers {
		addScheduleEntries(sched, mgr, s.cfg, s.mu, s.pending, s.gates[mgr.Database()])
	}
	if err := sched.Start(s.ctx); err != nil {
		return err
	}
	s.current = sched
	s.drains.Add(1)
	go func() {
		defer s.drains.Done()
		forwardResults(sched.Results(), s.clk, s.mu, s.pending)
	}()
	return nil
}

// stopCurrentLocked stops the running scheduler, if there is one. The caller
// holds schedMu. Stop waits for in-flight scrapes, so it never runs under mu.
func (s *scheduleSet) stopCurrentLocked() {
	if s.current != nil {
		s.current.Stop()
	}
}

// addScheduleEntries adds one target's entries for the gate it was read to. A
// check whose requirements the gate does not meet is queued as a skip instead.
func addScheduleEntries(scheduler *agent.Scheduler, mgr *agent.Manager, cfg *agent.Config, mu *sync.Mutex, pending map[string][]wire.Result, g targetGate) {
	for _, c := range check.All() {
		interval := cfg.GetCheckInterval(c.Name())
		if c.Name() == "ash" {
			// Driven by a dedicated sampler+aggregator (startASH, ash.go),
			// not the scheduler's once-per-interval Scrape() model — a
			// continuously-running 1-second loop doesn't fit that shape.
			// The registered check itself stays for its own unit tests, but
			// must never actually be scheduled, or its own superficial
			// count-only scrape would double-report under the same "ash"
			// check name.
			continue
		}
		req := c.Requires()
		if ok, reason := req.Supports(g.role, g.version, g.tier, g.exts); !ok {
			mu.Lock()
			pending[mgr.Database()] = append(pending[mgr.Database()], wire.Result{
				Check: c.Name(), TS: time.Now().UTC(), SkipReason: reason,
			})
			mu.Unlock()
			continue
		}
		if req.Scope == check.ScopeDatabase {
			for name := range g.databases {
				scheduler.AddEntryWithInterval(mgr, c, name, interval)
			}
			continue
		}
		scheduler.AddEntryWithInterval(mgr, c, "", interval)
	}
}

// forwardResults folds every result a scheduler produces into its target's
// pending batch, until the scheduler closes its results channel.
func forwardResults(results <-chan agent.EntryResult, clk clock.Clock, mu *sync.Mutex, pending map[string][]wire.Result) {
	for res := range results {
		m := make([]wire.Metric, 0, len(res.Metrics))
		for _, pm := range res.Metrics {
			m = append(m, wire.Metric{Name: pm.Name, Labels: pm.Labels, Value: pm.Value, Kind: string(pm.Kind)})
		}
		wr := wire.Result{Check: res.Entry.CheckName, TS: clk.Now(), Database: res.Entry.Database, Metrics: m, Truncated: res.Truncated, Partial: res.Partial}
		for _, f := range res.Facts {
			wf := wire.Fact{Kind: f.Kind, Key: f.Key, Labels: f.Labels, ValueText: f.ValueText, ValueJSON: f.ValueJSON}
			if err := wf.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: drop invalid fact check=%s key=%s: %v\n", res.Entry.CheckName, f.Key, err)
				continue
			}
			wr.Facts = append(wr.Facts, wf)
		}
		if res.Err != nil {
			wr.Error = res.Err.Error()
		}
		// check.Result.QueryTexts (int64 -> text) was computed by
		// stat_statements.go all along but never actually reached the
		// wire — agent.EntryResult never carried it and this loop never
		// converted it, found live re-verifying phase 7.5's README:
		// /api/v1/ash/top's query_text join always returned "" because
		// query_texts was never being written by anything, since
		// pipeline.go's own wire.Result.QueryTexts consumer (already
		// correct) had nothing to consume.
		if len(res.QueryTexts) > 0 {
			wr.QueryTexts = make(map[string]string, len(res.QueryTexts))
			for qid, text := range res.QueryTexts {
				wr.QueryTexts[strconv.FormatInt(qid, 10)] = text
			}
		}
		// Same class of gap, found immediately after: check.Result.StatsReset
		// (stat_statements.go's own pg_stat_statements_info.reset_time
		// read) never reached the wire either — SYS-RESET-002 needs the
		// server's delta engine to see an explicit reset hint, not just
		// infer one from a counter decrease.
		wr.StatsReset = res.StatsReset
		mu.Lock()
		pending[res.Entry.TargetName] = append(pending[res.Entry.TargetName], wr)
		mu.Unlock()
	}
}
