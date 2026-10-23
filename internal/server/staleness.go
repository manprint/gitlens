package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/store"
)

// Staleness implements the self-monitoring staleness evaluator described in
// phase_04.md §3.6. It runs every 15s on an injectable clock, updates
// pglens_up and pglens_agent_last_seen_seconds, and emits transition events
// once per transition while holding a session-scoped advisory lock so two
// replicas never emit duplicate events.

const (
	evaluatorInterval  = 15 * time.Second
	noPrimaryThreshold = 60 * time.Second
	// slotInactiveThreshold matches noPrimaryThreshold's own precedent in
	// this file: a short, hardcoded default rather than an env-configurable
	// one — nothing else in Staleness exposes its thresholds for override
	// either (SYS-SLOT-001, phase_07.md#6.4).
	slotInactiveThreshold = 30 * time.Second
	// slotStateWindow bounds the slot rows evaluateSlots reads.
	slotStateWindow = 5 * time.Minute
	// events had no retention: every reset, truncation, skip and staleness
	// transition stayed forever. The leader purges rows older than
	// eventRetention once per eventPurgeInterval, eventPurgeBatch rows at a
	// time, and keeps going on the next evaluation while a backlog remains.
	eventRetention     = 90 * 24 * time.Hour
	eventPurgeInterval = time.Hour
	eventPurgeBatch    = 10000
	// topologyEdgeLiveness is how long an instance may stay silent before the
	// topology edges it reports are dropped. A standby that stops reporting keeps
	// its edge to its upstream otherwise, and the topology readers and the
	// sync-state resolver would count that edge for good. It is the same liveness
	// the other readers of an instance's rows use.
	topologyEdgeLiveness = time.Hour
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

// checkKey names one check's counter series: the instance the check ran against
// and the check, with a name outside knownChecks already bucketed by checkLabel.
type checkKey struct {
	instance string
	check    string
}

// checkCounterVec is counterVec keyed by an instance and a check.
type checkCounterVec struct {
	mu   sync.Mutex
	vals map[checkKey]float64
}

func (c *checkCounterVec) Inc(key checkKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals[key]++
}

func (c *checkCounterVec) Get(key checkKey) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vals[key]
}

// Snapshot is counterVec.Snapshot for a checkCounterVec.
func (c *checkCounterVec) Snapshot() map[checkKey]float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[checkKey]float64, len(c.vals))
	for k, v := range c.vals {
		out[k] = v
	}
	return out
}

// knownChecks are the check names an agent reports: the checks it registers and
// the two results it builds itself. The agent supplies the name, under a token
// every agent shares, so an unknown name is bucketed rather than kept: otherwise
// each distinct name would hold a series in memory and in /metrics for the life
// of the process, and a name that is not valid text would break the scrape.
var knownChecks = map[string]bool{
	"activity":              true,
	"agent_self":            true,
	"archiver":              true,
	"ash":                   true,
	"bloat_estimate":        true,
	"checkpointer":          true,
	"database_stats":        true,
	"host":                  true,
	"index_stats":           true,
	"instance_info":         true,
	"io":                    true,
	"locks":                 true,
	"monitoring_role":       true,
	"replication_receiver":  true,
	"replication_slots":     true,
	"replication_streaming": true,
	"settings":              true,
	"stat_statements":       true,
	"table_stats":           true,
	"vacuum_progress":       true,
	"wal":                   true,
}

// checkOther is the check label of every name outside knownChecks.
const checkOther = "other"

