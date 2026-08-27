package agent

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
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
	Entry     ScheduleEntry
	Metrics   []pgtype.Metric
	Truncated bool
	Err       error
}

// Scheduler schedules checks on a bounded worker pool with per-check timeouts and circuit breaker.
type Scheduler struct {
	maxWorkers      int
	noJitter        bool
	clk             clock.Clock
	entries         []schedEntry
	breaker         map[string]*Breaker // per-check
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

type schedEntry struct {
	target   check.Target
	check    check.Check
	database string // empty for instance scope
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
		breaker:         make(map[string]*Breaker),
		results:         make(chan EntryResult, 100),
		workerSem:       make(chan struct{}, opts.MaxWorkers),
		shutdownTimeout: opts.ShutdownTimeout,
	}
}

// AddEntry registers a (target, check, database?) entry. Must be called before Start().
func (s *Scheduler) AddEntry(target check.Target, c check.Check, database string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		panic("AddEntry called after Start()")
	}

	br, ok := s.breaker[c.Name()]
	if !ok {
		br = NewBreaker()
		s.breaker[c.Name()] = br
	}

	jitter := time.Duration(0)
	if !s.noJitter {
		// Jitter up to one interval per entry
		interval := c.DefaultInterval()
		if interval > 0 {
			jitter = time.Duration(rand.Int63n(int64(interval)))
		}
	}

	s.entries = append(s.entries, schedEntry{
		target:   target,
		check:    c,
		database: database,
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

// Stop gracefully shuts down the scheduler, allowing in-flight scrapes to finish within ShutdownTimeout.
func (s *Scheduler) Stop() {
	s.cancel()
	doneCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(s.shutdownTimeout):
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
	interval := entry.check.DefaultInterval()
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

	for {
		select {
		case <-ticker.C():
			if entry.breaker.Allow() {
				s.workerSem <- struct{}{}
				s.wg.Add(1)
				s.scrapeWG.Add(1)
				go func() {
					defer s.wg.Done()
					defer s.scrapeWG.Done()
					defer func() { <-s.workerSem }()
					s.scrapeEntry(ctx, entry)
				}()
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) scrapeEntry(parentCtx context.Context, entry schedEntry) {
	ctx, cancel := context.WithTimeout(parentCtx, entry.check.Timeout())
	defer cancel()

	// Apply session limits if the target has a connection
	conn, err := s.getConnection(ctx, entry)
	if err != nil {
		s.recordError(entry, fmt.Sprintf("connection error: %v", err))
		return
	}
	defer conn.Release()

	if err := check.ApplySessionLimits(ctx, conn, entry.check.Timeout()); err != nil {
		s.recordError(entry, fmt.Sprintf("apply limits: %v", err))
		return
	}

	result, err := entry.check.Scrape(ctx, entry.target)
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
		Metrics:   result.Metrics,
		Truncated: result.Truncated,
		Err:       nil,
	}:
	case <-s.ctx.Done():
	}
}

func (s *Scheduler) getConnection(ctx context.Context, entry schedEntry) (check.Conn, error) {
	if entry.database == "" {
		return entry.target.Conn(ctx)
	}
	return entry.target.ConnFor(ctx, entry.database)
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
