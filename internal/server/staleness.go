package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/clock"
)

// Staleness implements the self-monitoring staleness evaluator described in
// phase_04.md §3.6. It runs every 15s on an injectable clock, updates
// pglens_up and pglens_agent_last_seen_seconds, and emits transition events
// once per transition while holding a session-scoped advisory lock so two
// replicas never emit duplicate events.

const (
	defaultExpectedInterval = 30 * time.Second
	evaluatorInterval       = 15 * time.Second
	noPrimaryThreshold      = 60 * time.Second
	// slotInactiveThreshold matches noPrimaryThreshold's own precedent in
	// this file: a short, hardcoded default rather than an env-configurable
	// one — nothing else in Staleness exposes its thresholds for override
	// either (SYS-SLOT-001, phase_07.md#6.4).
	slotInactiveThreshold = 30 * time.Second
)

// ---------------------------------------------------------------------------
// Simple in-memory metric primitives (no external Prometheus dependency).
// They expose the counter/gauge names required by the spec and are used by
// ingest/pipeline as well as the evaluator. Values are kept in-process; a
// real Prometheus exposition would read from these globals.
// ---------------------------------------------------------------------------

type gaugeVec struct {
	mu   sync.Mutex
	vals map[string]float64
}

func newGaugeVec() *gaugeVec {
	return &gaugeVec{vals: make(map[string]float64)}
}

func (g *gaugeVec) Set(key string, v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vals[key] = v
}

func (g *gaugeVec) Get(key string) float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.vals[key]
}

func (g *gaugeVec) Add(key string, v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vals[key] += v
}

// Snapshot returns a copy of every key/value pair currently set — used by
// the /metrics exposition handler, which must not hold the vec's own lock
// while writing to an http.ResponseWriter.
func (g *gaugeVec) Snapshot() map[string]float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]float64, len(g.vals))
	for k, v := range g.vals {
		out[k] = v
	}
	return out
}

type counterVec struct {
	mu   sync.Mutex
	vals map[string]float64
}

func (c *counterVec) Inc(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals[key]++
}

func (c *counterVec) Add(key string, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals[key] += v
}

func (c *counterVec) Get(key string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vals[key]
}

// Snapshot returns a copy of every key/value pair currently counted — used
// by the /metrics exposition handler, which must not hold the vec's own
// lock while writing to an http.ResponseWriter.
func (c *counterVec) Snapshot() map[string]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]float64, len(c.vals))
	for k, v := range c.vals {
		out[k] = v
	}
	return out
}

type counter struct {
	mu sync.Mutex
	v  float64
}

func (c *counter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v++
}

func (c *counter) Add(v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v += v
}

func (c *counter) Get() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.v
}

type gauge struct {
	mu sync.Mutex
	v  float64
}

func (g *gauge) Set(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.v = v
}

func (g *gauge) Add(v float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.v += v
}

func (g *gauge) Get() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.v
}

// Metrics required by phase_04 §3.6. They are package-level so ingest and
// pipeline can increment them without importing a separate metrics package.

var (
	pglensUpGauge                   = newGaugeVec()
	pglensAgentLastSeenGauge        = newGaugeVec()
	pglensIngestEnvelopesTotal      = &counterVec{vals: make(map[string]float64)}
	pglensIngestRejectedTotal       = &counterVec{vals: make(map[string]float64)}
	pglensSamplesTooOldTotal        = &counter{}
	pglensAgentClockSkewSeconds     = &gauge{}
	pglensSeriesTotalGauge          = newGaugeVec()
	pglensCardinalityTruncatedTotal = &counter{}
	pglensCheckErrorTotal           = &counterVec{vals: make(map[string]float64)}
	pglensCheckOKTotal              = &counterVec{vals: make(map[string]float64)}
)

// Exported helpers for other packages (ingest, pipeline) to update self-monitoring
// counters. Keeping them here avoids a separate metrics package for phase 3.

func IncIngestEnvelopes(result string) {
	pglensIngestEnvelopesTotal.Inc(result)
}

func IncIngestRejected(reason string) {
	pglensIngestRejectedTotal.Inc(reason)
}

func IncSamplesTooOld() {
	pglensSamplesTooOldTotal.Inc()
}

