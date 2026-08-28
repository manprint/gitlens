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

// registryTestTarget implements Target for testing checks against a real database.
type registryTestTarget struct {
	conn       *pgxpool.Conn
	version    pgtype.PGVersion
	database   string
	instanceID pgtype.InstanceID
	clusterID  pgtype.ClusterID
	role       pgtype.Role
	permTier   pgtype.PermTier
	extensions map[string]bool
}

func (m *registryTestTarget) InstanceID() pgtype.InstanceID { return m.instanceID }
func (m *registryTestTarget) ClusterID() pgtype.ClusterID   { return m.clusterID }
func (m *registryTestTarget) Role() pgtype.Role             { return m.role }
func (m *registryTestTarget) PGVersion() pgtype.PGVersion   { return m.version }
func (m *registryTestTarget) PermTier() pgtype.PermTier     { return m.permTier }
func (m *registryTestTarget) HasExtension(name string) bool { return m.extensions[name] }
func (m *registryTestTarget) Conn(ctx context.Context) (Conn, error) {
	return m.conn, nil
}
func (m *registryTestTarget) ConnFor(ctx context.Context, datname string) (Conn, error) {
	return m.conn, nil
}
func (m *registryTestTarget) Database() string {
	return m.database
}
func (m *registryTestTarget) Clock() clock.Clock {
	return clock.System()
}

// INT-CHECK-015: every check in All() runs clean as RoleT0, on every version, on both profiles.
// Any check needing more must declare it in Requires() and be skipped upstream;
// the test asserts that a skip carries a non-empty reason.
func TestINTCHECK015_AllChecksWorkOnT0(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)

		ctx := context.Background()
		dataPool := pg.Pool(t, "app", pgtest.RoleT0)

		// Query what extensions are available on this instance
		setupConn, err := dataPool.Acquire(ctx)
		require.NoError(t, err)
		rows, err := setupConn.Query(ctx, `SELECT extname FROM pg_extension`)
		require.NoError(t, err)
		extensions := make(map[string]bool)
		for rows.Next() {
			var ext string
			require.NoError(t, rows.Scan(&ext))
			extensions[ext] = true
		}
		rows.Close()
		setupConn.Release()

		// Iterate through every registered check
		checks := All()
		require.Greater(t, len(checks), 0, "must have registered checks")

		var runCount, skipCount int
		for _, check := range checks {
			reqs := check.Requires()

			// Determine if this check can run on T0 primary
			canRun, skipReason := reqs.Supports(
				pgtype.RolePrimary,
				pg.Version,
				pgtype.TierReadOnly,
				extensions,
			)

			if !canRun {
				// Check is appropriately skipped — verify skip reason is not empty
				require.NotEmpty(t, skipReason,
					"check %s is skipped but skip reason is empty", check.Name())
				skipCount++
				continue
			}

			// This check should run on T0 — acquire a fresh connection for it
			// (checks call Release() on the connection they receive)
			dataConn, err := dataPool.Acquire(ctx)
			require.NoError(t, err)

			target := &registryTestTarget{
				conn:       dataConn,
				version:    pg.Version,
				database:   "app",
				instanceID: uuid.New(),
				clusterID:  pgtype.ClusterID(1),
				role:       pgtype.RolePrimary,
				permTier:   pgtype.TierReadOnly,
				extensions: extensions,
			}

			result, err := check.Scrape(ctx, target)
			require.NoError(t, err,
				"check %s should run clean on T0 primary", check.Name())
			require.NotNil(t, result, "check %s returned nil result", check.Name())
			runCount++
		}

		// Both branches should be exercised: some checks run on T0, some are skipped.
		// If all checks are on the same branch, the test setup is likely wrong.
		require.Greater(t, runCount, 0,
			"must have at least one check that runs on T0 (setup verification)")
		require.Greater(t, skipCount, 0,
			"must have at least one check that is skipped on T0 (setup verification)")

		t.Logf("checks: %d run, %d skipped", runCount, skipCount)
	})
}
