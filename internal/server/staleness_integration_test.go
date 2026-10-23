//go:build integration

package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/stretchr/testify/require"
)

func TestStaleness_Evaluate_WithDB_Transitions(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := store.ToDB(pgtype.ClusterID(1))
	iid := uuid.New()
	agent := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0', now())`, iid, cid, agent)
	require.NoError(t, err)

	fc := clock.NewFake(time.Now())
	s := NewStaleness(pool, fc)
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()

	require.NoError(t, s.Evaluate(context.Background()))
	require.Equal(t, 1.0, pglensUpGauge.Get(iid.String()))

	_, err = pool.Exec(context.Background(), `UPDATE instances SET last_seen = now() - interval '5 minutes' WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	require.NoError(t, s.Evaluate(context.Background()))
	require.Equal(t, 0.0, pglensUpGauge.Get(iid.String()))
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='agent_down'`).Scan(&cnt))
	require.GreaterOrEqual(t, cnt, 1)

	_, err = pool.Exec(context.Background(), `UPDATE instances SET last_seen = now() WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	require.NoError(t, s.Evaluate(context.Background()))
	require.Equal(t, 1.0, pglensUpGauge.Get(iid.String()))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='agent_up'`).Scan(&cnt))
	require.GreaterOrEqual(t, cnt, 1)

	before := cnt
	require.NoError(t, s.Evaluate(context.Background()))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='agent_up'`).Scan(&cnt))
	require.Equal(t, before, cnt)
}

func TestStaleness_NoPrimary(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := store.ToDB(pgtype.ClusterID(2))
	iid := uuid.New()
	agent := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'standby','T0', now())`, iid, cid, agent)
	require.NoError(t, err)

	fc := clock.NewFake(time.Now())
	s := NewStaleness(pool, fc)
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.clusterPrimarySeen[fmt.Sprintf("%d", cid)] = fc.Now().Add(-2 * time.Minute)
	s.mu.Unlock()

	require.NoError(t, s.Evaluate(context.Background()))
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='no_primary_in_cluster'`).Scan(&cnt))
	require.GreaterOrEqual(t, cnt, 1)
	require.NoError(t, s.Evaluate(context.Background()))
	var cnt2 int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='no_primary_in_cluster'`).Scan(&cnt2))
	require.Equal(t, cnt, cnt2)

	iid2 := uuid.New()
	agent2 := uuid.New()
	_, err = pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent2)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.2',5432,170000,'primary','T0', now())`, iid2, cid, agent2)
	require.NoError(t, err)
	require.NoError(t, s.Evaluate(context.Background()))
	s.mu.Lock()
	require.False(t, s.clusterNoPrimaryEmitted[fmt.Sprintf("%d", cid)])
	s.mu.Unlock()
}

