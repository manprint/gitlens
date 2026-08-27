//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestInventory_Upsert_InvalidClusterID_WithPool(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: uuid.NewString(), ClusterID: "not-a-uint64", ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.1", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[env.Instances[0].InstanceID].Error(), "invalid cluster_id")
}

func TestInventory_Upsert_InvalidInstanceID_WithPool(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: "bad-uuid", ClusterID: pgtype.ClusterID(123).String(), ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.1", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[env.Instances[0].InstanceID].Error(), "invalid instance_id")
}

func TestInventory_Upsert_RoleNormalization(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	iid := uuid.NewString()
	cid := pgtype.ClusterID(9999).String()
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.2", Port: 5432, Role: "weird_role", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// verify role normalized to unknown in DB
	var role string
	err = pool.QueryRow(context.Background(), `SELECT role FROM instances WHERE instance_id=$1`, uuid.MustParse(iid)).Scan(&role)
	require.NoError(t, err)
	require.Equal(t, "unknown", role)

	// primary preserved
	iid2 := uuid.NewString()
	env2 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid2, ClusterID: pgtype.ClusterID(8888).String(), ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.3", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err = inv.Upsert(context.Background(), env2)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	err = pool.QueryRow(context.Background(), `SELECT role FROM instances WHERE instance_id=$1`, uuid.MustParse(iid2)).Scan(&role)
	require.NoError(t, err)
	require.Equal(t, "primary", role)
}

func TestInventory_Upsert_DatabasesAndSkipReason(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	iid := uuid.NewString()
	cid := pgtype.ClusterID(7777).String()
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{
				InstanceID: iid, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual),
				Addr: "10.0.0.4", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0",
				Databases: []wire.Database{
					{Name: "postgres", Monitored: true},
					{Name: "app", Monitored: false, SkipReason: "no perms"},
				},
			},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// verify databases
	var monitored bool
	var skip *string
	err = pool.QueryRow(context.Background(), `SELECT monitored, skip_reason FROM databases WHERE instance_id=$1 AND datname='app'`, uuid.MustParse(iid)).Scan(&monitored, &skip)
	require.NoError(t, err)
	require.False(t, monitored)
	require.NotNil(t, skip)
	require.Equal(t, "no perms", *skip)
	// nullableString "" => nil already tested via app with skip, but also test empty skip yields null
	err = pool.QueryRow(context.Background(), `SELECT monitored, skip_reason FROM databases WHERE instance_id=$1 AND datname='postgres'`, uuid.MustParse(iid)).Scan(&monitored, &skip)
	require.NoError(t, err)
	require.True(t, monitored)
	require.Nil(t, skip)
}

func TestInventory_UpsertCluster_IdentifierOverManual(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(12345)
	cidDB := int64(cid)
	cidStr := cid.String()
	instID := uuid.New()
	// first insert with system_identifier
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceSystemIdentifier), instID))
	var src string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&src))
	require.Equal(t, string(pgtype.IDSourceSystemIdentifier), src)
	// second with manual should not overwrite and should emit event
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceManual), instID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&src))
	require.Equal(t, string(pgtype.IDSourceSystemIdentifier), src)
	// verify event emitted
	var evCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='cluster_identity_conflict'`).Scan(&evCount))
	require.GreaterOrEqual(t, evCount, 1)
}

func TestInventory_UpsertCluster_ManualToIdentifier(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(54321)
	cidDB := int64(cid)
	cidStr := cid.String()
	instID := uuid.New()
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceManual), instID))
	var src string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&src))
	require.Equal(t, string(pgtype.IDSourceManual), src)
	// upgrade to system_identifier should update
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceSystemIdentifier), instID))
	require.NoError(t, pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&src))
	require.Equal(t, string(pgtype.IDSourceSystemIdentifier), src)
}

func TestInventory_UpsertCluster_SameSourceNoop(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(11111)
	cidDB := int64(cid)
	cidStr := cid.String()
	instID := uuid.New()
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceManual), instID))
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, string(pgtype.IDSourceManual), instID))
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&count))
	require.Equal(t, 1, count)
}

func TestInventory_UpsertCluster_InvalidSourceDefaultsManual(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(22222)
	cidDB := int64(cid)
	cidStr := cid.String()
	instID := uuid.New()
	require.NoError(t, inv.upsertCluster(ctx, "default", cidDB, cidStr, "weird_source", instID))
	var src string
	require.NoError(t, pool.QueryRow(ctx, `SELECT id_source FROM clusters WHERE cluster_id=$1`, cidDB).Scan(&src))
	require.Equal(t, string(pgtype.IDSourceManual), src)
}

func TestInventory_DuplicateInstance_Suspected(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	cid := pgtype.ClusterID(33333).String()
	addr := "10.0.0.5"
	port := 5432
	iid1 := uuid.NewString()
	env1 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid1, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: addr, Port: port, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(ctx, env1)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// second instance same addr/port different uuid within 5 minutes should emit duplicate_instance_suspected
	iid2 := uuid.NewString()
	env2 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid2, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: addr, Port: port, Role: "standby", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err = inv.Upsert(ctx, env2)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	var dupCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='duplicate_instance_suspected'`).Scan(&dupCount))
	require.GreaterOrEqual(t, dupCount, 1)
}

