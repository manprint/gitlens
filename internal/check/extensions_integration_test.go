//go:build integration

package check

import (
	"context"
	"testing"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

func scrapeExtensionsAtT0(t *testing.T, pg *pgtest.PG) map[string]Fact {
	t.Helper()
	ctx := context.Background()
	conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
	require.NoError(t, err)
	result, err := (&extensionsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	facts := map[string]Fact{}
	for _, f := range result.Facts {
		facts[f.Key] = f
	}
	return facts
}

// INT-EXT-001: at T0 the extension inventory reports each installed
// extension with its version and the packages' default version.
func TestINTEXT001_Extensions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		facts := scrapeExtensionsAtT0(t, pg)
		require.Contains(t, facts, "plpgsql")
		require.Equal(t, "pg_catalog", facts["plpgsql"].Labels["schema"])
		require.Equal(t, "none", facts["plpgsql"].Labels["upgrade"])
		require.Contains(t, facts, "pg_stat_statements")
		require.NotEmpty(t, facts["pg_stat_statements"].ValueText)
		require.NotContains(t, facts, "postgis")
	})
}

// INT-EXT-002: PostGIS is reported with its version, as installed, and the
// version its packages carry.
func TestINTEXT002_PostGISVersion(t *testing.T) {
	pgtest.ForEachPostGIS(t, func(t *testing.T, pg *pgtest.PG) {
		facts := scrapeExtensionsAtT0(t, pg)
		postgis := facts["postgis"]
		require.Regexp(t, `^3\.\d+\.\d+`, postgis.ValueText)
		require.Equal(t, postgis.ValueText, postgis.Labels["default_version"])
		require.Equal(t, "none", postgis.Labels["upgrade"])
		require.Contains(t, facts, "postgis_topology")
	})
}