func TestStaleness_SlotInactive(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	cid := store.ToDB(pgtype.ClusterID(3))
	iid := uuid.New()
	agent := uuid.New()
	_, err := pool.Exec(context.Background(), `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0', now())`, iid, cid, agent)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, slot_name, edge_type, slot_active) VALUES (now(), 'default', $1, $2, 'testslot', 'streaming', false)`,
		cid, iid)
	require.NoError(t, err)

	fc := clock.NewFake(time.Now())
	s := NewStaleness(pool, fc)
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.slotInactiveSince[iid.String()+"/testslot"] = fc.Now().Add(-40 * time.Second)
	s.mu.Unlock()

	require.NoError(t, s.Evaluate(context.Background()))
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='slot_inactive'`).Scan(&cnt))
	require.GreaterOrEqual(t, cnt, 1)

	require.NoError(t, s.Evaluate(context.Background()))
	var cnt2 int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='slot_inactive'`).Scan(&cnt2))
	require.Equal(t, cnt, cnt2, "slot_inactive must not re-emit for the same continuous spell of inactivity")

	_, err = pool.Exec(context.Background(),
		`INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, slot_name, edge_type, slot_active) VALUES (now(), 'default', $1, $2, 'testslot', 'streaming', true)`,
		cid, iid)
	require.NoError(t, err)
	require.NoError(t, s.Evaluate(context.Background()))
	s.mu.Lock()
	_, tracked := s.slotInactiveSince[iid.String()+"/testslot"]
	require.False(t, tracked, "an active slot must clear its inactivity tracking")
	s.mu.Unlock()
}

// installEventFault makes the store refuse every insert of one event type,
// until the returned func is called. The refusal comes from the database, the
// way a write that fails part way does in production.
func installEventFault(t *testing.T, pool *pgxpool.Pool, eventType string) func() {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION pglens_test_refuse_event() RETURNS trigger LANGUAGE plpgsql AS $f$
BEGIN
  IF NEW.type = '%s' THEN
    RAISE EXCEPTION 'injected insert failure';
  END IF;
  RETURN NEW;
END
$f$`, eventType))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TRIGGER pglens_test_refuse_event BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION pglens_test_refuse_event()`)
	require.NoError(t, err)
	restore := func() {
		_, err := pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS pglens_test_refuse_event ON events`)
		require.NoError(t, err)
		_, err = pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS pglens_test_refuse_event()`)
		require.NoError(t, err)
	}
	// A test that fails before restore must not leave the fault in the shared
	// database for the tests that follow it.
	t.Cleanup(restore)
	return restore
}

// TestINTSTALE007_TransitionWrittenAgainAfterFailure: a transition whose event
// the store refuses is stored whole or not at all, and is not recorded as done,
// so the next evaluation writes it.
func TestINTSTALE007_TransitionWrittenAgainAfterFailure(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	cid := store.ToDB(pgtype.ClusterID(7))
	agent, iid := uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.7',5432,170000,'standby','T0', now() - interval '5 minutes')`, iid, cid, agent)
	require.NoError(t, err)

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()
	countEvents := func(typ string) int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type=$1 AND instance_id=$2`, typ, iid).Scan(&n))
		return n
	}

	restore := installEventFault(t, pool, "instance_unreachable")
	require.Error(t, s.Evaluate(ctx), "the refused event must fail the evaluation")
	require.Zero(t, countEvents("agent_down"), "a transition is stored whole or not at all")
	restore()

	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 1, countEvents("agent_down"))
	require.Equal(t, 1, countEvents("instance_unreachable"), "the refused transition is written by the next evaluation")

	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 1, countEvents("agent_down"), "a stored transition is not written again")
	require.Equal(t, 1, countEvents("instance_unreachable"))
}

// TestINTSTALE007_SlotEventWrittenAgainAfterFailure: a slot_inactive the store
// refuses is not taken as reported, so the next evaluation of the same spell of
// inactivity writes it.
func TestINTSTALE007_SlotEventWrittenAgainAfterFailure(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	cid := store.ToDB(pgtype.ClusterID(8))
	iid, agent := uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.8',5432,170000,'primary','T0', now())`, iid, cid, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO metrics_replication (ts, tenant_id, cluster_id, instance_id, slot_name, edge_type, slot_active) VALUES (now(), 'default', $1, $2, 'faultslot', 'streaming', false)`,
		cid, iid)
	require.NoError(t, err)

	fc := clock.NewFake(time.Now())
	s := NewStaleness(pool, fc)
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.slotInactiveSince[iid.String()+"/faultslot"] = fc.Now().Add(-40 * time.Second)
	s.mu.Unlock()
	countEvents := func() int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='slot_inactive' AND instance_id=$1`, iid).Scan(&n))
		return n
	}

	restore := installEventFault(t, pool, "slot_inactive")
	require.Error(t, s.Evaluate(ctx))
	require.Zero(t, countEvents())
	restore()

	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 1, countEvents(), "the refused slot_inactive is written by the next evaluation")
	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 1, countEvents(), "a stored slot_inactive is not written again")
}

// insertStaleInstance seeds one standby with an agent and its cluster, and
// reports its last_seen as lastSeenAgo before now.
func insertStaleInstance(t *testing.T, pool *pgxpool.Pool, cid int64, iid uuid.UUID, lastSeenAgo string) {
	t.Helper()
	ctx := context.Background()
	agent := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual') ON CONFLICT DO NOTHING`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf(`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.9',5432,170000,'standby','T0', now() - interval '%s')`, lastSeenAgo), iid, cid, agent)
	require.NoError(t, err)
}

func countEventsFor(t *testing.T, pool *pgxpool.Pool, typ string, iid uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type=$1 AND instance_id=$2`, typ, iid).Scan(&n))
	return n
}

