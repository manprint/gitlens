package advisor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/clock"
)

const defaultAdvisorInterval = 15 * time.Minute

type snapshotLoader func(context.Context, uuid.UUID, time.Time) (*Snapshot, error)

// Engine evaluates registered advisor rules and persists current findings.
type Engine struct {
	pool      *pgxpool.Pool
	clock     clock.Clock
	interval  time.Duration
	store     Store
	load      snapshotLoader
	mu        sync.Mutex
	conn      *pgxpool.Conn
	hasLock   bool
	started   bool
	ticker    clock.Ticker
	stopCh    chan struct{}
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	// leaderOverride keeps leader/follower behavior deterministic in unit tests.
	// Production always uses the PostgreSQL advisory lock below.
	leaderOverride func(context.Context) (bool, error)
}

func NewEngine(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration) *Engine {
	if clk == nil {
		clk = clock.System()
	}
	if interval <= 0 {
		interval = advisorInterval()
	}
	var st Store
	if pool != nil {
		st = NewPgStore(pool)
	}
	return newEngine(pool, clk, interval, st, nil)
}

func NewEngineWithStore(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration, st Store) *Engine {
	if clk == nil {
		clk = clock.System()
	}
	if interval <= 0 {
		interval = advisorInterval()
	}
	return newEngine(pool, clk, interval, st, nil)
}

func newEngine(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration, st Store, loader snapshotLoader) *Engine {
	e := &Engine{pool: pool, clock: clk, interval: interval, store: st, load: loader, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	if e.load == nil {
		e.load = func(ctx context.Context, id uuid.UUID, now time.Time) (*Snapshot, error) {
			return LoadSnapshot(ctx, pool, id, now)
		}
	}
	return e
}

func (e *Engine) Start(ctx context.Context) {
	e.startOnce.Do(func() {
		e.mu.Lock()
		e.started = true
		e.ticker = e.clock.NewTicker(e.interval)
		t := e.ticker
		e.mu.Unlock()
		go func() {
			defer close(e.doneCh)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-e.stopCh:
					return
				case <-t.C():
					runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
					if err := e.Run(runCtx); err != nil {
						log.Printf("advisor engine: %v", err)
					}
					cancel()
				}
			}
		}()
	})
}
func (e *Engine) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		e.mu.Lock()
		started := e.started
		t := e.ticker
		e.mu.Unlock()
		if started {
			<-e.doneCh
		}
		if t != nil {
			t.Stop()
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = e.conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('pglens:advisor'))`)
			cancel()
			e.conn.Release()
			e.conn = nil
		}
		e.hasLock = false
	})
}

func (e *Engine) ensureLeader(ctx context.Context) error {
	if e.leaderOverride != nil {
		locked, err := e.leaderOverride(ctx)
		e.mu.Lock()
		e.hasLock = locked
		e.mu.Unlock()
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool == nil {
		e.hasLock = true
		return nil
	}
	// The advisory lock is held by this session, so leadership dies with the
	// connection. Without this check a closed conn was indistinguishable from
	// a live one (hasLock && conn != nil short-circuited), so a server that
	// lost its session kept writing findings as "the leader" while another
	// server legitimately took the freed lock.
	if e.conn != nil && e.conn.Conn().IsClosed() {
		e.releaseLeaderConn()
	}
	if e.hasLock && e.conn != nil {
		return nil
	}
	if e.conn == nil {
		c, err := e.pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire conn for advisor lock: %w", err)
		}
		e.conn = c
	}
	var locked bool
	if err := e.conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:advisor'))`).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			e.hasLock = false
			return nil
		}
		// Hand the connection back rather than pinning a broken one: every
		// later pass would otherwise retry on the same dead session and the
		// engine could never regain leadership without a restart.
		e.releaseLeaderConn()
		return fmt.Errorf("try advisor advisory lock: %w", err)
	}
	e.hasLock = locked
	return nil
}

// releaseLeaderConn drops the lock-holding connection and the leadership it
// carried. Caller must hold e.mu.
func (e *Engine) releaseLeaderConn() {
	if e.conn != nil {
		e.conn.Release()
		e.conn = nil
	}
	e.hasLock = false
}

