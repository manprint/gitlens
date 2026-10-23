//go:build integration

package check

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-PART-001: at T0 the partitions check rolls each partition tree up to
// its top table: leaves, depth, default partition rows, invalid partitioned
// indexes, and how far a time-range table's partitions reach. It takes no
// lock on any table, so partitions held ACCESS EXCLUSIVE by another session
// do not stop it.
func TestINTPART001_Partitions(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_part_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
REVOKE ALL ON SCHEMA %[1]s FROM PUBLIC;
CREATE TABLE %[1]s.events (id bigint NOT NULL, at timestamptz NOT NULL, body text) PARTITION BY RANGE (at);
CREATE TABLE %[1]s.events_2026 PARTITION OF %[1]s.events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE %[1]s.events_2027h1 PARTITION OF %[1]s.events FOR VALUES FROM ('2027-01-01') TO ('2027-07-01');
CREATE TABLE %[1]s.events_def PARTITION OF %[1]s.events DEFAULT;
CREATE UNIQUE INDEX ev_uq ON ONLY %[1]s.events (id, at);
CREATE INDEX ev_at ON %[1]s.events (at);
INSERT INTO %[1]s.events SELECT g, '2026-03-01'::timestamptz + g * interval '1 hour', repeat('x', 200) FROM generate_series(1, 3000) g;
INSERT INTO %[1]s.events VALUES (0, '2031-01-01', 'late');
ANALYZE %[1]s.events;
CREATE TABLE %[1]s.logs (kind text NOT NULL, n int NOT NULL) PARTITION BY LIST (kind);
CREATE TABLE %[1]s.logs_a PARTITION OF %[1]s.logs FOR VALUES IN ('a') PARTITION BY RANGE (n);
CREATE TABLE %[1]s.logs_a_1 PARTITION OF %[1]s.logs_a FOR VALUES FROM (0) TO (10);
CREATE TABLE %[1]s.logs_b PARTITION OF %[1]s.logs FOR VALUES IN ('b');
CREATE TABLE %[1]s.forever (d date NOT NULL) PARTITION BY RANGE (d);
CREATE TABLE %[1]s.forever_old PARTITION OF %[1]s.forever FOR VALUES FROM (MINVALUE) TO ('2026-01-01');
CREATE TABLE %[1]s.forever_new PARTITION OF %[1]s.forever FOR VALUES FROM ('2026-01-01') TO (MAXVALUE);
CREATE TABLE %[1]s.bytext (s text NOT NULL) PARTITION BY RANGE (s);
CREATE TABLE %[1]s.bytext_a PARTITION OF %[1]s.bytext FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE TABLE %[1]s.byhash (id int NOT NULL) PARTITION BY HASH (id);
CREATE TABLE %[1]s.byhash_0 PARTITION OF %[1]s.byhash FOR VALUES WITH (MODULUS 2, REMAINDER 0);
CREATE TABLE %[1]s.byhash_1 PARTITION OF %[1]s.byhash FOR VALUES WITH (MODULUS 2, REMAINDER 1)`, schema))
		require.NoError(t, err)

		// Another session holds partitions of two trees, and the top of a
		// third, as ALTER TABLE or VACUUM FULL would.
		holder, err := admin.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = holder.Rollback(context.Background()) }()
		_, err = holder.Exec(ctx, fmt.Sprintf("LOCK TABLE %[1]s.events_2026, %[1]s.logs_a_1, %[1]s.forever IN ACCESS EXCLUSIVE MODE", schema))
		require.NoError(t, err)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		started := time.Now()
		result, err := (&partitionsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Less(t, time.Since(started), 900*time.Millisecond, "the check waited for a lock")
		require.False(t, result.Truncated)
		facts := map[string]Fact{}
		for _, f := range result.Facts {
			if f.Labels["schemaname"] == schema {
				facts[f.Labels["relname"]] = f
			}
		}
		require.ElementsMatch(t, []string{"events", "logs", "forever", "bytext", "byhash"}, keysOf(facts))

		events := facts["events"]
		require.Equal(t, "range", events.ValueText)
		require.Equal(t, "RANGE (at)", events.Labels["partition_key"])
		require.Equal(t, "3", events.Labels["partitions"])
		require.Equal(t, "3", events.Labels["direct_partitions"])
		require.Equal(t, "1", events.Labels["depth"])
		require.Equal(t, schema+".events_def", events.Labels["default_partition"])
		require.Equal(t, "1", events.Labels["default_rows"])
		require.Equal(t, "3001", events.Labels["rows"])
		bytes, err := strconv.ParseInt(events.Labels["bytes"], 10, 64)
		require.NoError(t, err)
		require.Greater(t, bytes, int64(500_000))
		require.Equal(t, "ev_uq", events.Labels["invalid_indexes"])
		require.Equal(t, "1", events.Labels["invalid_unique_indexes"])
		require.Equal(t, "2027-07-01T00:00:00Z", events.Labels["covered_until"])
		require.NotEmpty(t, events.Labels["analyzed_at"])

		logs := facts["logs"]
		require.Equal(t, "list", logs.ValueText)
		require.Equal(t, "2", logs.Labels["partitions"])
		require.Equal(t, "2", logs.Labels["direct_partitions"])
		require.Equal(t, "2", logs.Labels["depth"])
		require.Empty(t, logs.Labels["default_partition"])
		require.Empty(t, logs.Labels["covered_until"])
		require.Empty(t, logs.Labels["invalid_indexes"])
		require.Empty(t, logs.Labels["analyzed_at"])

		require.Equal(t, "unbounded", facts["forever"].Labels["covered_until"])
		// A text range key is not read as a time, even when a bound looks
		// like one.
		require.Empty(t, facts["bytext"].Labels["covered_until"])
		require.Equal(t, "hash", facts["byhash"].ValueText)
	})
}
