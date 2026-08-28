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

// Add uuid import to handle uuid.UUID type

type testTarget struct {
	conn       *pgxpool.Conn
	version    pgtype.PGVersion
	database   string
	instanceID pgtype.InstanceID
	clusterID  pgtype.ClusterID
}

func (m *testTarget) InstanceID() pgtype.InstanceID { return m.instanceID }
func (m *testTarget) ClusterID() pgtype.ClusterID   { return m.clusterID }
func (m *testTarget) Role() pgtype.Role             { return pgtype.RolePrimary }
func (m *testTarget) PGVersion() pgtype.PGVersion   { return m.version }
func (m *testTarget) PermTier() pgtype.PermTier     { return pgtype.TierReadOnly }
func (m *testTarget) HasExtension(name string) bool { return false }
func (m *testTarget) Conn(ctx context.Context) (Conn, error) {
	return m.conn, nil
}
func (m *testTarget) ConnFor(ctx context.Context, datname string) (Conn, error) {
	return m.conn, nil
}
func (m *testTarget) Database() string {
	return m.database
}
func (m *testTarget) Clock() clock.Clock {
	return clock.System()
}

// INT-CHECK-014: database_stats returns a row per database and a non-nil `StatsReset`;
// after `SELECT pg_stat_reset()`, the returned `StatsReset` is strictly later than before.
// This is the end-to-end proof that reset detection has a real signal to work with.
func TestDatabaseStats_INT_CHECK_014_ResetDetection(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		// Use superuser to call pg_stat_reset, but read as T0
		adminPool := pg.Pool(t, "app", pgtest.RoleSuperuser)
		dataPool := pg.Pool(t, "app", pgtest.RoleT0)

		ctx := context.Background()

		check := &databaseStatsCheck{}

		// First, call pg_stat_reset() as admin to initialize stats_reset timestamp
		adminConn, err := adminPool.Acquire(ctx)
		require.NoError(t, err)
		_, err = adminConn.Exec(ctx, "SELECT pg_stat_reset()")
		require.NoError(t, err, "initial pg_stat_reset() should succeed")
		adminConn.Release()

		// First scrape - should now have non-nil StatsReset
		dataConn, err := dataPool.Acquire(ctx)
		require.NoError(t, err)
		mockTarget := &testTarget{
			conn:       dataConn,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ClusterID(1),
		}

		result1, err := check.Scrape(ctx, mockTarget)
		require.NoError(t, err, "first scrape should succeed")
		require.NotNil(t, result1.StatsReset, "StatsReset should not be nil after pg_stat_reset()")
		require.Greater(t, len(result1.Metrics), 0, "should have at least one metric")

		statsReset1 := *result1.StatsReset
		dataConn.Release()

		// No sleep needed: pg_stat_reset() sets stats_reset to now(), a
		// timestamptz with microsecond precision, and this call only
		// happens after the round trip above returns — the two now()
		// calls are already strictly ordered by real elapsed wall-clock
		// time, however small (CONTRIBUTING.md: "No time.Sleep anywhere
		// below L5").
		adminConn, err = adminPool.Acquire(ctx)
		require.NoError(t, err)
		_, err = adminConn.Exec(ctx, "SELECT pg_stat_reset()")
		require.NoError(t, err, "pg_stat_reset() should succeed")
		adminConn.Release()

		// Second scrape - should have newer StatsReset
		dataConn, err = dataPool.Acquire(ctx)
		require.NoError(t, err)
		mockTarget2 := &testTarget{
			conn:       dataConn,
			version:    pg.Version,
			database:   "app",
			instanceID: uuid.New(),
			clusterID:  pgtype.ClusterID(1),
		}

		result2, err := check.Scrape(ctx, mockTarget2)
		require.NoError(t, err, "second scrape should succeed")
		require.NotNil(t, result2.StatsReset, "StatsReset should still not be nil after reset")
		dataConn.Release()

		statsReset2 := *result2.StatsReset

		require.True(t, statsReset2.After(statsReset1), "stats_reset should be strictly later after pg_stat_reset() call")
	})
}
