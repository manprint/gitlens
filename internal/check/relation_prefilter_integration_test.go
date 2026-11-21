//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/manprint/pglens/internal/cardinality"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// prefilterFillers is more relations than the SQL prefilter reads per ranking
// at its minimum (300), so the LIMITs of indexStatsQuery and tableStatsQuery
// really narrow the rows.
const prefilterFillers = 320

// TestINTPREFILTER001_RetainedRelationIsReadPastTheLimit: a relation that was
// selected on one scrape and then falls below every ranking's SQL limit must
// still be read on the next scrape, through the retained keys, so the
// selector's hysteresis keeps reporting it. The relation names sort last, so
// after the TRUNCATE no ranking reaches it within the limit.
func TestINTPREFILTER001_RetainedRelationIsReadPastTheLimit(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_pf_%d", testSuffix())
		_, err := admin.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = admin.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)) })
		_, err = admin.Exec(ctx, fmt.Sprintf(`DO $$ BEGIN
FOR i IN 1..%d LOOP
  EXECUTE format('CREATE TABLE %s.f%%s (a integer)', lpad(i::text, 4, '0'));
  EXECUTE format('CREATE INDEX f%%s_i ON %s.f%%s (a)', lpad(i::text, 4, '0'), lpad(i::text, 4, '0'));
END LOOP; END $$`, prefilterFillers, schema, schema))
		require.NoError(t, err)
		for _, stmt := range []string{
			"CREATE TABLE %s.zz_big (a integer)",
			"INSERT INTO %s.zz_big SELECT generate_series(1, 50000)",
			"CREATE INDEX zz_big_i ON %s.zz_big (a)",
			"ANALYZE %s.zz_big",
		} {
			_, err = admin.Exec(ctx, fmt.Sprintf(stmt, schema))
			require.NoError(t, err)
		}
		tables := newScopedSelectors(cardinality.Options{TopN: 5})
		indexes := newScopedSelectors(cardinality.Options{TopN: 5})
		scrape := func() (Result, Result) {
			t.Helper()
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			tableResult, err := (&tableStatsCheck{selectors: tables}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app"})
			require.NoError(t, err)
			conn, err = pool.Acquire(ctx)
			require.NoError(t, err)
			indexResult, err := (&indexStatsCheck{selectors: indexes}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app"})
			require.NoError(t, err)
			return tableResult, indexResult
		}
		reported := func(r Result, name string) bool {
			for _, f := range r.Facts {
				if f.Labels["schemaname"] == schema && f.Labels["indexrelname"] == name {
					return true
				}
			}
			return len(findTableMetric(r, name, "pg_table_total_bytes")) > 0
		}

		tableResult, indexResult := scrape()
		require.True(t, reported(tableResult, "zz_big"), "the largest table is selected")
		require.True(t, reported(indexResult, "zz_big_i"), "the largest index is selected")
		require.True(t, tableResult.Partial, "the count covers every table, not only the rows read")
		require.True(t, indexResult.Partial, "the count covers every index, not only the rows read")

		_, err = admin.Exec(ctx, fmt.Sprintf("TRUNCATE %s.zz_big", schema))
		require.NoError(t, err)
		tableResult, indexResult = scrape()
		require.True(t, reported(tableResult, "zz_big"), "a retained table is read past the SQL limit")
		require.True(t, reported(indexResult, "zz_big_i"), "a retained index is read past the SQL limit")
	})
}
