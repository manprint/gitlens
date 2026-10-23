//go:build integration

package check

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-SEQ-001: the sequences check links each sequence to the column it
// feeds and counts its range against the narrower of the two. At T0 a value
// is read only where the role was granted SELECT on the sequence, and the
// column overflow is reported without any value; at T1 every value is read.
// No sequence advances.
func TestINTSEQ001_Sequences(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		t0 := pg.Pool(t, "app", pgtest.RoleT0)
		t1 := pg.Pool(t, "app", pgtest.RoleT1)
		schema := fmt.Sprintf("pglens_seq_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
GRANT USAGE ON SCHEMA %[1]s TO PUBLIC;
CREATE TABLE %[1]s.orders (id serial PRIMARY KEY);
SELECT setval('%[1]s.orders_id_seq', 1800000000);
CREATE SEQUENCE %[1]s.wide AS bigint;
CREATE TABLE %[1]s.narrow (id integer DEFAULT nextval('%[1]s.wide'));
SELECT setval('%[1]s.wide', 2000000000);
CREATE TABLE %[1]s.events (id bigint GENERATED ALWAYS AS IDENTITY, at date NOT NULL) PARTITION BY RANGE (at);
CREATE TABLE %[1]s.events_2026 PARTITION OF %[1]s.events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE SEQUENCE %[1]s.fresh;
CREATE SEQUENCE %[1]s.down INCREMENT -1 MINVALUE -100 MAXVALUE -1 START -1;
SELECT setval('%[1]s.down', -91);
CREATE SEQUENCE %[1]s.ring MAXVALUE 10 CYCLE;
SELECT setval('%[1]s.ring', 9);
CREATE SEQUENCE %[1]s.shared;
CREATE TABLE %[1]s.tiny (id smallint DEFAULT nextval('%[1]s.shared'));
CREATE TABLE %[1]s.big (id bigint DEFAULT nextval('%[1]s.shared'));
SELECT setval('%[1]s.shared', 30000);
GRANT SELECT ON SEQUENCE %[1]s.orders_id_seq TO pglens`, schema))
		require.NoError(t, err)
		values := func() map[string]string {
			rows, err := admin.Query(ctx, `SELECT sequencename || ' ' || COALESCE(last_value::text, '') FROM pg_sequences WHERE schemaname = $1 ORDER BY 1`, schema)
			require.NoError(t, err)
			defer rows.Close()
			out := map[string]string{}
			for rows.Next() {
				var line string
				require.NoError(t, rows.Scan(&line))
				out[line] = line
			}
			return out
		}
		before := values()

		scrape := func(pool *pgxpool.Pool, tier pgtype.PermTier) (Result, map[string]Fact) {
			t.Helper()
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)
			result, err := (&sequencesCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: tier})
			require.NoError(t, err)
			facts := map[string]Fact{}
			for _, f := range result.Facts {
				if f.Labels["schemaname"] == schema {
					facts[f.Labels["sequence"]] = f
				}
			}
			return result, facts
		}

		result, facts := scrape(t0, pgtype.TierReadOnly)
		require.False(t, result.Truncated)
		require.False(t, result.Partial)
		require.ElementsMatch(t, []string{"orders_id_seq", "wide", "events_id_seq", "fresh", "down", "ring", "shared"}, keysOf(facts))

		orders := facts["orders_id_seq"]
		require.Equal(t, "used", orders.ValueText)
		require.Equal(t, map[string]string{
			"schemaname": schema, "sequence": "orders_id_seq", "data_type": "integer",
			"start": "1", "increment": "1", "min": "1", "max": "2147483647", "cycle": "false",
			"table_schema": schema, "table": "orders", "column": "id", "column_type": "integer",
			"link": "owned", "columns": "1", "limit": "2147483647", "limit_source": "sequence", "exhausts": "true",
			"last_value": "1800000000", "used_ratio": "0.838190", "remaining": "347483647",
		}, orders.Labels)

		// Hidden at T0, but the integer column is known to overflow first.
		wide := facts["wide"]
		require.Equal(t, "hidden", wide.ValueText)
		require.Equal(t, "default", wide.Labels["link"])
		require.Equal(t, "narrow", wide.Labels["table"])
		require.Equal(t, "2147483647", wide.Labels["limit"])
		require.Equal(t, "column", wide.Labels["limit_source"])
		require.Empty(t, wide.Labels["used_ratio"])
		require.Empty(t, wide.Labels["last_value"])

		// An identity column of a partitioned table is reported on the
		// partitioned table, not on its partition.
		events := facts["events_id_seq"]
		require.Equal(t, "identity", events.Labels["link"])
		require.Equal(t, "events", events.Labels["table"])

		// The narrowest of the columns a shared sequence feeds sets its limit.
		shared := facts["shared"]
		require.Equal(t, "tiny", shared.Labels["table"])
		require.Equal(t, "smallint", shared.Labels["column_type"])
		require.Equal(t, "2", shared.Labels["columns"])
		require.Equal(t, "32767", shared.Labels["limit"])

		require.Equal(t, "", facts["fresh"].Labels["link"])
		require.Equal(t, "0", facts["fresh"].Labels["columns"])

		result, facts = scrape(t1, pgtype.TierExplain)
		require.False(t, result.Partial)
		require.Equal(t, "used", facts["wide"].ValueText)
		require.Equal(t, "0.931323", facts["wide"].Labels["used_ratio"])
		require.Equal(t, "147483647", facts["wide"].Labels["remaining"])
		require.Equal(t, "0.915553", facts["shared"].Labels["used_ratio"])
		require.Equal(t, "2767", facts["shared"].Labels["remaining"])

		fresh := facts["fresh"]
		require.Equal(t, "unused", fresh.ValueText)
		require.Equal(t, "0.000000", fresh.Labels["used_ratio"])
		require.Equal(t, "9223372036854775807", fresh.Labels["remaining"])

		down := facts["down"]
		require.Equal(t, "-100", down.Labels["limit"])
		require.Equal(t, "0.909091", down.Labels["used_ratio"])
		require.Equal(t, "9", down.Labels["remaining"])

		ring := facts["ring"]
		require.Equal(t, "false", ring.Labels["exhausts"])
		require.Equal(t, "true", ring.Labels["cycle"])

		// The highest used fraction first.
		var order []string
		for _, f := range result.Facts {
			if f.Labels["schemaname"] == schema {
				order = append(order, f.Labels["sequence"])
			}
		}
		require.Equal(t, []string{"wide", "shared", "down", "ring", "orders_id_seq"}, order[:5])

		require.Equal(t, before, values(), "a sequence advanced")
	})
}
