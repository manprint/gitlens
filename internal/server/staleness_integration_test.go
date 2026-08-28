//go:build integration

package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	s.hasLock = true
	s.conn = &pgxpool.Conn{}
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
	s.hasLock = true
	s.conn = &pgxpool.Conn{}
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
	s.hasLock = true
	s.conn = &pgxpool.Conn{}
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

func TestStaleness_EmitEvent_MarshalError_WithPool(t *testing.T) {
	pool := getSharedPool(t)
	truncateAll(t, pool)
	s := NewStaleness(pool, clock.NewFake(time.Now()))
	s.mu.Lock()
	s.hasLock = true
	s.conn = &pgxpool.Conn{}
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
