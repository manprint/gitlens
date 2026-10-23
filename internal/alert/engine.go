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

// Clearer is implemented by an event source. Cleared returns the episodes of
// rule whose condition no longer holds, for the rules whose event alone does
// not say (see spellEvents). An error means none of them can be confirmed.
type Clearer interface {
	Cleared(context.Context, Rule, []Alert) ([]Alert, error)
}

// spellEvents are the event types whose condition outlives the event that
// opened it. Each is emitted once, when the condition begins, so once the
// event leaves the lookback window nothing shows whether the condition still
// holds. Their episodes close only when a Clearer confirms it. Every other
// event type is a pulse, and its episode closes after the lookback passes
// with no new occurrence; split_brain_detected is re-emitted while it persists,
// well within that window.
var spellEvents = map[string]bool{
	"instance_unreachable":  true,
	"no_primary_in_cluster": true,
	"slot_inactive":         true,
}

// Notifier receives an alert transition. Delivery de-duplication belongs to
// the persistence-backed notifier implementation.
type Notifier interface {
	Notify(context.Context, Alert) error
}

// Filter is the query selector used by the alert store.
//
// Resolved alerts are left out unless the filter asks for them, either by
// naming the resolved state or by setting IncludeResolved. The engine's
// restore relies on the empty filter excluding them.
type Filter struct {
	RuleID          string
	State           State
	Severity        Severity
	ClusterID       *int64
	InstanceID      string
	IncludeResolved bool
	// Limit caps the rows returned, newest first. Zero means no cap.
	Limit int
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
	stopped     bool
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
		// Stop() before Start() already closed doneCh; starting the loop now
		// would close it a second time and panic. Nothing to run either way:
		// stopCh is closed, so the loop would exit on its first select.
		if e.stopped {
			e.mu.Unlock()
			return
		}
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
		e.stopped = true
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

// releaseLeaderConn drops the lock-holding connection and the leadership it
// carried. Caller must hold e.mu.
func (e *Engine) releaseLeaderConn() {
	if e.conn != nil {
		e.conn.Release()
		e.conn = nil
	}
	e.hasLock = false
}

func (e *Engine) ensureLeader(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool == nil {
		e.hasLock = true
		return nil
	}
	// The advisory lock lives on this session, so leadership dies with the
	// connection. A closed conn was previously indistinguishable from a live
	// one here (hasLock && conn != nil short-circuited), which meant a
	// server that lost its session — PostgreSQL restart, network blip,
	// termination by an admin — kept evaluating rules forever believing it
	// was the leader, while another server legitimately took the freed lock.
	// Two leaders means duplicate notifications for every alert.
	if e.conn != nil && e.conn.Conn().IsClosed() {
		e.releaseLeaderConn()
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
		// Give the connection back instead of holding a broken one: keeping
		// it pinned meant every later tick retried on the same dead session
		// and the engine could never regain leadership without a restart.
		e.releaseLeaderConn()
		return fmt.Errorf("try alert advisory lock: %w", err)
	}
	e.hasLock = locked
	if locked {
		// A new term starts from the alerts the previous leader left behind.
		// The episodes this engine held in its last term may have been closed
		// or reopened since, so they are dropped and Tick reloads the table.
		e.eval.Reset()
		e.stateLoaded = false
	}
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
	// stateLoaded was read and written with no lock at all. Tick is exported
	// and driven directly by the integration suites as well as by the
	// engine's own ticker, so two callers could both see it false and both
	// replay every active alert into the evaluator.
	e.mu.Lock()
	loaded := e.stateLoaded
	e.mu.Unlock()
	now := e.clock.Now()
	if !loaded {
		if e.store != nil {
			active, err := e.store.Active(ctx, Filter{})
			if err != nil {
				return fmt.Errorf("restore active alerts: %w", err)
			}
			for _, a := range active {
				e.eval.Restore(a, now)
			}
		}
		e.mu.Lock()
		e.stateLoaded = true
		e.mu.Unlock()
	}
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
	// Per-rule failures are collected, not propagated immediately: one rule
	// whose query is broken (a metric that does not exist, a permission
	// error, a statement timeout) used to abort the whole cycle at the point
	// it failed, so every rule after it in the list — including every
	// critical one — silently stopped being evaluated for as long as the bad
	// rule stayed enabled. The same goes for a single alert failing to
	// persist or to notify.
	// live is the rule set of this pass. An episode whose rule is not in it
	// was configured earlier, and no later sample will ever close it.
	live := make(map[string]struct{}, len(rules))
	grace := lookback(e.interval)
	var errs []error
	for _, rule := range rules {
		live[rule.ID] = struct{}{}
		observed, failed := 0, false
		for _, source := range e.sources {
			if (rule.EventType != "" && source.Kind() != "event") || (rule.EventType == "" && source.Kind() != "metric") {
				continue
			}
			samples, err := source.Samples(ctx, rule, now)
			if err != nil {
				errs = append(errs, fmt.Errorf("sample rule %s: %w", rule.ID, err))
				failed = true
				if ctx.Err() != nil {
					return errors.Join(errs...)
				}
				continue
			}
			observed++
			for _, sample := range samples {
				undo := e.eval.Checkpoint(Key(rule, sample))
				alertValue, transition := e.eval.Step(rule, sample, now)
				if alertValue != nil && transition == TransitionNone && alertValue.State == StateFiring {
					errs = append(errs, e.refresh(ctx, *alertValue)...)
					continue
				}
				if alertValue == nil || transition == TransitionNone || transition == TransitionOpened || transition == TransitionCancelled {
					continue
				}
				errs = append(errs, e.publish(ctx, *alertValue, silences, now, undo)...)
				if ctx.Err() != nil {
					return errors.Join(append(errs, ctx.Err())...)
				}
			}
		}
		// A subject that a complete pass did not sample has left the rule's
		// data. A source that failed, or a rule no source served, says nothing
		// about its subjects, so only a fully observed rule may close them.
		if observed > 0 && !failed {
			errs = append(errs, e.endUnsampled(ctx, &rule, silences, now, e.eval.Unsampled(func(id string) bool { return id == rule.ID }, now.Add(-grace)))...)
		}
	}
	errs = append(errs, e.endUnsampled(ctx, nil, silences, now, e.eval.Unsampled(func(id string) bool {
		_, ok := live[id]
		return !ok
	}, now))...)
	e.eval.Forget(now.Add(-time.Hour))
	return errors.Join(errs...)
}

// endUnsampled closes the episodes that a pass did not confirm. rule is the
// configured rule of those episodes, or nil when the rule is gone, and then
// they close at once. A spell episode closes only when its source confirms
// that the condition cleared: its event aged out of the window, but the
// condition may well still hold. If the check fails, nothing closes.
func (e *Engine) endUnsampled(ctx context.Context, rule *Rule, silences []Silence, now time.Time, unsampled []Alert) []error {
	if len(unsampled) == 0 {
		return nil
	}
	closing := unsampled
	if rule != nil && spellEvents[rule.EventType] {
		cleared, err := e.clearedBy(ctx, *rule, unsampled)
		if err != nil {
			return []error{fmt.Errorf("check cleared %s: %w", rule.ID, err)}
		}
		closing = cleared
	}
	var errs []error
	for _, a := range closing {
		undo := e.eval.Checkpoint(a.Key)
		if resolved, transition := e.eval.End(a.Key, now); transition == TransitionResolved && resolved != nil {
			errs = append(errs, e.publish(ctx, *resolved, silences, now, undo)...)
		}
	}
	return errs
}

// clearedBy asks the event source whether any of the episodes has cleared.
// With no source able to confirm, nothing has cleared.
func (e *Engine) clearedBy(ctx context.Context, rule Rule, episodes []Alert) ([]Alert, error) {
	for _, s := range e.sources {
		if c, ok := s.(Clearer); ok && s.Kind() == "event" {
			return c.Cleared(ctx, rule, episodes)
		}
	}
	return nil, nil
}

// refresh rewrites a firing episode that is still being evaluated, so that its
// stored value and last evaluation time follow the pass. Nothing is notified:
// the episode was notified when it fired.
func (e *Engine) refresh(ctx context.Context, a Alert) []error {
	if e.store == nil {
		return nil
	}
	if err := e.store.Upsert(ctx, a); err != nil {
		return []error{fmt.Errorf("refresh alert %s: %w", a.Key, err)}
	}
	return nil
}

// publish persists one transition and notifies it unless a silence covers the
// alert. A transition that cannot be persisted is undone and not notified: the
// next pass derives it again, and it is notified once it is on record, so a
// page is never sent for an alert the table does not show. Failures are
// returned, not raised, so one alert cannot stop the rest of the pass.
func (e *Engine) publish(ctx context.Context, a Alert, silences []Silence, now time.Time, undo func()) []error {
	var errs []error
	a.Suppressed = FirstMatch(silences, a, now) != nil
	if e.store != nil {
		if err := e.store.Upsert(ctx, a); err != nil {
			undo()
			return append(errs, fmt.Errorf("persist alert %s: %w", a.Key, err))
		}
	}
	if !a.Suppressed && e.notifier != nil {
		if err := e.notifier.Notify(ctx, a); err != nil {
			errs = append(errs, fmt.Errorf("notify alert %s: %w", a.Key, err))
		}
	}
	return errs
}