func ObserveAgentClockSkew(seconds float64) {
	pglensAgentClockSkewSeconds.Set(seconds)
}

func SetSeriesTotal(instanceID string, v float64) {
	pglensSeriesTotalGauge.Set(instanceID, v)
}

func IncCardinalityTruncated() {
	pglensCardinalityTruncatedTotal.Inc()
}

// IncCheckError records one failed check scrape, keyed by check name — the
// visibility gap that let stat_statements fail every single scrape,
// invisibly, until it was found by chance re-verifying a README (STATE.md
// §4 rows 141-142). wire.Result.Error was always sent by the agent but
// never once read by the server before this.
func IncCheckError(check string) {
	pglensCheckErrorTotal.Inc(check)
}

// IncCheckOK records one successful check scrape, keyed by check name.
//
// The error counter alone cannot tell "this check is broken" from "this
// check's target was deliberately taken away for a moment": a failover, a
// network partition or a restart makes every check against that instance
// report one error, and so does a check that has never worked. Counting the
// successes too makes the difference decidable without a magic threshold — a
// check that errored and never once succeeded produced no data at all, which
// is exactly the shape of the stat_statements bug IncCheckError was added
// for, while a check that errored during a fault window and succeeded on
// either side of it is behaving correctly.
func IncCheckOK(check string) {
	pglensCheckOKTotal.Inc(check)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// isStaleUp reports whether lastSeen is within threshold of now, inclusive at the
// boundary (exactly threshold is still up, one second past is down).
func isStaleUp(lastSeen, now time.Time, threshold time.Duration) bool {
	return !now.After(lastSeen.Add(threshold))
}

// ---------------------------------------------------------------------------
// Staleness evaluator
// ---------------------------------------------------------------------------

// Staleness evaluates instance staleness and emits transition events under an
// advisory lock.
type Staleness struct {
	pool     *pgxpool.Pool
	clock    clock.Clock
	interval time.Duration

	mu sync.Mutex
	// instanceUp tracks last emitted up state per instance_id string.
	instanceUp map[string]bool
	// clusterPrimarySeen tracks last time a primary was seen per cluster.
	clusterPrimarySeen map[string]time.Time
	// clusterNoPrimaryEmitted tracks whether no_primary_in_cluster already emitted.
	clusterNoPrimaryEmitted map[string]bool
	// slotInactiveSince/slotInactiveEmitted track, per "instance_id/slot_name"
	// key, when a slot was first observed inactive and whether slot_inactive
	// has already been emitted for that spell of inactivity.
	slotInactiveSince   map[string]time.Time
	slotInactiveEmitted map[string]bool

	conn    *pgxpool.Conn
	hasLock bool

	ticker    clock.Ticker
	stopCh    chan struct{}
	doneCh    chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	started   bool
	stopped   bool
}

// NewStaleness creates a Staleness evaluator.
// Pool and clock may be nil in tests; clock defaults to system clock and
// interval defaults to 30s (threshold 90s).
func NewStaleness(pool *pgxpool.Pool, clk clock.Clock) *Staleness {
	if clk == nil {
		clk = clock.System()
	}
	return &Staleness{
		pool:                    pool,
		clock:                   clk,
		interval:                defaultExpectedInterval,
		instanceUp:              make(map[string]bool),
		clusterPrimarySeen:      make(map[string]time.Time),
		clusterNoPrimaryEmitted: make(map[string]bool),
		slotInactiveSince:       make(map[string]time.Time),
		slotInactiveEmitted:     make(map[string]bool),
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
	}
}

// threshold returns 3*expected_interval, defaulting to 90s.
func (s *Staleness) threshold() time.Duration {
	iv := s.interval
	if iv == 0 {
		iv = defaultExpectedInterval
	}
	return 3 * iv
}

// ensureLeader acquires a dedicated session-scoped connection and tries to
// hold the advisory lock. Two replicas must not both emit. The lock is held
// on s.conn for the lifetime of the evaluator.
func (s *Staleness) ensureLeader(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pool == nil {
		return nil
	}
	// The advisory lock lives on this session, so leadership dies with the
	// connection. A closed conn used to be indistinguishable from a live one
	// here (hasLock && conn != nil short-circuited), so a server that lost
	// its session — PostgreSQL restart, network blip, pg_terminate_backend —
	// kept emitting staleness events believing it was the leader, while
	// another server legitimately took the freed lock. Two leaders means
	// duplicate agent_down/instance_unreachable events.
	if s.conn != nil && s.conn.Conn().IsClosed() {
		s.releaseLeaderConn()
	}
	if s.hasLock && s.conn != nil {
		return nil
	}
	if s.conn == nil {
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire conn for staleness lock: %w", err)
		}
		s.conn = conn
	}
	var locked bool
	err := s.conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:staleness'))`).Scan(&locked)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.hasLock = false
			return nil
		}
		// Give the connection back instead of pinning a broken one: every
		// later cycle would otherwise retry on the same dead session, and
		// this evaluator could never regain leadership without a restart.
		s.releaseLeaderConn()
		return fmt.Errorf("try advisory lock: %w", err)
	}
	s.hasLock = locked
	return nil
}

