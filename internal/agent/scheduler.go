package agent

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
)

// ScheduleEntry is a single (target, check, database?) entry.
type ScheduleEntry struct {
	TargetName string
	CheckName  string
	Database   string // empty for instance-scoped checks
}

// EntryResult is the result of a single entry's scrape.
type EntryResult struct {
	Entry      ScheduleEntry
	Metrics    []pgtype.Metric
	Facts      []check.Fact
	Truncated  bool
	Partial    bool
	Err        error
	QueryTexts map[int64]string // queryid -> text; stat_statements.go is the only producer today
	// StatsReset is when the check itself detected its own counters were
	// externally reset (e.g. pg_stat_statements_reset()) — stat_statements.go
	// has always computed this via check.Result.StatsReset, but nothing
	// carried it past scrapeEntry until SYS-RESET-002 needed it: without it,
	// the server's delta engine has no explicit reset hint and only a
	// generic counter-decrease heuristic to fall back on.
	StatsReset *time.Time
}

// Scheduler schedules checks on a bounded worker pool with per-check timeouts
// and one circuit breaker per scheduled entry (target, check, database).
type Scheduler struct {
	maxWorkers      int
	noJitter        bool
	clk             clock.Clock
	entries         []schedEntry
	breaker         map[breakerKey]*Breaker // per (target, check, database)
	results         chan EntryResult
	wg              sync.WaitGroup
	scrapeWG        sync.WaitGroup // tracks in-flight scrapeEntry goroutines specifically, see run()
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	running         bool
	workerSem       chan struct{} // semaphore for bounded concurrency
	shutdownTimeout time.Duration
}

// breakerKey identifies one scheduled entry for the circuit breaker.
type breakerKey struct {
	target   string
	check    string
	database string
}

type schedEntry struct {
	target   check.Target
	check    check.Check
	database string // empty for instance scope
	interval time.Duration
	breaker  *Breaker
	jitter   time.Duration
}

// ScheduleOptions configures a Scheduler.
type ScheduleOptions struct {
	MaxWorkers      int
	NoJitter        bool
	Clock           clock.Clock
	ShutdownTimeout time.Duration
}

// DefaultScheduleOptions returns sensible defaults.
func DefaultScheduleOptions() ScheduleOptions {
	maxWorkers := runtime.GOMAXPROCS(0)
	if maxWorkers > 8 {
		maxWorkers = 8
	}
	return ScheduleOptions{
		MaxWorkers:      maxWorkers,
		NoJitter:        false,
		Clock:           clock.System(),
		ShutdownTimeout: 10 * time.Second,
	}
}

// NewScheduler creates a scheduler. You must call Start().
func NewScheduler(opts ScheduleOptions) *Scheduler {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	return &Scheduler{
		maxWorkers:      opts.MaxWorkers,
		noJitter:        opts.NoJitter,
		clk:             opts.Clock,
		breaker:         make(map[breakerKey]*Breaker),
		results:         make(chan EntryResult, 100),
		workerSem:       make(chan struct{}, opts.MaxWorkers),
		shutdownTimeout: opts.ShutdownTimeout,
	}
}

// AddEntry registers a (target, check, database?) entry. Must be called before Start().
func (s *Scheduler) AddEntry(target check.Target, c check.Check, database string) {
	s.AddEntryWithInterval(target, c, database, 0)
}

// AddEntryWithInterval registers an entry with an optional configuration
// override. A non-positive interval uses the check's implementation default.
func (s *Scheduler) AddEntryWithInterval(target check.Target, c check.Check, database string, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		panic("AddEntry called after Start()")
	}

	// One breaker per entry. Keyed by check name alone, one unreachable
	// target (or one database without the extension a check needs) opened
	// the circuit for that check on every other target and database too, so
	// healthy instances stopped reporting for the whole backoff.
	key := breakerKey{target: target.Database(), check: c.Name(), database: database}
	br, ok := s.breaker[key]
	if !ok {
		br = NewBreaker()
		s.breaker[key] = br
	}

	if interval <= 0 {
		interval = c.DefaultInterval()
	}
	jitter := time.Duration(0)
	if !s.noJitter {
		// Jitter up to one interval per entry
		if interval > 0 {
			jitter = time.Duration(rand.Int63n(int64(interval)))
		}
	}

	s.entries = append(s.entries, schedEntry{
		target:   target,
		check:    c,
		database: database,
		interval: interval,
		breaker:  br,
		jitter:   jitter,
	})
}