// checkLabel is the check label a reported check name is counted under.
func checkLabel(check string) string {
	if knownChecks[check] {
		return check
	}
	return checkOther
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
	pglensCheckErrorTotal           = &checkCounterVec{vals: make(map[checkKey]float64)}
	pglensCheckOKTotal              = &checkCounterVec{vals: make(map[checkKey]float64)}
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

// IncCheckError records one failed check scrape of an instance, keyed by the
// instance and the check name — the visibility gap that let stat_statements
// fail every single scrape, invisibly, until it was found by chance
// re-verifying a README (STATE.md §4 rows 141-142). wire.Result.Error was
// always sent by the agent but never once read by the server before this.
func IncCheckError(instanceID, check string) {
	pglensCheckErrorTotal.Inc(checkKey{instance: instanceID, check: checkLabel(check)})
}

// IncCheckOK records one successful check scrape of an instance, keyed by the
// instance and the check name. The pipeline counts a skipped scrape as neither.
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
func IncCheckOK(instanceID, check string) {
	pglensCheckOKTotal.Inc(checkKey{instance: instanceID, check: checkLabel(check)})
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
	pool  *pgxpool.Pool
	clock clock.Clock

	mu sync.Mutex
	// instanceUp tracks last emitted up state per instance_id string.
	instanceUp map[string]bool
	// announcedLoaded is set once instanceUp has been rebuilt from the events of
	// the current leadership term. A replica that takes the lock starts from what
	// the events show as announced, not from whatever it held before.
	announcedLoaded bool
	// clusterPrimarySeen tracks last time a primary was seen per cluster.
	clusterPrimarySeen map[string]time.Time
	// clusterNoPrimaryEmitted tracks whether no_primary_in_cluster already emitted.
	clusterNoPrimaryEmitted map[string]bool
	// slotInactiveSince/slotInactiveEmitted track, per "instance_id/slot_name"
	// key, when a slot was first observed inactive and whether slot_inactive
	// has already been emitted for that spell of inactivity.
	slotInactiveSince   map[string]time.Time
	slotInactiveEmitted map[string]bool
	// lastEventPurge/eventPurgeBacklog pace purgeEvents.
	lastEventPurge    time.Time
	eventPurgeBacklog bool

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
// Pool and clock may be nil in tests; clock defaults to system clock.
func NewStaleness(pool *pgxpool.Pool, clk clock.Clock) *Staleness {
	if clk == nil {
		clk = clock.System()
	}
	return &Staleness{
		pool:                    pool,
		clock:                   clk,
		instanceUp:              make(map[string]bool),
		clusterPrimarySeen:      make(map[string]time.Time),
		clusterNoPrimaryEmitted: make(map[string]bool),
		slotInactiveSince:       make(map[string]time.Time),
		slotInactiveEmitted:     make(map[string]bool),
		stopCh:                  make(chan struct{}),
		doneCh:                  make(chan struct{}),
	}
}

// threshold is how long an instance may stay silent before it counts as down.
// It is the value the alert rules read too, so the two cannot disagree.
func (s *Staleness) threshold() time.Duration {
	return store.InstanceStaleAfter
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
	//
	// IsClosed turns true only after something has failed on the session, and a
	// leader does no I/O on its pinned session while it holds the lock. So the
	// session is pinged every cycle: one that does not answer no longer holds the
	// lock, and the leadership goes with it.
	if s.conn != nil && s.conn.Conn().IsClosed() {
		s.releaseLeaderConn(ctx)
	}
	if s.hasLock && s.conn != nil {
		if err := s.conn.Ping(ctx); err == nil {
			return nil
		}
		s.releaseLeaderConn(ctx)
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
		s.releaseLeaderConn(ctx)
		return fmt.Errorf("try advisory lock: %w", err)
	}
	if locked && !s.hasLock {
		s.announcedLoaded = false
	}
	s.hasLock = locked
	return nil
}

// releaseLeaderConn drops the lock-holding connection and the leadership it
// carried. Caller must hold s.mu.
//
// The session is closed, not returned to the pool: a session that may still
// hold the advisory lock would keep it for whoever borrowed the connection
// next, and this replica has given up the lock.
func (s *Staleness) releaseLeaderConn(ctx context.Context) {
	if s.conn != nil {
		closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = s.conn.Conn().Close(closeCtx)
		cancel()
		s.conn.Release()
		s.conn = nil
	}
	s.hasLock = false
}

// staleEvent is one event an evaluation writes.
type staleEvent struct {
	typ        string
	clusterID  *int64
	instanceID *uuid.UUID
	payload    map[string]any
}

// emitEvent inserts a single event row. Caller must hold s.mu if it needs the
// transition guarantee, but this helper does not acquire the mutex itself for
// the DB operation to avoid holding it across network I/O; callers should
// manage locking as needed. It always checks errors.
func (s *Staleness) emitEvent(ctx context.Context, typ string, clusterID *int64, instanceID *uuid.UUID, payload map[string]any) error {
	return s.emitEvents(ctx, staleEvent{typ: typ, clusterID: clusterID, instanceID: instanceID, payload: payload})
}

// emitEvents writes events together or not at all. A transition is such a
// group, and its state is recorded as done only once every event of it is
// stored: a write that fails part way leaves the state as it was, so the next
// evaluation writes the whole transition again.
func (s *Staleness) emitEvents(ctx context.Context, events ...staleEvent) error {
	if s.pool == nil {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin events tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ts := s.clock.Now()
	for _, ev := range events {
		if err := insertStaleEvent(ctx, tx, ts, ev); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit events: %w", err)
	}
	return nil
}

// insertStaleEvent writes one event row inside tx.
func insertStaleEvent(ctx context.Context, tx pgx.Tx, ts time.Time, ev staleEvent) error {
	var payloadJSON []byte
	var err error
	if ev.payload != nil {
		payloadJSON, err = json.Marshal(ev.payload)
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
	} else {
		payloadJSON = []byte(`{}`)
	}
	var cidParam any
	if ev.clusterID != nil {
		cidParam = *ev.clusterID
	}
	var iidParam any
	if ev.instanceID != nil {
		iidParam = *ev.instanceID
	}
	tenantID := "default"
	_, execErr := tx.Exec(ctx,
		`INSERT INTO events (tenant_id, ts, type, cluster_id, instance_id, payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`,
		tenantID, ts, ev.typ, cidParam, iidParam, string(payloadJSON))
	if execErr != nil {
		return fmt.Errorf("insert event %s: %w", ev.typ, execErr)
	}
	return nil
}

// transitionEvents are the events an instance's move to up or to down writes:
// the agent event and the instance event that the alert rules read.
func transitionEvents(clusterID int64, instanceID uuid.UUID, up bool, lastSeen time.Time) []staleEvent {
	cid := clusterID
	iid := instanceID
	iidStr := instanceID.String()
	if up {
		return []staleEvent{
			{typ: "agent_up", clusterID: &cid, instanceID: &iid, payload: map[string]any{"instance_id": iidStr}},
			{typ: "instance_reachable", clusterID: &cid, instanceID: &iid, payload: map[string]any{"instance_id": iidStr}},
		}
	}
	return []staleEvent{
		{typ: "agent_down", clusterID: &cid, instanceID: &iid, payload: map[string]any{"instance_id": iidStr, "last_seen": lastSeen}},
		{typ: "instance_unreachable", clusterID: &cid, instanceID: &iid, payload: map[string]any{"instance_id": iidStr}},
	}
}

// announcedStates returns, for each instance with an agent event on record,
// whether its latest agent transition was up. The events are what has been
// announced, whichever replica wrote them.
func (s *Staleness) announcedStates(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (instance_id) instance_id, type FROM events WHERE type IN ('agent_up', 'agent_down') AND instance_id IS NOT NULL ORDER BY instance_id, ts DESC`)
	if err != nil {
		return nil, fmt.Errorf("query agent events: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var iid uuid.UUID
		var typ string
		if scanErr := rows.Scan(&iid, &typ); scanErr != nil {
			return nil, fmt.Errorf("scan agent event: %w", scanErr)
		}
		out[iid.String()] = typ == "agent_up"
	}
	return out, rows.Err()
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

	now := s.clock.Now()
	threshold := s.threshold()

	rows, err := s.pool.Query(ctx, `SELECT i.instance_id, i.cluster_id, i.last_seen, i.role, i.agent_id, a.revoked_at IS NOT NULL FROM instances i LEFT JOIN agents a ON a.agent_id = i.agent_id`)
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
		revoked   bool
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
		var revoked bool
		if scanErr := rows.Scan(&iid, &cid, &lastSeen, &role, &agentID, &revoked); scanErr != nil {
			return fmt.Errorf("scan instance: %w", scanErr)
		}
		instances = append(instances, instInfo{id: iid, clusterID: cid, lastSeen: lastSeen, role: role, agentID: agentID, revoked: revoked})
		// A revoked agent's instances are out of monitoring by design. The primary
		// one reported is not a primary its cluster has lost, so it neither makes
		// the cluster a subject of the no-primary check nor counts as its primary.
		if revoked {
			continue
		}
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

	// Every replica reports the gauges from the database, not only the leader. A
	// former leader would otherwise keep the values of its last turn for
	// instances that have gone down since, and a replica that never led would
	// expose none. Only the leader emits events below.
	for _, inst := range instances {
		iidStr := inst.id.String()
		up := isStaleUp(inst.lastSeen, now, threshold)
		pglensUpGauge.Set(iidStr, boolToFloat(up))
		pglensAgentLastSeenGauge.Set(iidStr, float64(inst.lastSeen.Unix()))
	}
	if !hasLock {
		return nil
	}

	// Emit transitions under lock to ensure once-per-transition.
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.announcedLoaded {
		announced, err := s.announcedStates(ctx)
		if err != nil {
			// An empty map here would announce every instance that is down again.
			return fmt.Errorf("load announced instance states: %w", err)
		}
		// Rebuilt, not merged: the map of an earlier term may be older than the events.
		s.instanceUp = make(map[string]bool, len(instances))
		for _, inst := range instances {
			iidStr := inst.id.String()
			if up, ok := announced[iidStr]; ok {
				s.instanceUp[iidStr] = up
			} else if inst.lastSeen.Before(now.Add(-eventRetention)) {
				// Silent for longer than the events are kept, so the agent_down it
				// was announced with has been purged. It is down, and was announced.
				s.instanceUp[iidStr] = false
			}
		}
		s.announcedLoaded = true
	}

	for _, inst := range instances {
		iidStr := inst.id.String()
		up := isStaleUp(inst.lastSeen, now, threshold)

		// A revoked agent stops pushing by design, so its silence is not an
		// outage: it emits no agent_down or instance_unreachable, as the metric
		// path in alert/source_sql.go already treats it. The state is still kept
		// so that a later transition is measured from the true state.
		if inst.revoked {
			s.instanceUp[iidStr] = up
			continue
		}

		// The state of an instance moves only once the events of the move are
		// stored. Recorded first, a failed write would be taken for done, and
		// its events would never be written.
		prevUp, seenBefore := s.instanceUp[iidStr]
		if seenBefore && prevUp == up {
			continue
		}
		// An instance seen for the first time emits only when it is already down.
		if !seenBefore && up {
			s.instanceUp[iidStr] = up
			continue
		}
		if emitErr := s.emitEvents(ctx, transitionEvents(inst.clusterID, inst.id, up, inst.lastSeen)...); emitErr != nil {
			return emitErr
		}
		s.instanceUp[iidStr] = up
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

	if topoErr := s.purgeTopologyEdges(ctx, now); topoErr != nil {
		return topoErr
	}

	return s.purgeEvents(ctx, now)
}

// purgeTopologyEdges deletes the edges reported by an instance that has been
// silent for topologyEdgeLiveness. Only the edges an instance reports are
// dropped, not the edges that point at it: a live standby's edge to a silent
// primary is still its upstream. An instance that comes back writes its edges
// again with its next envelope. Called from Evaluate with s.mu held, by the
// leader only.
func (s *Staleness) purgeTopologyEdges(ctx context.Context, now time.Time) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM topology_edges e USING instances i
		WHERE i.instance_id = e.from_instance AND i.last_seen < $1`, now.Add(-topologyEdgeLiveness)); err != nil {
		return fmt.Errorf("purge topology edges: %w", err)
	}
	return nil
}

// purgeEvents deletes one batch of events older than eventRetention, at most
// once per eventPurgeInterval unless the previous batch was full. Called
// from Evaluate() with s.mu already held.
func (s *Staleness) purgeEvents(ctx context.Context, now time.Time) error {
	if !s.eventPurgeBacklog && !s.lastEventPurge.IsZero() && now.Sub(s.lastEventPurge) < eventPurgeInterval {
		return nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM events WHERE event_id IN (
		SELECT event_id FROM events WHERE ts < $1 LIMIT $2)`, now.Add(-eventRetention), eventPurgeBatch)
	if err != nil {
		return fmt.Errorf("purge events: %w", err)
	}
	s.lastEventPurge = now
	s.eventPurgeBacklog = tag.RowsAffected() == eventPurgeBatch
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

// slotKey identifies one slot of one instance in the slot state maps.
func slotKey(instanceID uuid.UUID, slotName string) string {
	return instanceID.String() + "/" + slotName
}

// decideSlotInactiveEmissions applies the slot_inactive state machine to one
// evaluation's rows, mutating since/emitted in place (the same maps
// evaluateSlots persists across calls as Staleness.slotInactiveSince/
// slotInactiveEmitted) and returning the emissions this call newly triggers.
// Deliberately pure otherwise — no DB, no clock read — so it is testable
// directly at L1 without a database, unlike the rest of this file (Staleness
// holds a concrete *pgxpool.Pool for its advisory-lock Acquire, which rules
// out a mock-pool L1 test for the surrounding I/O).
//
// A slot missing from rows (dropped, or its instance gone) loses its state:
// it used to be kept for the life of the process.
func decideSlotInactiveEmissions(rows []slotRow, now time.Time, since map[string]time.Time, emitted map[string]bool) []slotEmission {
	var out []slotEmission
	present := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		present[slotKey(r.InstanceID, r.SlotName)] = struct{}{}
	}
	for key := range since {
		if _, ok := present[key]; !ok {
			delete(since, key)
		}
	}
	for key := range emitted {
		if _, ok := present[key]; !ok {
			delete(emitted, key)
		}
	}
	for _, r := range rows {
		key := slotKey(r.InstanceID, r.SlotName)
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
//
// Only slots reported in the last five minutes are current (the check runs
// every 15 seconds). The query had no time bound: every 15 seconds it read
// the whole retained hypertable, decompressing 30 days of chunks, and kept a
// dropped slot's last state forever.
func (s *Staleness) evaluateSlots(ctx context.Context, now time.Time) error {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT ON (instance_id, slot_name) instance_id, cluster_id, slot_name, slot_active
FROM metrics_replication
WHERE ts >= $1 AND slot_name <> '' AND slot_active IS NOT NULL
  AND `+store.ReplicationSourceSQL+` = 'slot'
ORDER BY instance_id, slot_name, ts DESC`, now.Add(-slotStateWindow))
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

	emissions := decideSlotInactiveEmissions(slotRows, now, s.slotInactiveSince, s.slotInactiveEmitted)
	for i, em := range emissions {
		cidCopy := em.ClusterID
		iidCopy := em.InstanceID
		if emitErr := s.emitEvent(ctx, "slot_inactive", &cidCopy, &iidCopy, map[string]any{"slot_name": em.SlotName}); emitErr != nil {
			// decide marked this emission and the ones after it as reported, but
			// they were not written. Unmark them, so the next evaluation emits them.
			for _, rest := range emissions[i:] {
				delete(s.slotInactiveEmitted, slotKey(rest.InstanceID, rest.SlotName))
			}
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
					if err := s.Evaluate(evalCtx); err != nil {
						log.Printf("staleness: evaluate: %v", err)
					}
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
			if _, err := s.conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext('pglens:staleness'))`); err != nil {
				// The lock may still be held by this session, which must not
				// go back to the pool: close it, so the server frees the lock.
				s.releaseLeaderConn(unlockCtx)
			} else {
				s.conn.Release()
				s.conn = nil
			}
			cancel()
		}
		s.hasLock = false
		if s.ticker != nil {
			s.ticker.Stop()
		}
	})
}
