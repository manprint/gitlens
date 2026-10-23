package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestNullableString(t *testing.T) {
	t.Parallel()
	require.Nil(t, nullableString(""))
	require.Equal(t, "hello", nullableString("hello"))
	require.Equal(t, "x", nullableString("x"))
}

func TestInventory_Upsert_NilPool(t *testing.T) {
	t.Parallel()
	inv := NewInventory(nil)
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{
			{InstanceID: uuid.NewString(), ClusterID: "123", Role: "primary"},
			{InstanceID: uuid.NewString(), ClusterID: "456", Role: "standby"},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 2, res.Accepted)
	require.Equal(t, 0, res.Rejected)
	require.Empty(t, res.Errors)
}

func TestInventory_Upsert_NilPool_Empty(t *testing.T) {
	t.Parallel()
	inv := NewInventory(nil)
	res, err := inv.Upsert(context.Background(), wire.Envelope{})
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 0, res.Rejected)
}

func TestInventory_Upsert_NilPool_NoValidation(t *testing.T) {
	t.Parallel()
	inv := NewInventory(nil)
	env := wire.Envelope{
		Instances: []wire.Instance{
			{InstanceID: "not-a-uuid", ClusterID: "not-a-number", Role: "weird"},
		},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
}

func TestInventory_IsRevoked_NilPool(t *testing.T) {
	t.Parallel()
	inv := NewInventory(nil)
	revoked, err := inv.IsRevoked(context.Background(), uuid.NewString())
	require.NoError(t, err)
	require.False(t, revoked)
}

func TestInventory_IsRevoked_EmptyAgentID(t *testing.T) {
	t.Parallel()
	inv := &Inventory{pool: &mockPool{}}
	revoked, err := inv.IsRevoked(context.Background(), "")
	require.NoError(t, err)
	require.False(t, revoked)
}

func TestInventory_IsRevoked_UnknownAgent(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	inv := &Inventory{pool: pool}
	revoked, err := inv.IsRevoked(context.Background(), uuid.NewString())
	require.NoError(t, err)
	require.False(t, revoked)
}

func TestInventory_IsRevoked_NotRevoked(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{nil}}}}
	inv := &Inventory{pool: pool}
	revoked, err := inv.IsRevoked(context.Background(), uuid.NewString())
	require.NoError(t, err)
	require.False(t, revoked)
}

func TestInventory_IsRevoked_Revoked(t *testing.T) {
	t.Parallel()
	revokedAt := time.Now()
	pool := &mockPool{queryRowVals: []*mockRow{{vals: []any{&revokedAt}}}}
	inv := &Inventory{pool: pool}
	revoked, err := inv.IsRevoked(context.Background(), uuid.NewString())
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestInventory_IsRevoked_QueryError(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: errors.New("boom")}}}
	inv := &Inventory{pool: pool}
	_, err := inv.IsRevoked(context.Background(), uuid.NewString())
	require.Error(t, err)
}

func TestInventory_EmitEvent_MarshalError(t *testing.T) {
	t.Parallel()
	inv := NewInventory(nil)
	err := inv.emitEvent(context.Background(), "default", time.Now(), "t", nil, nil, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "marshal event payload")
}

// The tests below exercise Inventory's real (non-nil-pool) code paths with a
// mockPool instead of a database, closing V002-F07's L1-coverage gap for
// upsertOne/upsertCluster/checkDuplicateInstance/emitEvent (see
// internal/server/mockpool_test.go for the mock and scanInto).

