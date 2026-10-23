//go:build integration

package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manprint/pglens/internal/check"
	"github.com/manprint/pglens/internal/clock"
	"github.com/manprint/pglens/internal/wire"
	"github.com/manprint/pglens/test/pgtest"
)

// INT-CONN-008: pg_stat_statements created only in an application database
// enables the stat_statements check, and the maintenance database's rows are
// read through that database's view. Scheduling used to look at the
// maintenance database alone and left the check disabled.
func TestINTCONN008_StatementsViewOutsideTheMaintenanceDatabase(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		const maintDB, appDB = "pglens_int_conn_008_maint", "pglens_int_conn_008_app"
		admin := pg.Pool(t, "postgres", pgtest.RoleSuperuser)
		t.Cleanup(func() {
			for _, db := range []string{maintDB, appDB} {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
			}
		})
		// pg.Pool creates each database with the extension in it.
		_, err := pg.Pool(t, maintDB, pgtest.RoleSuperuser).Exec(ctx, "DROP EXTENSION IF EXISTS pg_stat_statements")
		require.NoError(t, err)
		pg.Pool(t, appDB, pgtest.RoleSuperuser)

		mgr, err := NewManager(ctx, "statements-source", pg.DSN(maintDB, pgtest.RoleT0), DefaultConnOptions(), clock.System())
		require.NoError(t, err)
		defer mgr.Close()
		require.False(t, mgr.HasExtension("pg_stat_statements"))

		found, err := mgr.LocateStatements(ctx, []wire.Database{{Name: maintDB, Monitored: true}})
		require.NoError(t, err)
		require.False(t, found)
		require.Empty(t, mgr.StatementsDatabase())

		dbs := []wire.Database{{Name: maintDB, Monitored: true}, {Name: appDB, Monitored: true}}
		found, err = mgr.LocateStatements(ctx, dbs)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, appDB, mgr.StatementsDatabase())

		var statements check.Check
		for _, c := range check.All() {
			if c.Name() == "stat_statements" {
				statements = c
			}
		}
		require.NotNil(t, statements)
		_, err = statements.Scrape(ctx, databaseScopedTarget{Target: mgr, database: maintDB})
		require.NoError(t, err, "the maintenance database is read through the application database's view")
	})
}