// Start begins the scheduling loop. It is not safe to call AddEntry after Start.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("scheduler already running")
	}
	s.running = true
	runCtx, cancel := context.WithCancel(ctx)
	s.ctx, s.cancel = runCtx, cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.run(runCtx)
	return nil
}

// Stop gracefully shuts down the scheduler, allowing in-flight scrapes to
// finish within ShutdownTimeout. It is safe to call before Start, and safe to
// call more than once — cmd/pglens-agent calls it from the push loop when the
// server revokes the agent and again at shutdown.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		// Never started: nothing is running, and calling a nil cancel would
		// panic instead of being the no-op every caller expects.
		return
	}
	cancel()

	doneCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(doneCh)
	}()

	timer := time.NewTimer(s.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-doneCh:
	case <-timer.C:
	}
}

// Results returns a channel of entry results. Closed when Stop() completes.
func (s *Scheduler) Results() <-chan EntryResult {
	return s.results
}

func (s *Scheduler) run(ctx context.Context) {
	defer s.wg.Done()
	defer close(s.results)

	if len(s.entries) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, entry := range s.entries {
		wg.Add(1)
		go func(entry schedEntry) {
			defer wg.Done()
			s.runEntry(ctx, entry)
		}(entry)
	}

	// Wait for every per-entry ticker loop to exit on ctx.Done() ...
	wg.Wait()
	// ... and for every scrapeEntry goroutine any of them spawned right
	// before exiting to actually finish. A ticker loop can select
	// ctx.Done() in the same instant it just launched a scrape (the
	// select in runEntry races the two cases), so without this second
	// wait close(s.results) below can run while a scrape goroutine is
	// still trying to send on it — a "send on closed channel" panic.
	s.scrapeWG.Wait()
}

