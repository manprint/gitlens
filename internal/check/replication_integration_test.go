//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-REPL-005: replication_slots runs without error on the standby (no standby topology available in pgtest)
// Deferred assertion: this test proves the pg_is_in_recovery() CASE guard compiles and works on the primary.
// The real test of standby safety requires a real standby from test/compose or phase 5's E2E harness.
func TestReplicationSlots_Integration_RunsOnPrimary(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		mockTarget := &SimpleTarget{
			ConnFunc: func(ctx context.Context) (Conn, error) {
				c, err := pool.Acquire(ctx)
				return c, err
			},
			ClockValue: clock.System(),
		}

		check := &replicationSlotsCheck{}
		result, err := check.Scrape(ctx, mockTarget)
		require.NoError(t, err, "slots query should succeed on primary")
		// On a standalone instance with no slots, expect empty result
		require.Empty(t, result.Metrics)
	})
}

// INT-REPL-006 (deferred): after pg_ctl promote, replication_streaming becomes applicable
// and replication_receiver stops being applicable, driven purely by Requires().Supports.
// This test requires primary-standby topology and pg_ctl promote, available only in phase 5's E2E harness.
// Unit test already confirms Requires() role check works correctly.

// TestReplicationReceiver_Integration_NotApplicableOnPrimary — replication_receiver Requires() rejects primary
func TestReplicationReceiver_Integration_NotApplicableOnPrimary(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		check := &replicationReceiverCheck{}
		req := check.Requires()
		ok, reason := req.Supports(pgtype.RolePrimary, pg.Version, pgtype.TierReadOnly, nil)
		require.False(t, ok, "replication_receiver should reject primary role: %s", reason)
	})
}

// TestReplicationStreaming_Integration_NotApplicableOnStandby — replication_streaming Requires() rejects standby
// (Note: testing on a primary-only instance, so we verify the check logic via Requires())
func TestReplicationStreaming_Integration_NotApplicableOnStandby(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		check := &replicationStreamingCheck{}
		req := check.Requires()
		ok, reason := req.Supports(pgtype.RoleStandby, pg.Version, pgtype.TierReadOnly, nil)
		require.False(t, ok, "replication_streaming should reject standby role: %s", reason)
	})
}

// TestReplicationStreaming_Integration_EmptyPrimary — pg_stat_replication is empty on standalone
func TestReplicationStreaming_Integration_EmptyPrimary(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		mockTarget := &SimpleTarget{
			InstanceIDValue: uuid.New(),
			ClusterIDValue:  pgtype.ManualClusterID("test"),
			PGVersionValue:  pg.Version,
			RoleValue:       pgtype.RolePrimary,
			PermTierValue:   pgtype.TierReadOnly,
			ConnFunc: func(ctx context.Context) (Conn, error) {
				c, err := pool.Acquire(ctx)
				return c, err
			},
			ClockValue: clock.System(),
		}

		check := &replicationStreamingCheck{}
		result, err := check.Scrape(ctx, mockTarget)
		require.NoError(t, err, "streaming query should succeed on primary")
		require.Empty(t, result.Metrics, "standalone primary should have no replicas")
	})
}

// TestReplicationSlots_Integration_SlotManipulation — create a slot, verify it's reported
func TestReplicationSlots_Integration_SlotManipulation(t *testing.T) {
	t.Parallel()
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()

		// Creating/dropping a physical replication slot requires superuser or
		// the replication role; pg_monitor does not grant it. The check under
		// test still runs as RoleT0, matching a real T0-permission agent.
		adminPool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		defer adminPool.Close()
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		defer adminConn.Release()

		_, err = adminConn.Exec(ctx, "SELECT pg_create_physical_replication_slot('test_slot')")
		require.NoError(t, err)
		defer func() {
			_, _ = adminConn.Exec(ctx, "SELECT pg_drop_replication_slot('test_slot')")
		}()

		pool, err := pgxpool.New(ctx, pg.DSN("app", pgtest.RoleT0))
		require.NoError(t, err)
		defer pool.Close()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		defer conn.Release()

		mockTarget := &SimpleTarget{
			InstanceIDValue: uuid.New(),
			ClusterIDValue:  pgtype.ManualClusterID("test"),
			PGVersionValue:  pg.Version,
			RoleValue:       pgtype.RolePrimary,
			PermTierValue:   pgtype.TierReadOnly,
			ConnFunc: func(ctx context.Context) (Conn, error) {
				c, err := pool.Acquire(ctx)
				return c, err
			},
			ClockValue: clock.System(),
		}

		check := &replicationSlotsCheck{}
		result, err := check.Scrape(ctx, mockTarget)
		require.NoError(t, err, "slots query should succeed after creating a slot")
		// Should report the slot
		require.Greater(t, len(result.Metrics), 0, "should report created slot")
		hasTestSlot := false
		for _, m := range result.Metrics {
			if m.Labels["slot_name"] == "test_slot" {
				hasTestSlot = true
				break
			}
		}
		require.True(t, hasTestSlot, "should report test_slot in metrics")
	})
}