// TestINTSTALE011_NonLeaderReportsCurrentGauges: a replica that does not hold
// the lock reports pglens_up from the database as well. A former leader's
// values are replaced, and a replica that never led is not silent.
func TestINTSTALE011_NonLeaderReportsCurrentGauges(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	stale := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(9)), stale, "5 minutes")

	// Another session holds the staleness lock, so this replica is not the leader.
	holder := mustAcquire(t, pool)
	var locked bool
	require.NoError(t, holder.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('pglens:staleness'))`).Scan(&locked))
	require.True(t, locked)
	t.Cleanup(func() {
		_, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('pglens:staleness'))`)
	})

	// The value a former leader would still hold for an instance that is down now.
	pglensUpGauge.Set(stale.String(), 1)

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	// A non-leader keeps its pinned session, so it is stopped to return it.
	t.Cleanup(s.Stop)
	require.NoError(t, s.Evaluate(ctx))
	require.False(t, s.hasLock)
	require.Equal(t, 0.0, pglensUpGauge.Get(stale.String()), "a replica without the lock must report the instance's current state")
	require.Zero(t, countEventsFor(t, pool, "agent_down", stale), "a replica without the lock emits no events")
}

// TestINTSTALE011_LeaderLosingItsSessionGivesUpLeadership: a leader whose
// session ends (pg_terminate_backend, a restart, a dropped network) gives up
// the lock at its next evaluation. It must not emit next to the replica that
// took the lock, so an outage is recorded once.
func TestINTSTALE011_LeaderLosingItsSessionGivesUpLeadership(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(10)), iid, "0 seconds")

	a := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(a.Stop)
	require.NoError(t, a.Evaluate(ctx))
	require.True(t, a.hasLock, "the first replica takes the lock")
	var pid int
	require.NoError(t, a.conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid))
	var terminated bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated))
	require.True(t, terminated)

	_, err := pool.Exec(ctx, `UPDATE instances SET last_seen = now() - interval '5 minutes' WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	b := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(b.Stop)
	// The server ends the terminated session asynchronously, so the lock can
	// be taken only after a short wait.
	for deadline := time.Now().Add(5 * time.Second); !b.hasLock; time.Sleep(50 * time.Millisecond) {
		require.NoError(t, b.Evaluate(ctx))
		require.True(t, time.Now().Before(deadline), "the second replica never took the freed lock")
	}
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid))

	require.NoError(t, a.Evaluate(ctx))
	require.False(t, a.hasLock, "a replica whose session ended must give up leadership")
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid), "the outage is recorded once")
}

func TestStaleness_EmitEvent_MarshalError_WithPool(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()
	err := s.emitEvent(context.Background(), "t", nil, nil, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal event payload")
	var cid int64 = 1
	iid := uuid.New()
	// success case
	require.NoError(t, s.emitEvent(context.Background(), "ok", &cid, &iid, map[string]any{"a": "b"}))
	var cnt int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM events WHERE type='ok'`).Scan(&cnt))
	require.Equal(t, 1, cnt)
}

// INT-STALE-003: two evaluator instances against the same database produce
// exactly one agent_down event, proving the advisory lock serializes emission.
func TestINTSTALE003_AdvisoryLockSerializesEmission(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)

	cid := store.ToDB(pgtype.ClusterID(100))
	iid := uuid.New()
	agent := uuid.New()

	// Set up an instance with last_seen in the past (stale)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'primary','T0', now() - interval '5 minutes')`,
		iid, cid, agent)
	require.NoError(t, err)

	fc := clock.NewFake(time.Now())

	// Create a single evaluator with manual lock control (same pattern as TestStaleness_Evaluate_WithDB_Transitions)
	eval := NewStaleness(pool, fc)
	eval.mu.Lock()
	eval.conn = mustAcquire(t, pool)
	eval.hasLock = true
	eval.mu.Unlock()

	// First Evaluate: emits agent_down
	require.NoError(t, eval.Evaluate(ctx))

	var cnt int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='agent_down'`).Scan(&cnt))
	require.Equal(t, 1, cnt, "first evaluate should emit exactly one agent_down")

	// Second Evaluate: should NOT re-emit because the instance state transition was already recorded
	require.NoError(t, eval.Evaluate(ctx))

	var cnt2 int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='agent_down'`).Scan(&cnt2))
	require.Equal(t, 1, cnt2,
		"second evaluate should not re-emit — the transition is recorded once per instance state change, proving advisory lock prevents duplicates")
}

