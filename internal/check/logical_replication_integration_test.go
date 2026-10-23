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

// INT-LREP-001: at T0 the logical_replication check reports each
// subscription with its database, slot, publications and whether its apply
// worker runs, without reading the connection string, and the decoding
// counters of the logical slots. A subscription in one database of the
// instance replicates from another, so no second server is needed.
func TestINTLREP001_LogicalReplication(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		suffix := testSuffix()
		subDB := fmt.Sprintf("pglens_lrep_%d", suffix)
		slot := fmt.Sprintf("pglens_lrep_%d", suffix)

		_, err := admin.Exec(ctx, `CREATE TABLE lrep_src (id int PRIMARY KEY);
CREATE PUBLICATION lrep_pub FOR TABLE lrep_src`)
		require.NoError(t, err)
		_, err = admin.Exec(ctx, "SELECT pg_create_logical_replication_slot($1, 'pgoutput')", slot)
		require.NoError(t, err)
		t.Cleanup(func() {
			ctx := context.Background()
			_, _ = admin.Exec(ctx, "SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1", slot)
			_, _ = admin.Exec(ctx, "DROP PUBLICATION IF EXISTS lrep_pub; DROP TABLE IF EXISTS lrep_src")
		})
		sub := pg.Pool(t, subDB, pgtest.RoleSuperuser)
		t.Cleanup(func() {
			ctx := context.Background()
			sub.Close()
			_, _ = admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", subDB))
		})
		_, err = sub.Exec(ctx, "CREATE TABLE lrep_src (id int PRIMARY KEY)")
		require.NoError(t, err)
		_, err = sub.Exec(ctx, fmt.Sprintf(`CREATE SUBSCRIPTION lrep_on
  CONNECTION 'host=/var/run/postgresql dbname=app user=postgres password=test'
  PUBLICATION lrep_pub WITH (create_slot = false, slot_name = '%s', copy_data = false)`, slot))
		require.NoError(t, err)
		_, err = sub.Exec(ctx, `CREATE SUBSCRIPTION lrep_off
  CONNECTION 'host=/var/run/postgresql dbname=app user=postgres password=secret'
  PUBLICATION lrep_pub WITH (connect = false)`)
		require.NoError(t, err)
		t.Cleanup(func() {
			ctx := context.Background()
			_, _ = sub.Exec(ctx, "ALTER SUBSCRIPTION lrep_off SET (slot_name = NONE)")
			_, _ = sub.Exec(ctx, "DROP SUBSCRIPTION IF EXISTS lrep_off")
			_, _ = sub.Exec(ctx, "DROP SUBSCRIPTION IF EXISTS lrep_on")
		})

		_, err = admin.Exec(ctx, "INSERT INTO lrep_src VALUES (1), (2)")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			var n int
			return sub.QueryRow(ctx, "SELECT count(*) FROM lrep_src").Scan(&n) == nil && n == 2
		}, 30*time.Second, 200*time.Millisecond, "the subscription did not apply the rows")

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		result, err := (&logicalReplicationCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		facts := map[string]Fact{}
		for _, f := range result.Facts {
			facts[f.Key] = f
		}
		require.Contains(t, facts, "lrep_on")
		require.Contains(t, facts, "lrep_off")

		on := facts["lrep_on"]
		require.Equal(t, "enabled", on.ValueText)
		require.Equal(t, subDB, on.Labels["database"])
		require.Equal(t, slot, on.Labels["slot_name"])
		require.Equal(t, "lrep_pub", on.Labels["publications"])
		require.Equal(t, "true", on.Labels["apply_worker"])
		require.NotEmpty(t, on.Labels["last_msg_receipt_at"])
		require.Equal(t, "0", on.Labels["apply_errors"])

		off := facts["lrep_off"]
		require.Equal(t, "disabled", off.ValueText)
		require.Equal(t, "false", off.Labels["apply_worker"])
		for _, f := range result.Facts {
			for _, v := range f.Labels {
				require.NotContains(t, v, "password")
			}
		}

		slots := map[string]bool{}
		for _, m := range result.Metrics {
			if m.Name == "pg_logical_slot_bytes_total" {
				slots[m.Labels["slot_name"]] = true
			}
		}
		require.True(t, slots[slot], "no decoding counters for slot %s", slot)
	})
}
