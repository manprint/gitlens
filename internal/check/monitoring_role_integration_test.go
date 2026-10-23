//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func scrapeMonitoringRole(t *testing.T, pg *pgtest.PG, pool *pgxpool.Pool, setup string) Fact {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	if setup != "" {
		_, err = conn.Exec(ctx, setup)
		require.NoError(t, err)
	}
	result, err := (&monitoringRoleCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: pgtype.TierReadOnly})
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	return result.Facts[0]
}

// TestINTCHECK018_MonitoringRolePosture: the role the fixtures set up like
// deploy/sql/monitoring_user.sql reports nothing; a role that can write
// through another role, and one whose read-only default a database setting
// turned off, are reported even when the session itself is read-only.
func TestINTCHECK018_MonitoringRolePosture(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		drop := func() {
			_, err := admin.Exec(context.Background(), `DROP TABLE IF EXISTS check_role_owned;
DROP ROLE IF EXISTS check_role_writer;
DROP ROLE IF EXISTS check_role_group`)
			require.NoError(t, err)
		}
		drop()
		t.Cleanup(drop)

		for _, role := range []pgtest.Role{pgtest.RoleT0, pgtest.RoleT1, pgtest.RoleT2} {
			f := scrapeMonitoringRole(t, pg, pg.Pool(t, "app", role), "")
			require.Equal(t, monitoringRoleReadOnly, f.ValueText, role.String())
			require.Equal(t, "on", f.Labels["read_only_default"], role.String())
		}

		f := scrapeMonitoringRole(t, pg, admin, "")
		require.Contains(t, f.ValueText, "has SUPERUSER")
		require.NotContains(t, f.ValueText, "is a member of")

		_, err := admin.Exec(ctx, `CREATE ROLE check_role_group;
GRANT pg_write_all_data TO check_role_group;
CREATE ROLE check_role_writer LOGIN PASSWORD 'test' IN ROLE check_role_group;
GRANT pg_monitor TO check_role_writer;
ALTER ROLE check_role_writer SET default_transaction_read_only = on;
ALTER ROLE check_role_writer IN DATABASE app SET default_transaction_read_only = off;
CREATE TABLE check_role_owned (a int);
ALTER TABLE check_role_owned OWNER TO check_role_writer`)
		require.NoError(t, err)
		cfg, err := pgxpool.ParseConfig(pg.DSN("app", pgtest.RoleSuperuser))
		require.NoError(t, err)
		cfg.ConnConfig.User, cfg.ConnConfig.Password = "check_role_writer", "test"
		writer, err := pgxpool.NewWithConfig(ctx, cfg)
		require.NoError(t, err)
		t.Cleanup(writer.Close)

		// The agent's own session is read-only whatever the role says; the
		// check reads the value the role would get without it.
		f = scrapeMonitoringRole(t, pg, writer, "SET default_transaction_read_only = on")
		require.Equal(t, "is a member of check_role_group; is a member of pg_write_all_data; owns 1 object(s)", f.ValueText)
		require.Equal(t, "off", f.Labels["read_only_default"])
		require.Equal(t, "check_role_writer", f.Labels["role"])
	})
}
