package ash

import (
	"context"
	"fmt"
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
// exercise doSample's parsing and control-flow logic with a fake, so the L1
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

	mu          sync.Mutex
	running     bool
	ticksMissed atomic.Int64
	lastErr     error // error of the most recent tick, nil if it succeeded
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

// LastError returns the error of the most recent tick, or nil when that tick
// succeeded. It is what says why a window's ticks all failed.
func (s *Sampler) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
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
			s.lastErr = err
			onTick := s.onTick
			s.mu.Unlock()
			if onTick != nil {
				onTick(err == nil)
			}
		}
	}
}

// doSample executes one ASH sample query.
//
// The agent's own sessions are named pglens/shared, pglens/ash and
// pglens/<datname>, and their checks are load pglens puts on the instance, not
// the workload it is meant to show. They are left out by name. The bare name
// pglens is not, because the monitoring role's default application_name is
// pglens and the target's own sessions can carry it too.
func (s *Sampler) doSample(ctx context.Context) error {
	rows, err := s.conn.Query(ctx, `
SELECT COALESCE(datname, '')             AS datname,
       state                             AS state,
       COALESCE(wait_event_type, 'CPU')  AS wait_event_type,
       COALESCE(wait_event, 'CPU')       AS wait_event,
       query_id
  FROM pg_stat_activity
 WHERE backend_type IN ('client backend', 'parallel worker')
   AND state <> 'idle'
   AND pid <> pg_backend_pid()
   AND COALESCE(application_name, '') NOT LIKE 'pglens/%'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// The rows of a tick are held back until the whole tick has read cleanly.
	// A tick that fails midway is a failed tick and does not count toward its
	// window's ticks, so the rows it had already returned must not count
	// toward the window's samples either.
	var batch []SampleRow
	for rows.Next() {
		var datname, state, waitEventType, waitEvent string
		var queryID *int64

		if err := rows.Scan(&datname, &state, &waitEventType, &waitEvent, &queryID); err != nil {
			return err
		}

		batch = append(batch, SampleRow{
			Datname:       datname,
			State:         state,
			WaitEventType: waitEventType,
			WaitEvent:     waitEvent,
			QueryID:       queryID,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if s.onSample != nil {
		for _, row := range batch {
			s.onSample(row)
		}
	}

	return nil
}

// SessionsVisible reports whether the role behind q sees every session in
// pg_stat_activity, which takes pg_read_all_stats (pg_monitor includes it) or
// superuser. Without it the other users' sessions come back as hidden rows,
// with NULL state and backend_type, and the sampler does not count them, so
// the samples would read as an idle instance.
func SessionsVisible(ctx context.Context, q querier) (bool, error) {
	rows, err := q.Query(ctx, `SELECT pg_catalog.pg_has_role(current_user, 'pg_read_all_stats', 'USAGE')`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var visible bool
	if err := rows.Scan(&visible); err != nil {
		return false, err
	}
	return visible, nil
}

// ComputeQueryIDSetting returns the compute_query_id setting: on, off, or auto,
// where query IDs are computed only while a loaded module asks for them. A
// sample's query_id is null whenever the setting leaves it unset, so the
// setting is what the agent reports, rather than a guess from null samples.
func ComputeQueryIDSetting(ctx context.Context, q querier) (string, error) {
	rows, err := q.Query(ctx, `SELECT pg_catalog.current_setting('compute_query_id')`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var setting string
	if err := rows.Scan(&setting); err != nil {
		return "", err
	}
	return setting, nil
}