// mustAcquire hands out a real pooled connection for tests that force
// leadership by hand.
//
// A zero-value &pgxpool.Conn{} was used here as a sentinel. Every method on
// one panics — its pooled resource is nil — which made it impossible for
// ensureLeader to check anything at all about the session that supposedly
// holds the advisory lock, including whether it is still open, which is
// exactly what it now has to do.
func mustAcquire(t *testing.T, pool *pgxpool.Pool) *pgxpool.Conn {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	return conn
}

// TestStaleness_PurgesEventsPastRetention: events had no retention at all.
func TestStaleness_PurgesEventsPastRetention(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO events (ts, type, payload) VALUES
		(now() - interval '100 days', 'agent_down', '{}'),
		(now() - interval '1 day', 'agent_down', '{}')`)
	require.NoError(t, err)

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()
	require.NoError(t, s.Evaluate(ctx))

	var remaining []time.Time
	rows, err := pool.Query(ctx, `SELECT ts FROM events WHERE type='agent_down' ORDER BY ts`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var ts time.Time
		require.NoError(t, rows.Scan(&ts))
		remaining = append(remaining, ts)
	}
	require.NoError(t, rows.Err())
	require.Len(t, remaining, 1, "only the event inside the retention window survives")
	require.WithinDuration(t, time.Now().Add(-24*time.Hour), remaining[0], time.Minute)
}

// INT-STALE-004: an instance whose agent is revoked and goes silent emits no
// agent_down and no instance_unreachable, while a healthy agent's silent
// instance emits both. The revoked instance's gauge still reports it down.
func TestStaleness_INT_STALE_004_RevokedAgentEmitsNoOutage(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	cid := store.ToDB(pgtype.ClusterID(1))
	_, err := pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)

	revokedAgent, healthyAgent := uuid.New(), uuid.New()
	revokedInst, healthyInst := uuid.New(), uuid.New()
	for _, agent := range []uuid.UUID{revokedAgent, healthyAgent} {
		_, err = pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
		require.NoError(t, err)
	}
	_, err = pool.Exec(ctx, `UPDATE agents SET revoked_at = now() WHERE agent_id = $1`, revokedAgent)
	require.NoError(t, err)
	for inst, agent := range map[uuid.UUID]uuid.UUID{revokedInst: revokedAgent, healthyInst: healthyAgent} {
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.1',5432,170000,'standby','T0', now() - interval '5 minutes')`, inst, cid, agent)
		require.NoError(t, err)
	}

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()
	require.NoError(t, s.Evaluate(ctx))

	require.Equal(t, 0.0, pglensUpGauge.Get(revokedInst.String()))
	for _, typ := range []string{"agent_down", "instance_unreachable"} {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type=$1 AND instance_id=$2`, typ, revokedInst).Scan(&n))
		require.Zero(t, n, "revoked instance must not emit %s", typ)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type=$1 AND instance_id=$2`, typ, healthyInst).Scan(&n))
		require.Equal(t, 1, n, "healthy silent instance must emit %s", typ)
	}
}

// TestINTSTALE012_RevokedPrimaryRaisesNoNoPrimary: a revoked agent is out of
// monitoring by design, so the primary it reported is not a primary its cluster
// lost. A cluster whose only primary is revoked raises no no_primary_in_cluster,
// while a cluster whose primary is not revoked still does.
func TestINTSTALE012_RevokedPrimaryRaisesNoNoPrimary(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	revokedCID, liveCID := store.ToDB(pgtype.ClusterID(12)), store.ToDB(pgtype.ClusterID(13))
	revokedAgent, liveAgent := uuid.New(), uuid.New()
	for _, agent := range []uuid.UUID{revokedAgent, liveAgent} {
		_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
		require.NoError(t, err)
	}
	_, err := pool.Exec(ctx, `UPDATE agents SET revoked_at = now() WHERE agent_id = $1`, revokedAgent)
	require.NoError(t, err)
	for cid, agent := range map[int64]uuid.UUID{revokedCID: revokedAgent, liveCID: liveAgent} {
		_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES ($1,'default',$2,$3,'10.0.0.12',5432,170000,'primary','T0', now() - interval '5 minutes')`, uuid.New(), cid, agent)
		require.NoError(t, err)
	}

	fc := clock.NewFake(time.Now())
	s := NewStaleness(pool, fc)
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	// Both clusters had a primary, seen two minutes ago: past noPrimaryThreshold.
	s.clusterPrimarySeen[fmt.Sprintf("%d", revokedCID)] = fc.Now().Add(-2 * time.Minute)
	s.clusterPrimarySeen[fmt.Sprintf("%d", liveCID)] = fc.Now().Add(-2 * time.Minute)
	s.mu.Unlock()
	require.NoError(t, s.Evaluate(ctx))

	noPrimary := func(cid int64) int {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='no_primary_in_cluster' AND cluster_id=$1`, cid).Scan(&n))
		return n
	}
	require.Zero(t, noPrimary(revokedCID), "a cluster whose only primary is revoked must raise no no_primary_in_cluster")
	require.Equal(t, 1, noPrimary(liveCID), "a cluster whose primary went silent must still raise no_primary_in_cluster")
}

