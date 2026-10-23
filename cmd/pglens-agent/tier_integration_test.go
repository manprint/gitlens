//go:build integration

package main

import (
	"context"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
)

// TestINTCLI001_CheckTierKeepsMixedCaseRole runs the check command's tier
// lookup as a role whose name has an upper-case letter. The lookup used to
// cast current_user to regrole, which folds the name to lower case, so the
// role was reported as T0 although it held pg_read_all_data.
func TestINTCLI001_CheckTierKeepsMixedCaseRole(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		const role = "PgLensCheck"
		quoted := pgx.Identifier{role}.Sanitize()
		_, err := admin.Exec(ctx, "DROP ROLE IF EXISTS "+quoted)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN PASSWORD 'test'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "GRANT pg_monitor, pg_read_all_data TO "+quoted)
		require.NoError(t, err)
		// Roles are cluster-wide and the snapshot restore does not drop them.
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+quoted) })

		u, err := url.Parse(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		u.User = url.UserPassword(role, "test")
		conn, err := pgx.Connect(ctx, u.String())
		require.NoError(t, err)
		defer func() { _ = conn.Close(ctx) }()

		require.Equal(t, pgtype.TierExplain, determinePermTier(ctx, conn))
	})
}
