//go:build integration

package check

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

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

// INT-REPL-001: On the primary, replication_streaming returns exactly one row
// with the standby's application_name, sync_state='async', and all three lag
// SECONDS values non-NULL after the standby has replayed once.
func TestReplicationStreaming_Integration_PrimaryWithStandby(t *testing.T) {
	t.Parallel()
	// Only test on one major version to reduce test runtime
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			ctx := context.Background()
			primary, _ := pgtest.PrimaryStandby(t, major)

			// Use superuser to create the test table (pglens role doesn't have schema creation perms)
			primaryPoolAdmin, err := pgxpool.New(ctx, primary.DSN("app", pgtest.RoleSuperuser))
			require.NoError(t, err)
			defer primaryPoolAdmin.Close()

			// Do a write on primary to populate the lag values
			// (they're NULL until there's write activity with commit feedback)
			_, err = primaryPoolAdmin.Exec(ctx, "CREATE TABLE int_repl_001_test (id INT)")
			require.NoError(t, err)

			// Now connect as T0 role to query replication
			primaryPool, err := pgxpool.New(ctx, primary.DSN("app", pgtest.RoleT0))
			require.NoError(t, err)
			defer primaryPool.Close()

			// Now query the primary's replication_streaming
			mockTarget := &SimpleTarget{
				InstanceIDValue: uuid.New(),
				ClusterIDValue:  pgtype.ManualClusterID("test"),
				PGVersionValue:  primary.Version,
				RoleValue:       pgtype.RolePrimary,
				PermTierValue:   pgtype.TierReadOnly,
				ConnFunc: func(ctx context.Context) (Conn, error) {
					c, err := primaryPool.Acquire(ctx)
					return c, err
				},
				ClockValue: clock.System(),
			}

			check := &replicationStreamingCheck{}

			// Poll instead of a fixed sleep (CONTRIBUTING.md: "No time.Sleep
			// anywhere below L5") — wait for the standby to replay the write
			// above, which is what actually makes the three lag_seconds
			// columns non-NULL.
			var result Result
			require.Eventually(t, func() bool {
				var scrapeErr error
				result, scrapeErr = check.Scrape(ctx, mockTarget)
				if scrapeErr != nil || len(result.Metrics) == 0 {
					return false
				}
				lagCount := 0
				for _, m := range result.Metrics {
					switch m.Name {
					case "replication_write_lag_seconds", "replication_flush_lag_seconds", "replication_replay_lag_seconds":
						lagCount++
					}
				}
				return lagCount == 3
			}, 15*time.Second, 200*time.Millisecond, "replication_streaming should report all three lag_seconds metrics once the standby has replayed")

			// Should have exactly one replica (the standby)
			// We expect 6 metrics per replica (3 lag types × 2: bytes and seconds)
			require.Greater(t, len(result.Metrics), 0, "should have metrics for standby")

			// Group metrics by labels to count unique replicas
			replicaLabels := make(map[string]bool)
			for _, m := range result.Metrics {
				replicaLabels[m.Labels["standby"]] = true
			}
			require.Len(t, replicaLabels, 1, "should have exactly one standby")

			// Verify the standby's application_name is pg-standby
			for _, m := range result.Metrics {
				require.Equal(t, "pg-standby", m.Labels["standby"], "standby should use pg-standby as application_name")
				require.Equal(t, "async", m.Labels["sync_mode"], "sync_state should be async")
			}

			// Verify all three lag SECONDS values are present and non-NULL
			lagSecsMetrics := map[string]bool{}
			for _, m := range result.Metrics {
				if m.Name == "replication_write_lag_seconds" ||
					m.Name == "replication_flush_lag_seconds" ||
					m.Name == "replication_replay_lag_seconds" {
					lagSecsMetrics[m.Name] = true
					// Value should be a valid number (non-NaN)
					require.False(t, math.IsNaN(m.Value), "%s should not be NaN", m.Name)
				}
			}
			require.Len(t, lagSecsMetrics, 3, "should have all three lag_seconds metrics")
		})
	}
}

