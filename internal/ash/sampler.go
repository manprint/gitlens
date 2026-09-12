package ash

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/clock"
)

// SampleRow represents one row from the ASH sample query.
type SampleRow struct {
	Datname       string
	State         string
	WaitEventType string
	WaitEvent     string
	QueryID       *int64
}

// querier is the subset of *pgxpool.Conn the sampler needs. Narrowing to an
// interface (matching the internal/server dbPool convention) lets unit tests
// exercise doSample's parsing and warn-once logic with a fake, so the L1
// coverage floor is met without a real database.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Sampler periodically samples pg_stat_activity every 1 second.
type Sampler struct {
	conn     querier
	clk      clock.Clock
	ticker   clock.Ticker
	ctx      context.Context
	cancel   context.CancelFunc
	timeout  time.Duration
	interval time.Duration      // tick period; defaults to 1s in NewSampler, overridable via SetInterval before Start
	onSample func(SampleRow)    // invoked for every row of every successful tick; nil is valid (rows dropped)
	onTick   func(success bool) // invoked exactly once per tick attempt, success or not; nil is valid

	mu                  sync.Mutex
	running             bool
	ticksMissed         atomic.Int64
	computeQueryID      *bool // nil = unknown, true/false = discovered
	computeQueryIDMu    sync.Mutex
	nullOnlySampleCount int // consecutive samples with >=1 row and every row's query_id null
	warnedNoQueryID     atomic.Bool
}

// computeQueryIDOffSamples is how many CONSECUTIVE samples must show every
// row's query_id null before concluding compute_query_id is off. A single
// early sample can catch every active session between statements (query_id
// transiently null even with the GUC genuinely on) — latching "off" from
// that one observation (the original behavior) reported it permanently,
// even though compute_query_id was confirmed on server-wide via `SHOW
// compute_query_id` and later samples would have shown real query_ids. "on"
// still latches from a single non-null observation, since that can only
// happen when the GUC is genuinely on.
const computeQueryIDOffSamples = 5

// NewSampler creates a new ASH sampler.
// conn must be dedicated (not shared); it will be held for the lifetime of the sampler.
// onSample is invoked synchronously for every sampled row (typically Aggregator.Add); it must
// not block. The sampler runs independently and must be stopped via Stop().
func NewSampler(conn querier, clk clock.Clock, onSample func(SampleRow)) *Sampler {
	return &Sampler{
		conn:     conn,
		clk:      clk,
		timeout:  500 * time.Millisecond,
		interval: 1 * time.Second,
		onSample: onSample,
	}
}

// SetInterval overrides the tick period (default 1s) — phase_08.md 7.1's own
// "setting interval: 5s ... is supported for sensitive instances." Must be
// called before Start; a non-positive duration is ignored (keeps the
// default rather than spinning on a zero-length ticker).
func (s *Sampler) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = d
}

// Start begins the sampling loop at the configured interval (default 1s).
// Returns an error if already running.
func (s *Sampler) Start(parentCtx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("sampler already running")
	}
	// Keyed on `running`, not on `s.ctx != nil`: Stop() leaves s.ctx set, so
	// the old condition made a stopped sampler permanently unrestartable and
	// reported it as "already running" — the opposite of the truth.
	s.running = true

	runCtx, cancel := context.WithCancel(parentCtx)
	s.ctx, s.cancel = runCtx, cancel
	ticker := s.clk.NewTicker(s.interval)
	s.ticker = ticker

	// The ticker is handed to run() rather than read from the struct: a
	// restarted sampler has a previous run() goroutine that may not have
	// returned yet, and its unsynchronised `s.ticker` read raced this write.
	// Each run owns the ticker it was started with, so a stale loop can never
	// observe the new one.
	go s.run(runCtx, ticker)
	return nil
}

// Stop gracefully stops the sampling loop.
func (s *Sampler) Stop() {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	s.cancel()
	s.cancel = nil
	s.ctx = nil
	s.running = false
	ticker := s.ticker
	s.ticker = nil
	s.mu.Unlock()

	if ticker != nil {
		ticker.Stop()
	}
}

// TicksMissed returns the count of ticks that exceeded the timeout or failed.
func (s *Sampler) TicksMissed() int64 {
	return s.ticksMissed.Load()
}

// SetTickCallback registers a callback invoked exactly once per tick attempt
// (success or failure), regardless of how many rows that tick returned —
// unlike onSample, which never fires for a genuinely idle, zero-row tick.
// Callers that need an accurate tick count (e.g. driving
// Aggregator.TickSucceeded/TickFailed for the honest samples/ticks average)
// need this distinction; must be called before Start.
func (s *Sampler) SetTickCallback(f func(success bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onTick = f
}

// ComputeQueryIDEnabled returns true if compute_query_id is on, false if off, nil if not yet discovered.
func (s *Sampler) ComputeQueryIDEnabled() *bool {
	s.computeQueryIDMu.Lock()
	defer s.computeQueryIDMu.Unlock()
	return s.computeQueryID
}

// run is the main sampling loop. It samples every 1 second and processes the results.
// The sampler must be stopped via Stop(); run does not return until ctx is cancelled.
func (s *Sampler) run(ctx context.Context, ticker clock.Ticker) {
	if ticker == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			// Create a context with timeout for this tick
			tickCtx, cancel := context.WithTimeout(ctx, s.timeout)
			err := s.doSample(tickCtx)
			if err != nil {
				s.ticksMissed.Add(1)
			}
			cancel()
			s.mu.Lock()
			onTick := s.onTick
			s.mu.Unlock()
			if onTick != nil {
				onTick(err == nil)
			}
		}
	}
}

// doSample executes one ASH sample query.
func (s *Sampler) doSample(ctx context.Context) error {
	rows, err := s.conn.Query(ctx, `
SELECT COALESCE(datname, '')             AS datname,
       COALESCE(state, 'unknown')        AS state,
       COALESCE(wait_event_type, 'CPU')  AS wait_event_type,
       COALESCE(wait_event, 'CPU')       AS wait_event,
       query_id
  FROM pg_stat_activity
 WHERE backend_type = 'client backend'
   AND state <> 'idle'
   AND pid <> pg_backend_pid()
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sawRow, sawNonNullQueryID bool
	for rows.Next() {
		var datname, state, waitEventType, waitEvent string
		var queryID *int64

		if err := rows.Scan(&datname, &state, &waitEventType, &waitEvent, &queryID); err != nil {
			return err
		}
		sawRow = true
		if queryID != nil {
			sawNonNullQueryID = true
		}

		if s.onSample != nil {
			s.onSample(SampleRow{
				Datname:       datname,
				State:         state,
				WaitEventType: waitEventType,
				WaitEvent:     waitEvent,
				QueryID:       queryID,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.computeQueryIDMu.Lock()
	if s.computeQueryID == nil {
		switch {
		case sawNonNullQueryID:
			enabled := true
			s.computeQueryID = &enabled
		case sawRow:
			s.nullOnlySampleCount++
			if s.nullOnlySampleCount >= computeQueryIDOffSamples {
				disabled := false
				s.computeQueryID = &disabled
			}
		}
		// No active sessions at all this tick: inconclusive, don't count
		// either way — waiting for a sample that actually has rows.
	}
	s.computeQueryIDMu.Unlock()

	s.computeQueryIDMu.Lock()
	knownOff := s.computeQueryID != nil && !*s.computeQueryID
	s.computeQueryIDMu.Unlock()
	if knownOff && s.warnedNoQueryID.CompareAndSwap(false, true) {
		log.Printf("ash: compute_query_id is off — samples will not be attributable to a query")
	}

	return nil
}