func TestInventory_ClusterIDChanged_EmitsEvent(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	iid := uuid.NewString()
	cid1 := pgtype.ClusterID(44444).String()
	cid2 := pgtype.ClusterID(55555).String()
	env1 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid, ClusterID: cid1, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.6", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(ctx, env1)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	env2 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid, ClusterID: cid2, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.6", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err = inv.Upsert(ctx, env2)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[iid].Error(), "cluster_id_changed")
	var ev int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='cluster_id_changed'`).Scan(&ev))
	require.Equal(t, 1, ev)
	// stored cluster_id unchanged
	var storedCID int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT cluster_id FROM instances WHERE instance_id=$1`, uuid.MustParse(iid)).Scan(&storedCID))
	require.Equal(t, int64(pgtype.ClusterID(44444)), storedCID)
}

func TestInventory_RoleChange_EmitsEvent(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	iid := uuid.NewString()
	cid := pgtype.ClusterID(66666).String()
	env1 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.7", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	_, err := inv.Upsert(ctx, env1)
	require.NoError(t, err)
	env2 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.7", Port: 5432, Role: "standby", PGVersion: 170000, PermTier: "T0"},
		},
	}
	_, err = inv.Upsert(ctx, env2)
	require.NoError(t, err)
	var ev int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='role_change'`).Scan(&ev))
	require.Equal(t, 1, ev)
	// unknown -> primary should not emit? Only primary<->standby
	iid2 := uuid.NewString()
	cid2 := pgtype.ClusterID(77777).String()
	env3 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid2, ClusterID: cid2, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.8", Port: 5432, Role: "unknown", PGVersion: 170000, PermTier: "T0"},
		},
	}
	_, err = inv.Upsert(ctx, env3)
	require.NoError(t, err)
	env4 := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: iid2, ClusterID: cid2, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.8", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	_, err = inv.Upsert(ctx, env4)
	require.NoError(t, err)
	// should still be 1 total role_change (previous one), not 2, because unknown->primary not counted
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE type='role_change'`).Scan(&ev))
	require.Equal(t, 1, ev)
}

func TestInventory_EmitEvent_JSON(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	payload := map[string]any{"instance_id": "x", "existing_cluster_id": "1", "attempted_cluster_id": "2"}
	cid := int64(1)
	iid := uuid.New()
	require.NoError(t, inv.emitEvent(ctx, "default", time.Now(), "test_event", &cid, &iid, payload))
	var stored []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM events WHERE type='test_event'`).Scan(&stored))
	var out map[string]any
	require.NoError(t, json.Unmarshal(stored, &out))
	require.Equal(t, "x", out["instance_id"])
	// nil payload => {}
	require.NoError(t, inv.emitEvent(ctx, "default", time.Now(), "test_event2", nil, nil, nil))
	require.NoError(t, pool.QueryRow(ctx, `SELECT payload FROM events WHERE type='test_event2'`).Scan(&stored))
	require.Equal(t, "{}", string(stored))
}

func TestInventory_Upsert_MultipleInstancesIndependent(t *testing.T) {
	pool := getTestPool(t)
	truncateAll(t, pool)
	inv := NewInventory(pool)
	ctx := context.Background()
	goodIID := uuid.NewString()
	badIID := "bad-uuid"
	cid := pgtype.ClusterID(88888).String()
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: goodIID, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.9", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
			{InstanceID: badIID, ClusterID: cid, ClusterIDSource: string(pgtype.IDSourceManual), Addr: "10.0.0.10", Port: 5432, Role: "primary", PGVersion: 170000, PermTier: "T0"},
		},
	}
	res, err := inv.Upsert(ctx, env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[badIID].Error(), "invalid instance_id")
	// good one persisted
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM instances WHERE instance_id=$1)`, uuid.MustParse(goodIID)).Scan(&exists))
	require.True(t, exists)
}

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return getSharedPool(t)
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `TRUNCATE events, databases, instances, clusters, agents, metrics, metrics_statements, metrics_ash, metrics_replication, query_texts CASCADE`)
}

var _ = fmt.Sprintf
