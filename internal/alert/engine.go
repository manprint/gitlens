package alert

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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/clock"
)

const defaultEngineInterval = 30 * time.Second

// Source produces samples for rules of one kind.
type Source interface {
	Kind() string
	Samples(context.Context, Rule, time.Time) ([]Sample, error)
}

// Notifier receives an alert transition. Delivery de-duplication belongs to
// the persistence-backed notifier implementation.
type Notifier interface {
	Notify(context.Context, Alert) error
}

// Filter is the query selector used by the alert store.
type Filter struct {
	RuleID     string
	State      State
	Severity   Severity
	ClusterID  *int64
	InstanceID string
}

// Store is the persistence boundary for the alert engine.
type Store interface {
	Rules(context.Context) ([]Rule, error)
	Silences(context.Context, time.Time) ([]Silence, error)
	Upsert(context.Context, Alert) error
	ClaimNotification(context.Context, string, string, string, Alert) (bool, error)
	MarkNotification(context.Context, string, string, string, bool, int, error) error
	Active(context.Context, Filter) ([]Alert, error)
}

// Engine evaluates alert rules while holding a PostgreSQL advisory lock.
type Engine struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	interval time.Duration
	eval     *Evaluator
	store    Store
	notifier Notifier
	sources  []Source

	mu          sync.Mutex
	conn        *pgxpool.Conn
	hasLock     bool
	ticker      clock.Ticker
	stopCh      chan struct{}
	doneCh      chan struct{}
	stopOnce    sync.Once
	startOnce   sync.Once
	started     bool
	stateLoaded bool
}

func NewEngine(pool *pgxpool.Pool, clk clock.Clock, st Store, n Notifier, srcs []Source) *Engine {
	if clk == nil {
		clk = clock.System()
	}
	interval := defaultEngineInterval
	if raw := os.Getenv("PGLENS_ALERT_INTERVAL"); raw != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil && d > 0 {
			interval = d
		} else if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			interval = time.Duration(seconds) * time.Second
		}
	}
	return newEngine(pool, clk, interval, st, n, srcs)
}

func NewEngineWithInterval(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration, st Store, n Notifier, srcs []Source) *Engine {
	if clk == nil {
		clk = clock.System()
	}
	if interval <= 0 {
		interval = defaultEngineInterval
	}
	return newEngine(pool, clk, interval, st, n, srcs)
}

func newEngine(pool *pgxpool.Pool, clk clock.Clock, interval time.Duration, st Store, n Notifier, srcs []Source) *Engine {
	return &Engine{pool: pool, clock: clk, interval: interval, eval: NewEvaluator(), store: st, notifier: n, sources: srcs, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}

func (e *Engine) Start(ctx context.Context) {
	e.startOnce.Do(func() {
		e.mu.Lock()
		e.started = true
		e.ticker = e.clock.NewTicker(e.interval)
		ticker := e.ticker
		e.mu.Unlock()
		go func() {
			defer close(e.doneCh)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-e.stopCh:
					return
				case <-ticker.C():
					cycleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					if err := e.Tick(cycleCtx); err != nil {
						log.Printf("alert engine: %v", err)
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
		e.mu.Unlock()
		if started {
			<-e.doneCh
		} else {
			close(e.doneCh)
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = e.conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('pglens:alert-engine'))`)
			cancel()
			e.conn.Release()
			e.conn = nil
		}
		e.hasLock = false
		if e.ticker != nil {
			e.ticker.Stop()
		}
	})
}

func (e *Engine) ensureLeader(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool == nil {
		e.hasLock = true
		return nil
	}
	if e.hasLock && e.conn != nil {
		return nil
	}
	if e.conn == nil {
		conn, err := e.pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire conn for alert lock: %w", err)
		}
		e.conn = conn
	}
	var locked bool
	if err := e.conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:alert-engine'))`).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			e.hasLock = false
			return nil
		}
		return fmt.Errorf("try alert advisory lock: %w", err)
	}
	e.hasLock = locked
	return nil
}

func (e *Engine) Tick(ctx context.Context) error {
	if err := e.ensureLeader(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	leader := e.hasLock
	e.mu.Unlock()
	if !leader {
		return nil
	}
	if !e.stateLoaded {
		if e.store != nil {
			active, err := e.store.Active(ctx, Filter{})
			if err != nil {
				return fmt.Errorf("restore active alerts: %w", err)
			}
			for _, a := range active {
				e.eval.Restore(a)
			}
		}
		e.stateLoaded = true
	}
	now := e.clock.Now()
	rules := Builtin()
	if e.store != nil {
		stored, err := e.store.Rules(ctx)
		if err != nil {
			return fmt.Errorf("load alert rules: %w", err)
		}
		builtinIDs := make(map[string]struct{}, len(rules))
		for _, r := range rules {
			builtinIDs[r.ID] = struct{}{}
		}
		for _, r := range stored {
			if r.Enabled {
				if _, exists := builtinIDs[r.ID]; !exists {
					rules = append(rules, r)
				}
			}
		}
	}
	var silences []Silence
	if e.store != nil {
		var err error
		silences, err = e.store.Silences(ctx, now)
		if err != nil {
			return fmt.Errorf("load alert silences: %w", err)
		}
	}
	for _, rule := range rules {
		for _, source := range e.sources {
			if (rule.EventType != "" && source.Kind() != "event") || (rule.EventType == "" && source.Kind() != "metric") {
				continue
			}
			samples, err := source.Samples(ctx, rule, now)
			if err != nil {
				return fmt.Errorf("sample rule %s: %w", rule.ID, err)
			}
			for _, sample := range samples {
				alertValue, transition := e.eval.Step(rule, sample, now)
				if alertValue == nil || transition == TransitionNone || transition == TransitionOpened || transition == TransitionCancelled {
					continue
				}
				alertValue.Suppressed = FirstMatch(silences, *alertValue, now) != nil
				if e.store != nil {
					if err := e.store.Upsert(ctx, *alertValue); err != nil {
						return fmt.Errorf("persist alert %s: %w", alertValue.Key, err)
					}
				}
				if !alertValue.Suppressed && e.notifier != nil {
					if err := e.notifier.Notify(ctx, *alertValue); err != nil {
						return fmt.Errorf("notify alert %s: %w", alertValue.Key, err)
					}
				}
			}
		}
	}
	e.eval.Forget(now.Add(-time.Hour))
	return nil
}