// Run executes one complete pass. A follower returns without touching findings.
func (e *Engine) Run(ctx context.Context) error {
	if e.store == nil {
		return nil
	}
	if err := e.ensureLeader(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	leader := e.hasLock
	e.mu.Unlock()
	if !leader {
		return nil
	}
	now := e.clock.Now()
	ids, err := e.store.ActiveInstances(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	// One instance failing must not cost every other instance its pass. This
	// used to abort at the first error, so a single unreachable or
	// mis-permissioned instance (a snapshot that cannot be loaded) stopped
	// advisor findings for every instance after it in the list and skipped
	// the retention purge entirely — indefinitely, since the list order is
	// stable.
	var errs []error
	for _, id := range ids {
		if err := e.runInstance(ctx, id, now); err != nil {
			errs = append(errs, err)
		}
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
	}
	if err := e.store.PurgeResolved(ctx, now.Add(-90*24*time.Hour)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (e *Engine) runInstance(ctx context.Context, id uuid.UUID, now time.Time) error {
	s, err := e.load(ctx, id, now)
	if err != nil {
		return fmt.Errorf("load advisor snapshot %s: %w", id, err)
	}
	rules := All()
	ran := make([]string, 0, len(rules))
	seen := []string{}
	var errs []error
	for _, r := range rules {
		ran = append(ran, r.ID())
		findings, panicked := e.evaluateRule(r, s)
		if panicked {
			ran = ran[:len(ran)-1]
		}
		for i := range findings {
			f := findings[i]
			if f.RuleID == "" {
				f.RuleID = r.ID()
			}
			if f.InstanceID == nil {
				f.InstanceID = &s.InstanceID
			}
			if f.ClusterID == nil && s.ClusterID != 0 {
				v := s.ClusterID
				f.ClusterID = &v
			}
			if f.Scope == "" {
				f.Scope = r.Scope()
			}
			if f.Severity == "" {
				f.Severity = r.Severity()
			}
			if f.ID() == r.ID()+"/" {
				f.ObjectName = id.String()
			}
			// A finding that fails to persist is still recorded as seen and
			// the pass continues: aborting here skipped ResolveAbsent, so
			// findings that had genuinely gone away were never resolved and
			// stayed on the dashboard forever. Keeping it in `seen` also
			// stops ResolveAbsent from resolving a finding that does exist
			// and merely failed to be written this cycle.
			if err := e.store.UpsertFinding(ctx, f, now); err != nil {
				errs = append(errs, fmt.Errorf("persist finding %s: %w", f.ID(), err))
			}
			seen = append(seen, f.ID())
		}
	}
	if err := e.store.ResolveAbsent(ctx, s.InstanceID, ran, seen, now); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (e *Engine) evaluateRule(r Rule, s *Snapshot) (out []Finding, panicked bool) {
	defer func() {
		if v := recover(); v != nil {
			log.Printf("advisor rule %s panicked: %v", r.ID(), v)
			panicked = true
			out = []Finding{{RuleID: r.ID(), Severity: r.Severity(), State: StateDegraded, Scope: r.Scope(), Title: "Advisor rule failed", Detail: "rule evaluation panicked", DegradedReason: "rule evaluation failed"}}
		}
	}()
	if s.PermTier < r.MinTier() {
		return []Finding{{RuleID: r.ID(), Severity: r.Severity(), State: StateDegraded, Scope: r.Scope(), Title: "Permission tier unavailable", Detail: "advisor rule requires a higher permission tier", DegradedReason: "required permission tier " + r.MinTier().String()}}, false
	}
	for _, need := range r.Needs() {
		if missingSnapshotInput(s, need) {
			reason := "missing input: " + need
			if skipped, ok := s.SkippedChecks[need]; ok && strings.TrimSpace(skipped) != "" {
				reason = skipped
			}
			return []Finding{{RuleID: r.ID(), Severity: r.Severity(), State: StateDegraded, Scope: r.Scope(), Title: "Advisor input unavailable", Detail: "required advisor input is unavailable: " + reason, DegradedReason: reason}}, false
		}
	}
	return r.Evaluate(s), false
}

func missingSnapshotInput(s *Snapshot, need string) bool {
	if reason, skipped := s.SkippedChecks[need]; skipped && strings.TrimSpace(reason) != "" {
		return true
	}
	switch need {
	case "Statements":
		return s.Statements == nil
	case "Indexes":
		return s.Indexes == nil
	case "Tables":
		return s.Tables == nil
	case "Bloat":
		return s.Bloat == nil
	case "Siblings":
		return s.Siblings == nil
	case "Baseline":
		return s.Baseline == nil
	case "host":
		return !s.Host.Available
	case "settings":
		return s.Settings == nil
	}
	if strings.HasPrefix(need, "pg_") {
		_, ok := s.Metrics[need]
		return !ok
	}
	return false
}

func advisorInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("PGLENS_ADVISOR_INTERVAL"))
	if raw == "" {
		return defaultAdvisorInterval
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return defaultAdvisorInterval
}