// INT-REPL-002: On the standby, replication_receiver reports status='streaming'
// and replay_lag_sec equals exactly 0 while the primary is idle.
// This tests the CASE guard in the SQL that prevents drift when caught up.
func TestReplicationReceiver_Integration_StandbyStreaming(t *testing.T) {
	t.Parallel()
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			ctx := context.Background()
			primary, standby := pgtest.PrimaryStandby(t, major)
			_ = primary // We'll use standby

			standbyPool, err := pgxpool.New(ctx, standby.DSN("app", pgtest.RoleT0))
			require.NoError(t, err)
			defer standbyPool.Close()

			mockTarget := &SimpleTarget{
				InstanceIDValue: uuid.New(),
				ClusterIDValue:  pgtype.ManualClusterID("test"),
				PGVersionValue:  standby.Version,
				RoleValue:       pgtype.RoleStandby,
				PermTierValue:   pgtype.TierReadOnly,
				ConnFunc: func(ctx context.Context) (Conn, error) {
					c, err := standbyPool.Acquire(ctx)
					return c, err
				},
				ClockValue: clock.System(),
			}

			check := &replicationReceiverCheck{}

			// Poll instead of a fixed sleep — wait for replication to settle
			// into status='streaming' (CONTRIBUTING.md: "No time.Sleep
			// anywhere below L5").
			var result Result
			require.Eventually(t, func() bool {
				var scrapeErr error
				result, scrapeErr = check.Scrape(ctx, mockTarget)
				if scrapeErr != nil {
					return false
				}
				for _, m := range result.Metrics {
					if m.Name == "replication_receiver_status" && m.Labels["status"] == "streaming" {
						return true
					}
				}
				return false
			}, 15*time.Second, 200*time.Millisecond, "standby should reach status=streaming")

			// Should have exactly one row (from the LEFT JOIN dummy guarantee)
			require.Len(t, result.Metrics, 3, "should have 3 metrics (status, sender_port, replay_lag)")

			// Check status is streaming
			foundStatus := false
			foundLagZero := false
			for _, m := range result.Metrics {
				if m.Name == "replication_receiver_status" {
					foundStatus = true
					require.Equal(t, float64(1), m.Value, "status should be 'streaming' (encoded as 1)")
					require.Equal(t, "streaming", m.Labels["status"])
				}
				if m.Name == "replication_receiver_replay_lag_seconds" {
					foundLagZero = true
					// CRITICAL: The CASE guard in the SQL ensures this is exactly 0 when caught up
					require.Equal(t, 0.0, m.Value, "replay_lag_sec should be exactly 0 when caught up (CASE guard test)")
				}
			}
			require.True(t, foundStatus, "should find replication_receiver_status metric")
			require.True(t, foundLagZero, "should find replication_receiver_replay_lag_seconds metric")
		})
	}
}

// INT-REPL-003: When the standby has no active WAL receiver, the receiver check
// reports status='disconnected' in exactly one row. This tests the LEFT JOIN guarantee:
// without the LEFT JOIN on a dummy row, an empty pg_stat_wal_receiver would produce
// zero result rows, not one. The LEFT JOIN ensures we always get exactly one row.
func TestReplicationReceiver_Integration_Disconnected(t *testing.T) {
	t.Parallel()
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			ctx := context.Background()
			_, standby := pgtest.PrimaryStandby(t, major)

			standbyPool, err := pgxpool.New(ctx, standby.DSN("app", pgtest.RoleSuperuser))
			require.NoError(t, err)
			defer standbyPool.Close()

			// Disconnect the standby from the primary by clearing its recovery config.
			// This makes pg_stat_wal_receiver empty (no active WAL receiver).
			conn, err := standbyPool.Acquire(ctx)
			require.NoError(t, err)

			// ALTER SYSTEM to remove the primary connection info, then reload
			// This will cause the standby to lose its replication connection
			_, _ = conn.Exec(ctx, "ALTER SYSTEM SET primary_conninfo = ''")
			_, _ = conn.Exec(ctx, "SELECT pg_reload_conf()")

			conn.Release()

			// Now scrape from a new connection with T0 permissions
			standbyPool2, err := pgxpool.New(ctx, standby.DSN("app", pgtest.RoleT0))
			require.NoError(t, err)
			defer standbyPool2.Close()

			mockTarget := &SimpleTarget{
				InstanceIDValue: uuid.New(),
				ClusterIDValue:  pgtype.ManualClusterID("test"),
				PGVersionValue:  standby.Version,
				RoleValue:       pgtype.RoleStandby,
				PermTierValue:   pgtype.TierReadOnly,
				ConnFunc: func(ctx context.Context) (Conn, error) {
					c, err := standbyPool2.Acquire(ctx)
					return c, err
				},
				ClockValue: clock.System(),
			}

			check := &replicationReceiverCheck{}

			// Poll instead of a fixed sleep — clearing primary_conninfo and
			// reloading config doesn't disconnect the WAL receiver
			// instantly, so wait for status to actually become
			// 'disconnected' rather than guessing how long that takes.
			var result Result
			require.Eventually(t, func() bool {
				var scrapeErr error
				result, scrapeErr = check.Scrape(ctx, mockTarget)
				if scrapeErr != nil {
					return false
				}
				for _, m := range result.Metrics {
					if m.Name == "replication_receiver_status" && m.Labels["status"] == "disconnected" {
						return true
					}
				}
				return false
			}, 15*time.Second, 200*time.Millisecond, "WAL receiver should disconnect after primary_conninfo is cleared")

			// The LEFT JOIN guarantee: exactly one row, even though pg_stat_wal_receiver is empty
			require.Len(t, result.Metrics, 3, "should have exactly 3 metrics even when no receiver")

			foundDisconnected := false
			for _, m := range result.Metrics {
				if m.Name == "replication_receiver_status" {
					foundDisconnected = true
					require.Equal(t, float64(0), m.Value, "status should be 'disconnected' (encoded as 0)")
					require.Equal(t, "disconnected", m.Labels["status"])
				}
			}
			require.True(t, foundDisconnected, "should find disconnected status")
		})
	}
}

