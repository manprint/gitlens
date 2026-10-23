//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-FKI-001: at T0 the foreign-key index check reports the foreign keys no
// valid, non-partial index leads with, whatever the column order of a
// composite key, and checks a partitioned table once, against its
// partitioned indexes.
func TestINTFKI001_UnindexedForeignKeys(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_fki_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
REVOKE ALL ON SCHEMA %[1]s FROM PUBLIC;
CREATE TABLE %[1]s.parent (a int, b int, PRIMARY KEY (a, b));
-- Served: an index leading with the column, then the composite in reverse order.
CREATE TABLE %[1]s.covered (id int, a int, b int, FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
CREATE INDEX ON %[1]s.covered (b, a, id);
CREATE TABLE %[1]s.hashed (a int, b int, FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
-- Not served: no index, the wrong leading column, a partial index, and an
-- index whose only matching column is an INCLUDE column.
CREATE TABLE %[1]s.bare (a int, b int, CONSTRAINT bare_fk FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
INSERT INTO %[1]s.parent SELECT g, g FROM generate_series(1, 100) g;
INSERT INTO %[1]s.bare SELECT g, g FROM generate_series(1, 100) g;
ANALYZE %[1]s.bare;
CREATE TABLE %[1]s.wrong_lead (id int, a int, b int, CONSTRAINT wrong_lead_fk FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
CREATE INDEX ON %[1]s.wrong_lead (id, a, b);
CREATE TABLE %[1]s.partial (a int, b int, CONSTRAINT partial_fk FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
CREATE INDEX ON %[1]s.partial (a, b) WHERE a > 0;
CREATE TABLE %[1]s.included (id int, a int, b int, CONSTRAINT included_fk FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b));
CREATE INDEX ON %[1]s.included (a) INCLUDE (b);
-- A partitioned table: one finding for its constraint, none for the clones.
CREATE TABLE %[1]s.events (at date NOT NULL, a int, b int, CONSTRAINT events_fk FOREIGN KEY (a, b) REFERENCES %[1]s.parent (a, b)) PARTITION BY RANGE (at);
CREATE TABLE %[1]s.events_2026 PARTITION OF %[1]s.events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE %[1]s.events_2027 PARTITION OF %[1]s.events FOR VALUES FROM ('2027-01-01') TO ('2028-01-01')`, schema))
		require.NoError(t, err)
		// A single-column foreign key a hash index serves.
		_, err = admin.Exec(ctx, fmt.Sprintf(`ALTER TABLE %[1]s.hashed DROP CONSTRAINT hashed_a_b_fkey;
CREATE TABLE %[1]s.single (id int PRIMARY KEY);
ALTER TABLE %[1]s.hashed ADD FOREIGN KEY (a) REFERENCES %[1]s.single (id);
CREATE INDEX ON %[1]s.hashed USING hash (a)`, schema))
		require.NoError(t, err)

		scrape := func() map[string]Fact {
			// Another session holds a reported table and a partition, as
			// ALTER TABLE would: the check takes no lock, so it does not
			// wait for them.
			holder, err := admin.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = holder.Rollback(context.Background()) }()
			_, err = holder.Exec(ctx, fmt.Sprintf("LOCK TABLE %[1]s.bare, %[1]s.events_2026 IN ACCESS EXCLUSIVE MODE", schema))
			require.NoError(t, err)
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			started := time.Now()
			result, err := (&fkIndexesCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
			require.NoError(t, err)
			require.Less(t, time.Since(started), 900*time.Millisecond, "the check waited for a lock")
			require.False(t, result.Truncated)
			facts := map[string]Fact{}
			for _, f := range result.Facts {
				if f.Labels["schemaname"] == schema {
					facts[f.Labels["constraint"]] = f
				}
			}
			return facts
		}

		facts := scrape()
		require.ElementsMatch(t, []string{"bare_fk", "wrong_lead_fk", "partial_fk", "included_fk", "events_fk"}, keysOf(facts))
		bare := facts["bare_fk"]
		require.Equal(t, "a, b", bare.ValueText)
		require.Equal(t, "a, b", bare.Labels["columns_sql"])
		require.Equal(t, schema+".parent", bare.Labels["referenced"])
		require.Equal(t, "bare", bare.Labels["relname"])
		require.Equal(t, schema+".bare.bare_fk", bare.Key)
		require.NotEqual(t, "0", bare.Labels["table_bytes"])
		require.Equal(t, "events", facts["events_fk"].Labels["relname"])

		// An index on the partitioned table serves its foreign key once
		// every partition has it.
		_, err = admin.Exec(ctx, fmt.Sprintf("CREATE INDEX ON %[1]s.events (a, b); CREATE INDEX ON %[1]s.bare (a, b)", schema))
		require.NoError(t, err)
		facts = scrape()
		require.ElementsMatch(t, []string{"wrong_lead_fk", "partial_fk", "included_fk"}, keysOf(facts))
	})
}