func (s *Scheduler) runEntry(ctx context.Context, entry schedEntry) {
	interval := entry.interval
	if interval == 0 {
		interval = 1 * time.Second
	}

	// Apply jitter
	if entry.jitter > 0 {
		err := s.clk.Sleep(ctx, entry.jitter)
		if err != nil {
			return
		}
	}

	ticker := s.clk.NewTicker(interval)
	defer ticker.Stop()

	// A scrape still running when the next tick arrives owns the entry: the
	// tick is skipped instead of starting a second scrape of it. Overlapping
	// scrapes could deliver their snapshots out of order, and the delta-based
	// checks need them in order; a slow check would also fill every worker
	// with copies of itself. Only this loop sets the flag, so it needs no lock.
	var busy atomic.Bool

	for {
		select {
		case <-ticker.C():
			if busy.Load() {
				continue
			}
			if entry.breaker.Allow() {
				// Acquiring the worker slot must observe cancellation: with
				// every worker busy on a target that has stopped answering,
				// a bare send parked this ticker loop until some scrape's
				// own timeout expired, so Stop() had to wait out that scrape
				// before this loop could even notice ctx was done — turning
				// a graceful stop into a ShutdownTimeout-length hang.
				select {
				case s.workerSem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				busy.Store(true)
				s.wg.Add(1)
				s.scrapeWG.Add(1)
				go func() {
					defer s.wg.Done()
					defer s.scrapeWG.Done()
					defer func() { <-s.workerSem }()
					defer busy.Store(false)
					s.scrapeEntry(ctx, entry)
				}()
			}
		case <-ctx.Done():
			return
		}
	}
}

// databaseScopedTarget wraps a check.Target for one ScopeDatabase entry,
// overriding only Database() to return the entry's own discovered database
// name. Every other method delegates to the embedded Target unchanged.
//
// Found live verifying phase 7.5's README against a real multi-database
// agent config (target name "pg", monitored database "postgres" — genuinely
// different strings): stat_statements.go's Scrape() calls
// `t.ConnFor(ctx, t.Database())`, and the underlying Manager.Database()
// always returns the *target's own name* ("pg"), never the per-entry
// database this scheduler already resolved via entry.database — so the
// check silently tried to connect to a database literally named "pg" (which
// doesn't exist), failing every single scrape. This had gone completely
// unnoticed because stat_statements was then the only ScopeDatabase check in
// the codebase, so no other check could have exposed the same interface
// mismatch, and check-level Scrape errors are never logged to agent stderr
// (only carried silently in wire.Result.Error — V001-F11's absent
// check_error_total is exactly the visibility gap that let this hide). Every
// pre-existing unit/integration test happened to use a target name equal to
// its own database name, masking the bug by coincidence.
type databaseScopedTarget struct {
	check.Target
	database string
}

func (d databaseScopedTarget) Database() string { return d.database }

// StatementsDatabase forwards the Manager's choice: embedding check.Target
// promotes only that interface's methods, which would hide it from
// check.StatementsConn.
func (d databaseScopedTarget) StatementsDatabase() string {
	if s, ok := d.Target.(check.StatementsSource); ok {
		return s.StatementsDatabase()
	}
	return ""
}

// scheduledConnForer is implemented by a Manager. Its scheduled checks reach
// their databases through ScheduledConnFor, not through ConnFor, which commands
// use.
type scheduledConnForer interface {
	ScheduledConnFor(ctx context.Context, datname string) (check.Conn, error)
}

// ConnFor is what the checks call to reach their database, the entry's own
// one or the statements database. The schedule was built at startup, so these
// checks use the startup selection rather than the latest one a command sees.
func (d databaseScopedTarget) ConnFor(ctx context.Context, datname string) (check.Conn, error) {
	if s, ok := d.Target.(scheduledConnForer); ok {
		return s.ScheduledConnFor(ctx, datname)
	}
	return d.Target.ConnFor(ctx, datname)
}

func (s *Scheduler) scrapeEntry(parentCtx context.Context, entry schedEntry) {
	ctx, cancel := context.WithTimeout(parentCtx, entry.check.Timeout())
	defer cancel()

	// Checks acquire and release their own connection. Acquiring one here as
	// well would consume a second pool slot for every scrape; with database-
	// scoped checks that can exhaust the shared pool before Scrape can run.
	scrapeTarget := entry.target
	if entry.database != "" {
		scrapeTarget = databaseScopedTarget{Target: entry.target, database: entry.database}
	}
	result, err := entry.check.Scrape(ctx, scrapeTarget)
	if err != nil {
		s.recordError(entry, fmt.Sprintf("scrape error: %v", err))
		return
	}

	entry.breaker.RecordSuccess()

	select {
	case s.results <- EntryResult{
		Entry: ScheduleEntry{
			TargetName: entry.target.Database(),
			CheckName:  entry.check.Name(),
			Database:   entry.database,
		},
		Metrics:    result.Metrics,
		Facts:      result.Facts,
		Truncated:  result.Truncated,
		Partial:    result.Partial,
		QueryTexts: result.QueryTexts,
		StatsReset: result.StatsReset,
		Err:        nil,
	}:
	case <-s.ctx.Done():
	}
}

func (s *Scheduler) recordError(entry schedEntry, reason string) {
	entry.breaker.RecordFailure()

	select {
	case s.results <- EntryResult{
		Entry: ScheduleEntry{
			TargetName: entry.target.Database(),
			CheckName:  entry.check.Name(),
			Database:   entry.database,
		},
		Err: fmt.Errorf("check failed: %s", reason),
	}:
	case <-s.ctx.Done():
	}
}