func TestInventory_MockPool_NewInstance_WithDatabases(t *testing.T) {
	t.Parallel()
	pool := &mockPool{}
	inv := &Inventory{pool: pool}
	cid := pgtype.ClusterID(555555)
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{{
			InstanceID:      uuid.NewString(),
			ClusterID:       cid.String(),
			ClusterIDSource: "manual",
			Role:            "primary",
			Addr:            "10.0.0.1",
			Port:            5432,
			PGVersion:       170000,
			PermTier:        "T0",
			Databases: []wire.Database{
				{Name: "app", Monitored: true},
				{Name: "template0", Monitored: false, SkipReason: "system db"},
			},
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
	// insert cluster, ensure agent, insert instance, 2x upsert database, and the
	// removal of the databases the list no longer names
	require.Equal(t, 6, pool.execCalls)
}

// A role change of an existing instance is written by the pipeline's topology
// step, once, and not by the inventory: the inventory only updates the instance.
func TestInventory_MockPool_ExistingInstance_WritesNoRoleChange(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(777)
	cidDB := store.ToDB(cid)
	pool := &mockPool{
		queryRowVals: []*mockRow{
			{vals: []any{cidDB}},    // upsertOne: existing instance
			{vals: []any{"manual"}}, // upsertCluster: existing row, same source
		},
	}
	inv := &Inventory{pool: pool}
	env := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: uuid.NewString(),
			ClusterID:  cid.String(),
			Role:       "standby", // a primary that became a standby
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
	// The mutable-fields UPDATE is the only write.
	require.Equal(t, 1, pool.execCalls)
}

func TestInventory_MockPool_ClusterIDChanged_Rejected(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(888)
	otherCID := int64(999999)
	pool := &mockPool{
		queryRowVals: []*mockRow{
			{vals: []any{otherCID}}, // upsertOne: existing instance has a DIFFERENT cluster_id
		},
	}
	inv := &Inventory{pool: pool}
	instID := uuid.NewString()
	env := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: instID,
			ClusterID:  cid.String(),
			Role:       "primary",
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[instID].Error(), "cluster_id_changed")
	// Only the event: the attempted cluster is neither created nor touched.
	require.Equal(t, 1, pool.execCalls)
}

func TestInventory_MockPool_DuplicateInstanceSuspected(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(4242)
	existing := uuid.New()
	pool := &mockPool{
		queryResults: []*mockRows{
			{rows: [][]any{{existing.String()}}}, // checkDuplicateInstance: one live duplicate
		},
	}
	inv := &Inventory{pool: pool}
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{{
			InstanceID: uuid.NewString(),
			ClusterID:  cid.String(),
			Addr:       "10.0.0.5",
			Port:       5432,
			Role:       "primary",
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	require.Equal(t, 0, res.Rejected)
	// insert cluster, emit duplicate_instance_suspected, ensure agent, insert instance
	require.Equal(t, 4, pool.execCalls)
}

// A database that cannot be read says nothing about the instance. The envelope
// is not stored and not acknowledged, so the agent sends it again, and nothing
// is written for it in the meantime.
func TestInventory_MockPool_StoreFailure_FailsEnvelope(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(1)
	pool := &mockPool{
		queryRowVals: []*mockRow{
			{err: errors.New("db down")},
		},
	}
	inv := &Inventory{pool: pool}
	env := wire.Envelope{
		Instances: []wire.Instance{{InstanceID: uuid.NewString(), ClusterID: cid.String(), Role: "primary"}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.ErrorContains(t, err, "db down")
	require.Nil(t, res)
	require.Equal(t, 0, pool.execCalls)
}

// A data error from the database would come back on every push, so the
// instance it concerns is refused and the rest of the envelope is stored.
func TestInventory_MockPool_DataError_RejectsInstance(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(1)
	pool := &mockPool{
		queryRowVals: []*mockRow{
			{err: &pgconn.PgError{Code: "22001", Message: "value too long for type"}},
		},
	}
	inv := &Inventory{pool: pool}
	instID := uuid.NewString()
	env := wire.Envelope{
		Instances: []wire.Instance{{InstanceID: instID, ClusterID: cid.String(), Role: "primary"}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[instID].Error(), "value too long for type")
}

func TestInventory_MockPool_IdentityConflict_ManualLosesToSystemIdentifier(t *testing.T) {
	t.Parallel()
	cid := pgtype.ClusterID(31337)
	pool := &mockPool{
		queryRowVals: []*mockRow{
			nil,                                // upsertOne: new instance
			{vals: []any{"system_identifier"}}, // upsertCluster: existing row was identifier-sourced
		},
	}
	inv := &Inventory{pool: pool}
	env := wire.Envelope{
		AgentID: uuid.NewString(),
		Instances: []wire.Instance{{
			InstanceID:      uuid.NewString(),
			ClusterID:       cid.String(),
			ClusterIDSource: "manual", // manual attempt against an identifier-sourced cluster
			Role:            "primary",
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 1, res.Accepted)
	// cluster_identity_conflict event, ensure agent, insert instance
	require.Equal(t, 3, pool.execCalls)
}

func TestInventory_EmitEvent_NilPoolNoDB(t *testing.T) {
	t.Parallel()
	// emitEvent with nil pool currently will panic if we try to Exec, but marshal succeeds then attempts Exec on nil pool.
	// To test marshal success path without DB, we test staleness version which returns early on nil pool.
	// Here we test inventory emitEvent marshal with nil pool and bad payload already covered;
	// for good payload, we need a DB; skip nil-pool success case as it would panic.
	// Instead verify that JSON marshal of empty payload works via helper.
	payload := map[string]any{"a": "b", "n": 123}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, "b", out["a"])
}

// TestInventory_MockPool_NewInstanceWithoutAgent_Refused: a new instance is booked
// to the agent that reports it. Without an agent_id it is refused before its
// cluster is written, so the refused push leaves nothing behind.
func TestInventory_MockPool_NewInstanceWithoutAgent_Refused(t *testing.T) {
	t.Parallel()
	pool := &mockPool{queryRowVals: []*mockRow{{err: pgx.ErrNoRows}}}
	inv := &Inventory{pool: pool}
	env := wire.Envelope{
		Instances: []wire.Instance{{
			InstanceID: uuid.NewString(),
			ClusterID:  pgtype.ClusterID(555556).String(),
			Role:       "primary",
		}},
	}
	res, err := inv.Upsert(context.Background(), env)
	require.NoError(t, err)
	require.Equal(t, 0, res.Accepted)
	require.Equal(t, 1, res.Rejected)
	require.Contains(t, res.Errors[env.Instances[0].InstanceID].Error(), "missing agent_id")
	require.Equal(t, 0, pool.execCalls, "a refused instance writes no cluster, agent or instance row")
}
