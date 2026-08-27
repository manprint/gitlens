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
	onSample func(SampleRow) // invoked for every row of every successful tick; nil is valid (rows dropped)

	mu               sync.Mutex
	ticksMissed      atomic.Int64
	computeQueryID   *bool // nil = unknown, true/false = discovered
	computeQueryIDMu sync.Mutex
	warnedNoQueryID  atomic.Bool
}

// NewSampler creates a new ASH sampler.
// conn must be dedicated (not shared); it will be held for the lifetime of the sampler.
// onSample is invoked synchronously for every sampled row (typically Aggregator.Add); it must
// not block. The sampler runs independently and must be stopped via Stop().
func NewSampler(conn querier, clk clock.Clock, onSample func(SampleRow)) *Sampler {
	return &Sampler{
		conn:     conn,
		clk:      clk,
		timeout:  500 * time.Millisecond,
		onSample: onSample,
	}
}

// Start begins the 1-second sampling loop. Returns an error if already running.
func (s *Sampler) Start(parentCtx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx != nil {
		return fmt.Errorf("sampler already running")
	}

	runCtx, cancel := context.WithCancel(parentCtx)
	s.ctx, s.cancel = runCtx, cancel
	s.ticker = s.clk.NewTicker(1 * time.Second)

	go s.run(runCtx)
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
	s.mu.Unlock()

	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// TicksMissed returns the count of ticks that exceeded the timeout or failed.
func (s *Sampler) TicksMissed() int64 {
	return s.ticksMissed.Load()
}

// ComputeQueryIDEnabled returns true if compute_query_id is on, false if off, nil if not yet discovered.
func (s *Sampler) ComputeQueryIDEnabled() *bool {
	s.computeQueryIDMu.Lock()
	defer s.computeQueryIDMu.Unlock()
	return s.computeQueryID
}

// run is the main sampling loop. It samples every 1 second and processes the results.
// The sampler must be stopped via Stop(); run does not return until ctx is cancelled.
func (s *Sampler) run(ctx context.Context) {
	ticker := s.ticker
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
			if err := s.doSample(tickCtx); err != nil {
				s.ticksMissed.Add(1)
			}
			cancel()
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

	for rows.Next() {
		var datname, state, waitEventType, waitEvent string
		var queryID *int64

		if err := rows.Scan(&datname, &state, &waitEventType, &waitEvent, &queryID); err != nil {
			return err
		}

		// On first successful sample, check compute_query_id
		s.computeQueryIDMu.Lock()
		if s.computeQueryID == nil {
			enabled := queryID != nil
			s.computeQueryID = &enabled
		}
		s.computeQueryIDMu.Unlock()

		if queryID == nil && s.warnedNoQueryID.CompareAndSwap(false, true) {
			log.Printf("ash: compute_query_id is off — samples will not be attributable to a query")
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

	return rows.Err()
}
