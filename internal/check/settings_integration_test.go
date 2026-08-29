//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func TestINTSET001_ChangedWorkMemIsReportedAsBytes(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		_, err := admin.Exec(ctx, "ALTER SYSTEM SET work_mem = '64MB'")
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT pg_reload_conf()")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(ctx, "ALTER SYSTEM RESET work_mem")
			_, _ = admin.Exec(ctx, "SELECT pg_reload_conf()")
		})

		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		result, err := (&settingsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Equal(t, float64(64*1024*1024), metricValue(result, "pg_setting_bytes", map[string]string{"name": "work_mem"}))
		var fact *Fact
		for i := range result.Facts {
			if result.Facts[i].Key == "work_mem" {
				fact = &result.Facts[i]
				break
			}
		}
		require.NotNil(t, fact)
		// pg_settings exposes work_mem as its canonical kB value on PG15/18.
		require.Equal(t, "65536", fact.ValueText)
	})
}

func TestINTSET002_RestartSettingReportsPendingRestart(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		var currentMaxConnections int
		require.NoError(t, admin.QueryRow(ctx, "SELECT current_setting('max_connections')::int").Scan(&currentMaxConnections))
		_, err := admin.Exec(ctx, fmt.Sprintf("ALTER SYSTEM SET max_connections = '%d'", currentMaxConnections+1))
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT pg_reload_conf()")
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = admin.Exec(ctx, "ALTER SYSTEM RESET max_connections")
			_, _ = admin.Exec(ctx, "SELECT pg_reload_conf()")
		})

		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		result, err := (&settingsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.GreaterOrEqual(t, metricValue(result, "pg_settings_pending_restart", nil), 1.0)
		var pending bool
		for i := range result.Facts {
			if result.Facts[i].Key == "max_connections" && result.Facts[i].Labels["pending_restart"] == "true" {
				pending = true
			}
		}
		require.True(t, pending)
	})
}