// INT-REPL-004: replication_slots on the primary reports the slot as active.
// After stopping the standby, it reports inactive and retained_bytes grows.
func TestReplicationSlots_Integration_SlotActivityAndRetention(t *testing.T) {
	t.Parallel()
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			ctx := context.Background()
			primary, standby := pgtest.PrimaryStandby(t, major)

			primaryPool, err := pgxpool.New(ctx, primary.DSN("app", pgtest.RoleT0))
			require.NoError(t, err)
			defer primaryPool.Close()

			// Admin pool for writes
			primaryPoolAdmin, err := pgxpool.New(ctx, primary.DSN("app", pgtest.RoleSuperuser))
			require.NoError(t, err)
			defer primaryPoolAdmin.Close()

			// Wait for the slot to become active by retrying the check
			var result1 Result
			var standby1Metrics1 []pgtype.Metric
			mockTarget := &SimpleTarget{
				InstanceIDValue: uuid.New(),
				ClusterIDValue:  pgtype.ManualClusterID("test"),
				PGVersionValue:  primary.Version,
				RoleValue:       pgtype.RolePrimary,
				PermTierValue:   pgtype.TierReadOnly,
				ConnFunc: func(ctx context.Context) (Conn, error) {
					c, err := primaryPool.Acquire(ctx)
					return c, err
				},
				ClockValue: clock.System(),
			}

			check := &replicationSlotsCheck{}

			// Poll instead of a fixed-count sleep loop — wait for the slot
			// to become active (CONTRIBUTING.md: "No time.Sleep anywhere
			// below L5").
			var foundActive bool
			require.Eventually(t, func() bool {
				var scrapeErr error
				result1, scrapeErr = check.Scrape(ctx, mockTarget)
				if scrapeErr != nil {
					return false
				}
				standby1Metrics1 = nil
				for _, m := range result1.Metrics {
					if m.Labels["slot_name"] == "standby1" {
						standby1Metrics1 = append(standby1Metrics1, m)
					}
				}
				if len(standby1Metrics1) == 0 {
					return false
				}
				foundActive = false
				for _, m := range standby1Metrics1 {
					if m.Name == "replication_slot_active" && m.Value == 1.0 {
						foundActive = true
						break
					}
				}
				return foundActive
			}, 15*time.Second, 200*time.Millisecond, "standby1 slot should become active")

			require.Greater(t, len(standby1Metrics1), 0, "should find standby1 slot")

			// phase_07.md's own wording: "after stopping the standby it
			// reports inactive and retained_bytes grows" — growth is a
			// property of the standby being GONE (nothing consuming WAL,
			// so restart_lsn freezes while the primary keeps advancing),
			// not of writing while the standby is still connected and
			// replaying: a local, low-latency standby replays a one-row
			// INSERT essentially instantly, keeping restart_lsn caught up
			// and retained_bytes at/near 0 indefinitely — found live, this
			// is exactly what made an earlier version of this test
			// (write-while-connected, no stop) time out waiting for
			// retained_bytes > 0 that was never coming.
			standby.Stop(t)

			// Poll instead of a fixed sleep — wait for the slot to report
			// inactive now that the standby is gone.
			require.Eventually(t, func() bool {
				result2, scrapeErr := check.Scrape(ctx, mockTarget)
				if scrapeErr != nil {
					return false
				}
				for _, m := range result2.Metrics {
					if m.Labels["slot_name"] == "standby1" && m.Name == "replication_slot_active" {
						return m.Value == 0.0
					}
				}
				return false
			}, 15*time.Second, 200*time.Millisecond, "standby1 slot should become inactive once the standby stops")

			// Write data on primary: with nothing consuming WAL anymore,
			// retained_bytes must now actually grow rather than track ~0.
			_, err = primaryPoolAdmin.Exec(ctx, "CREATE TABLE int_repl_004_test (id INT); INSERT INTO int_repl_004_test VALUES (1)")
			require.NoError(t, err)

			var standby1Metrics2 []pgtype.Metric
			var retainedBytes2 float64
			require.Eventually(t, func() bool {
				result3, scrapeErr := check.Scrape(ctx, mockTarget)
				if scrapeErr != nil {
					return false
				}
				standby1Metrics2 = nil
				for _, m := range result3.Metrics {
					if m.Labels["slot_name"] == "standby1" {
						standby1Metrics2 = append(standby1Metrics2, m)
					}
				}
				if len(standby1Metrics2) == 0 {
					return false
				}
				retainedBytes2 = 0
				for _, m := range standby1Metrics2 {
					if m.Name == "replication_slot_retained_bytes" {
						retainedBytes2 = m.Value
					}
				}
				return retainedBytes2 > 0
			}, 15*time.Second, 200*time.Millisecond, "replication_slot_retained_bytes should reflect the new WAL retained by the inactive slot")

			require.Greater(t, len(standby1Metrics2), 0, "should still find standby1 slot")
			require.Greater(t, retainedBytes2, 0.0, "retained_bytes should track slot retention once the standby has stopped")
		})
	}
}