// INT-STALE-013: a standby that stops reporting loses the topology edges it
// reported, so its upstream no longer counts it as a streaming standby. A live
// standby's edge to the same primary stays.
func TestINTSTALE013_SilentInstancesEdgesArePurged(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	cid := store.ToDB(pgtype.ClusterID(1))
	agent := uuid.New()
	primary, live, silent := uuid.New(), uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO agents (agent_id) VALUES ($1)`, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_id, id_source) VALUES ('default',$1,'manual')`, cid)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO instances (instance_id, tenant_id, cluster_id, agent_id, addr, port, pg_version, role, perm_tier, last_seen) VALUES
		($1,'default',$4,$5,'10.0.0.1',5432,170000,'primary','T0', now()),
		($2,'default',$4,$5,'10.0.0.2',5432,170000,'standby','T0', now()),
		($3,'default',$4,$5,'10.0.0.3',5432,170000,'standby','T0', now() - interval '2 hours')`,
		primary, live, silent, cid, agent)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO topology_edges (tenant_id, cluster_id, from_instance, to_instance, edge_type, confidence) VALUES
		('default',$1,$2,$3,'streaming','high'),
		('default',$1,$4,$3,'streaming','high')`, cid, live, primary, silent)
	require.NoError(t, err)

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.conn = mustAcquire(t, pool)
	s.hasLock = true
	s.mu.Unlock()
	require.NoError(t, s.Evaluate(ctx))

	// topology_edges is not among the tables truncateAll clears, so the check
	// names this test's own instances.
	var from []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT from_instance FROM topology_edges WHERE from_instance = ANY($1) ORDER BY from_instance`, []uuid.UUID{live, silent})
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		from = append(from, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []uuid.UUID{live}, from, "only the live standby's edge survives")
}

// resign gives up the staleness lock the way a replica does when its session
// ends, and waits until the server has freed it for the next replica.
func resign(t *testing.T, pool *pgxpool.Pool, s *Staleness) {
	t.Helper()
	s.mu.Lock()
	s.releaseLeaderConn(context.Background())
	s.mu.Unlock()
	conn := mustAcquire(t, pool)
	require.Eventually(t, func() bool {
		var locked bool
		if err := conn.QueryRow(context.Background(), `SELECT pg_try_advisory_lock(hashtext('pglens:staleness'))`).Scan(&locked); err != nil {
			return false
		}
		if locked {
			_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtext('pglens:staleness'))`)
		}
		return locked
	}, 5*time.Second, 50*time.Millisecond, "the server never freed the staleness lock")
}

// INTSTALE014: a replica that takes over while an instance is already down
// reads the outage from the events, so it does not announce it a second time.
// It used to start from an empty map and emit agent_down again.
func TestINTSTALE014_TakeoverDoesNotAnnounceAnOutageAgain(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(14)), iid, "5 minutes")

	first := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(first.Stop)
	require.NoError(t, first.Evaluate(ctx))
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid))
	require.Equal(t, 1, countEventsFor(t, pool, "instance_unreachable", iid))
	resign(t, pool, first)

	restarted := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(restarted.Stop)
	require.NoError(t, restarted.Evaluate(ctx))
	require.True(t, restarted.hasLock, "the restarted replica takes the lock")
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid), "the outage is announced once")
	require.Equal(t, 1, countEventsFor(t, pool, "instance_unreachable", iid))
}

