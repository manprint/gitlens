//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTCHECK017_PhaseFiveChecksRunOnT0(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		for _, check := range []Check{&settingsCheck{}, &walCheck{}, &checkpointerCheck{}, &archiverCheck{}} {
			conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
			require.NoError(t, err)
			result, err := check.Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly})
			require.NoError(t, err, check.Name())
			require.NotNil(t, result, check.Name())
		}
		if pg.Version >= pgtype.PG16 {
			conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
			require.NoError(t, err)
			_, err = (&ioCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", role: pgtype.RolePrimary, permTier: pgtype.TierReadOnly})
			require.NoError(t, err)
		}
	})
}

func TestINTSET004_SettingsWithoutElevatedGrantDegradeCleanly(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(context.Background())
		require.NoError(t, err)
		_, err = (&settingsCheck{}).Scrape(context.Background(), &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
		require.NoError(t, err)
	})
}