// releaseLeaderConn drops the lock-holding connection and the leadership it
// carried. Caller must hold s.mu.
func (s *Staleness) releaseLeaderConn() {
	if s.conn != nil {
		s.conn.Release()
		s.conn = nil
	}
	s.hasLock = false
}

// emitEvent inserts a single event row. Caller must hold s.mu if it needs the
// transition guarantee, but this helper does not acquire the mutex itself for
// the DB operation to avoid holding it across network I/O; callers should
// manage locking as needed. It always checks errors.
func (s *Staleness) emitEvent(ctx context.Context, typ string, clusterID *int64, instanceID *uuid.UUID, payload map[string]any) error {
	if s.pool == nil {
		return nil
	}
	var payloadJSON []byte
	var err error
	if payload != nil {
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
	} else {
		payloadJSON = []byte(`{}`)
	}
	var cidParam any
	if clusterID != nil {
		cidParam = *clusterID
	}
	var iidParam any
	if instanceID != nil {
		iidParam = *instanceID
	}
	tenantID := "default"
	ts := s.clock.Now()
	_, execErr := s.pool.Exec(ctx,
		`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
		tenantID, ts, typ, cidParam, iidParam, string(payloadJSON))
	if execErr != nil {
		return fmt.Errorf("insert event %s: %w", typ, execErr)
	}
	return nil
}

// Evaluate performs a single staleness evaluation. It is safe to call
// concurrently, but leader election ensures only one replica emits.
func (s *Staleness) Evaluate(ctx context.Context) error {
	if s.pool == nil {
		return nil
	}
	if err := s.ensureLeader(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	hasLock := s.hasLock
	s.mu.Unlock()
	if !hasLock {
		return nil
	}

	now := s.clock.Now()
	threshold := s.threshold()

	rows, err := s.pool.Query(ctx, `SELECT instance_id, cluster_id, last_seen, role, agent_id FROM instances`)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("query instances: %w", err)
	}
	defer rows.Close()

	type instInfo struct {
		id        uuid.UUID
		clusterID int64
		lastSeen  time.Time
		role      string
		agentID   uuid.UUID
	}
	var instances []instInfo
	clusterHasPrimary := make(map[int64]bool)
	clusterIDs := make(map[int64]struct{})

	for rows.Next() {
		var iid uuid.UUID
		var cid int64
		var lastSeen time.Time
		var role string
		var agentID uuid.UUID
		if scanErr := rows.Scan(&iid, &cid, &lastSeen, &role, &agentID); scanErr != nil {
			return fmt.Errorf("scan instance: %w", scanErr)
		}
		instances = append(instances, instInfo{id: iid, clusterID: cid, lastSeen: lastSeen, role: role, agentID: agentID})
		clusterIDs[cid] = struct{}{}
		// A stale primary (its agent has gone silent — no update ever
		// retroactively changes `role` once that happens) must not count as
		// "the cluster has a primary": found live via SYS-REPL-003 (stop the
		// primary, never promote the standby), where this let
		// clusterHasPrimary stay permanently true off the dead instance's
		// last-known role, making no_primary_in_cluster structurally
		// unreachable for the exact case it exists to detect. The pre-existing
		// TestStaleness_NoPrimary only ever seeded a standby row, never a
		// stale primary one, so this never surfaced before.
		if role == "primary" && isStaleUp(lastSeen, now, threshold) {
			clusterHasPrimary[cid] = true
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("iterate instances: %w", rowsErr)
	}

	// Update gauges and emit transitions under lock to ensure once-per-transition.
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, inst := range instances {
		iidStr := inst.id.String()
		up := isStaleUp(inst.lastSeen, now, threshold)

		pglensUpGauge.Set(iidStr, boolToFloat(up))
		pglensAgentLastSeenGauge.Set(iidStr, float64(inst.lastSeen.Unix()))

		prevUp, seenBefore := s.instanceUp[iidStr]
		if !seenBefore {
			s.instanceUp[iidStr] = up
			if !up {
				// First observation already stale: emit down once.
				cid := inst.clusterID
				iidCopy := inst.id
				if emitErr := s.emitEvent(ctx, "agent_down", &cid, &iidCopy, map[string]any{"instance_id": iidStr, "last_seen": inst.lastSeen}); emitErr != nil {
					return emitErr
				}
				if emitErr := s.emitEvent(ctx, "instance_unreachable", &cid, &iidCopy, map[string]any{"instance_id": iidStr}); emitErr != nil {
					return emitErr
				}
			}
			continue
		}
		if prevUp != up {
			s.instanceUp[iidStr] = up
			cid := inst.clusterID
			iidCopy := inst.id
			if up {
				if emitErr := s.emitEvent(ctx, "agent_up", &cid, &iidCopy, map[string]any{"instance_id": iidStr}); emitErr != nil {
					return emitErr
				}
				if emitErr := s.emitEvent(ctx, "instance_reachable", &cid, &iidCopy, map[string]any{"instance_id": iidStr}); emitErr != nil {
					return emitErr
				}
			} else {
				if emitErr := s.emitEvent(ctx, "agent_down", &cid, &iidCopy, map[string]any{"instance_id": iidStr, "last_seen": inst.lastSeen}); emitErr != nil {
					return emitErr
				}
				if emitErr := s.emitEvent(ctx, "instance_unreachable", &cid, &iidCopy, map[string]any{"instance_id": iidStr}); emitErr != nil {
					return emitErr
				}
			}
		}
	}

	// no_primary_in_cluster handling
	for cid := range clusterIDs {
		cidStr := fmt.Sprintf("%d", cid)
		hasPrimary := clusterHasPrimary[cid]
		lastSeen, ok := s.clusterPrimarySeen[cidStr]
		if hasPrimary {
			s.clusterPrimarySeen[cidStr] = now
			s.clusterNoPrimaryEmitted[cidStr] = false
			_ = ok
			_ = lastSeen
		} else {
			if !ok || lastSeen.IsZero() {
				if !ok {
					s.clusterPrimarySeen[cidStr] = now
				}
				continue
			}
			if now.Sub(lastSeen) > noPrimaryThreshold && !s.clusterNoPrimaryEmitted[cidStr] {
				cidCopy := cid
				if emitErr := s.emitEvent(ctx, "no_primary_in_cluster", &cidCopy, nil, map[string]any{"cluster_id": cidStr}); emitErr != nil {
					return emitErr
				}
				s.clusterNoPrimaryEmitted[cidStr] = true
			}
		}
	}

	if slotErr := s.evaluateSlots(ctx, now); slotErr != nil {
		return slotErr
	}

	return nil
}

// slotRow is one (instance_id, slot_name)'s latest known state, as read by
// evaluateSlots.
type slotRow struct {
	InstanceID uuid.UUID
	ClusterID  int64
	SlotName   string
	Active     bool
}

// slotEmission is one slot_inactive event decideSlotInactiveEmissions has
// decided to emit.
type slotEmission struct {
	ClusterID  int64
	InstanceID uuid.UUID
	SlotName   string
}

// decideSlotInactiveEmissions applies the slot_inactive state machine to one
// evaluation's rows, mutating since/emitted in place (the same maps
// evaluateSlots persists across calls as Staleness.slotInactiveSince/
// slotInactiveEmitted) and returning the emissions this call newly triggers.
// Deliberately pure otherwise — no DB, no clock read — so it is testable
// directly at L1 without a database, unlike the rest of this file (Staleness
// holds a concrete *pgxpool.Pool for its advisory-lock Acquire, which rules
// out a mock-pool L1 test for the surrounding I/O).
func decideSlotInactiveEmissions(rows []slotRow, now time.Time, since map[string]time.Time, emitted map[string]bool) []slotEmission {
	var out []slotEmission
	for _, r := range rows {
		key := r.InstanceID.String() + "/" + r.SlotName
		if r.Active {
			delete(since, key)
			delete(emitted, key)
			continue
		}
		t, tracked := since[key]
		if !tracked {
			since[key] = now
			continue
		}
		if now.Sub(t) > slotInactiveThreshold && !emitted[key] {
			out = append(out, slotEmission{ClusterID: r.ClusterID, InstanceID: r.InstanceID, SlotName: r.SlotName})
			emitted[key] = true
		}
	}
	return out
}

// evaluateSlots emits slot_inactive once per continuous spell of inactivity
// for any replication slot (physical slots live on the upstream/primary side
// — pg_replication_slots is empty on a plain standby). Reads the latest
// known state per (instance_id, slot_name) from metrics_replication, which
// internal/check/replication_slots.go already populates every scrape cycle;
// no new check or wire field was needed, only this periodic scan (SYS-SLOT-001,
// phase_07.md#6.4). Called from Evaluate() with s.mu already held.
func (s *Staleness) evaluateSlots(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT ON (instance_id, slot_name) instance_id, cluster_id, slot_name, slot_active
FROM metrics_replication
WHERE slot_name <> '' AND slot_active IS NOT NULL
ORDER BY instance_id, slot_name, ts DESC`)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("query slot state: %w", err)
	}
	defer rows.Close()

	var slotRows []slotRow
	for rows.Next() {
		var r slotRow
		if scanErr := rows.Scan(&r.InstanceID, &r.ClusterID, &r.SlotName, &r.Active); scanErr != nil {
			return fmt.Errorf("scan slot state: %w", scanErr)
		}
		slotRows = append(slotRows, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, em := range decideSlotInactiveEmissions(slotRows, now, s.slotInactiveSince, s.slotInactiveEmitted) {
		cidCopy := em.ClusterID
		iidCopy := em.InstanceID
		if emitErr := s.emitEvent(ctx, "slot_inactive", &cidCopy, &iidCopy, map[string]any{"slot_name": em.SlotName}); emitErr != nil {
			return emitErr
		}
	}
	return nil
}

// Start launches the evaluator goroutine that runs every 15s on the injected
// clock. It is idempotent. The goroutine stops when ctx is cancelled or
// Stop is called.
func (s *Staleness) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		s.mu.Lock()
		// Already stopped: the loop below would exit on its first select
		// anyway, and starting it would leave Stop's own bookkeeping (which
		// has already run) describing a goroutine that outlived it.
		if s.stopped {
			s.mu.Unlock()
			return
		}
		ticker := s.clock.NewTicker(evaluatorInterval)
		s.ticker = ticker
		s.started = true
		s.mu.Unlock()
		go func() {
			defer close(s.doneCh)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.stopCh:
					return
				case t := <-ticker.C():
					_ = t
					evalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_ = s.Evaluate(evalCtx)
					cancel()
				}
			}
		}()
	})
}

// Stop stops the evaluator and releases the advisory lock connection.
// It is safe to call multiple times.
func (s *Staleness) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.mu.Lock()
		started := s.started
		s.stopped = true
		s.mu.Unlock()
		// Wait briefly for the goroutine to exit — but only if there is one.
		// Stopping an evaluator that was never started used to cost a flat
		// two-second sleep on every server shutdown that ran without a
		// database.
		if started {
			select {
			case <-s.doneCh:
			case <-time.After(2 * time.Second):
			}
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.conn != nil {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = s.conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext('pglens:staleness'))`)
			cancel()
			s.conn.Release()
			s.conn = nil
		}
		s.hasLock = false
		if s.ticker != nil {
			s.ticker.Stop()
		}
	})
}