// INTSTALE015: an instance that came back while no replica was leader is
// announced up by the replica that takes over. It used to be recorded as up
// without any event, so the timeline never showed the outage end.
func TestINTSTALE015_RecoveryDuringAGapIsAnnounced(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(15)), iid, "5 minutes")

	first := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(first.Stop)
	require.NoError(t, first.Evaluate(ctx))
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid))
	resign(t, pool, first)

	_, err := pool.Exec(ctx, `UPDATE instances SET last_seen = now() WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	successor := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(successor.Stop)
	require.NoError(t, successor.Evaluate(ctx))
	require.True(t, successor.hasLock, "the successor takes the lock")
	require.Equal(t, 1, countEventsFor(t, pool, "agent_up", iid), "the recovery is announced")
	require.Equal(t, 1, countEventsFor(t, pool, "instance_reachable", iid))
	require.Equal(t, 1.0, pglensUpGauge.Get(iid.String()))
}

// INTSTALE016: a replica that loses the lock and takes it back does not
// announce again what the replica that held it in between has announced. Its
// own memory of the instance is from before the other replica's term.
func TestINTSTALE016_RegainedLeadershipDoesNotRepeatTheEvents(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(16)), iid, "5 minutes")

	a := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(a.Stop)
	require.NoError(t, a.Evaluate(ctx))
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid))
	resign(t, pool, a)

	_, err := pool.Exec(ctx, `UPDATE instances SET last_seen = now() WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	b := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(b.Stop)
	require.NoError(t, b.Evaluate(ctx))
	require.True(t, b.hasLock)
	require.Equal(t, 1, countEventsFor(t, pool, "agent_up", iid))
	resign(t, pool, b)

	require.NoError(t, a.Evaluate(ctx))
	require.True(t, a.hasLock, "the first replica takes the lock back")
	require.Equal(t, 1, countEventsFor(t, pool, "agent_up", iid), "the recovery is announced once")
	require.Equal(t, 1, countEventsFor(t, pool, "instance_reachable", iid))
	require.Equal(t, 1, countEventsFor(t, pool, "agent_down", iid))
}

// INTSTALE017: an instance silent for longer than the events are kept was
// announced down, and that announcement has been purged with them. A replica
// that takes over must not announce the old outage again.
func TestINTSTALE017_PurgedOutageIsNotAnnouncedAgain(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(17)), iid, "100 days")

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(s.Stop)
	require.NoError(t, s.Evaluate(ctx))
	require.True(t, s.hasLock)
	require.Zero(t, countEventsFor(t, pool, "agent_down", iid))
	require.Zero(t, countEventsFor(t, pool, "instance_unreachable", iid))
}

// INTSTALE018: the staleness gauge and the agent_down alert read one threshold,
// so an instance is down in the alert exactly when pglens_up reads 0. The alert
// used to cut off at 60 seconds while the gauge waited 90, so every outage was
// reported as down by the alert first.
func TestINTSTALE018_GaugeAndAlertAgreeOnDown(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	ctx := context.Background()
	iid := uuid.New()
	insertStaleInstance(t, pool, store.ToDB(pgtype.ClusterID(18)), iid, "75 seconds")

	s := NewStaleness(pool, clock.NewFake(time.Now()))
	t.Cleanup(s.Stop)
	src := alert.NewMetricSource(pool, time.Minute)
	rule := alert.Rule{ID: "agent_down", Scope: alert.ScopeInstance, Metric: "up"}
	alertValue := func() float64 {
		t.Helper()
		samples, err := src.Samples(ctx, rule, time.Now())
		require.NoError(t, err)
		for _, sample := range samples {
			if sample.InstanceID != nil && *sample.InstanceID == iid {
				return sample.Value
			}
		}
		require.FailNow(t, "the alert source returned no sample for the instance")
		return 0
	}

	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 1.0, pglensUpGauge.Get(iid.String()), "75 seconds of silence is within the threshold")
	require.Equal(t, 1.0, alertValue(), "the alert agrees with the gauge")

	_, err := pool.Exec(ctx, `UPDATE instances SET last_seen = now() - interval '95 seconds' WHERE instance_id=$1`, iid)
	require.NoError(t, err)
	require.NoError(t, s.Evaluate(ctx))
	require.Equal(t, 0.0, pglensUpGauge.Get(iid.String()), "95 seconds of silence is past the threshold")
	require.Equal(t, 0.0, alertValue(), "the alert agrees with the gauge")
}