// INT-REPL-006: After promoting the standby to primary via pg_ctl promote or
// SELECT pg_promote(), replication_streaming becomes applicable on the former
// standby and replication_receiver stops being applicable, driven purely by
// Requires().Supports() checking the role.
func TestReplicationRole_Integration_Promotion(t *testing.T) {
	t.Parallel()
	for _, major := range pgtest.Versions() {
		major := major
		t.Run(fmt.Sprintf("pg%d", major), func(t *testing.T) {
			ctx := context.Background()
			_, standby := pgtest.PrimaryStandby(t, major)

			standbyPool, err := pgxpool.New(ctx, standby.DSN("app", pgtest.RoleSuperuser))
			require.NoError(t, err)
			defer standbyPool.Close()

			// Verify it's currently a standby
			var isRecovery bool
			err = standbyPool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isRecovery)
			require.NoError(t, err)
			require.True(t, isRecovery, "should be in recovery (standby) before promotion")

			// Promote it
			_, err = standbyPool.Exec(ctx, "SELECT pg_promote()")
			require.NoError(t, err)

			// Poll instead of a fixed sleep — wait for promotion to
			// actually complete (CONTRIBUTING.md: "No time.Sleep anywhere
			// below L5").
			require.Eventually(t, func() bool {
				var recovering bool
				if err := standbyPool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&recovering); err != nil {
					return false
				}
				return !recovering
			}, 15*time.Second, 200*time.Millisecond, "promotion should complete")

			// Verify it's now a primary
			err = standbyPool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isRecovery)
			require.NoError(t, err)
			require.False(t, isRecovery, "should not be in recovery (primary) after promotion")

			// Close old pool and reconnect to ensure we have a fresh session
			standbyPool.Close()
			standbyPool, err = pgxpool.New(ctx, standby.DSN("app", pgtest.RoleSuperuser))
			require.NoError(t, err)
			defer standbyPool.Close()

			// After promotion, the former standby is now a primary.
			// replication_streaming requires PRIMARY role, so it should be supported.
			checkStream := &replicationStreamingCheck{}
			req := checkStream.Requires()
			ok, _ := req.Supports(pgtype.RolePrimary, standby.Version, pgtype.TierReadOnly, nil)
			require.True(t, ok, "replication_streaming should be supported for primary role")

			// replication_receiver requires STANDBY role, so it should NOT be supported.
			checkRecv := &replicationReceiverCheck{}
			req2 := checkRecv.Requires()
			ok2, _ := req2.Supports(pgtype.RolePrimary, standby.Version, pgtype.TierReadOnly, nil)
			require.False(t, ok2, "replication_receiver should NOT be supported for primary role")
		})
	}
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
