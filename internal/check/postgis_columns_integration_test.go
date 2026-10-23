//go:build integration

package check

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manprint/pglens/test/pgtest"
	"github.com/stretchr/testify/require"
)

// INT-GIS-001: at T0 the PostGIS column check reports every geometry and
// geography column with the subtype and SRID format_type prints for it,
// whether spatial_ref_sys defines the SRID, and the index serving spatial
// searches, unindexed columns first.
func TestINTGIS001_SpatialColumns(t *testing.T) {
	pgtest.ForEachPostGIS(t, func(t *testing.T, pg *pgtest.PG) {
		pg.Lock(t)
		ctx := context.Background()
		admin := pg.Pool(t, "app", pgtest.RoleSuperuser)
		pool := pg.Pool(t, "app", pgtest.RoleT0)
		schema := fmt.Sprintf("pglens_gis_%d", testSuffix())
		t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE") })
		_, err := admin.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %[1]s;
REVOKE ALL ON SCHEMA %[1]s FROM PUBLIC;
CREATE TABLE %[1]s.places (id int, pt geometry(Point, 4326), area geometry(MultiPolygonZ, 3857), track geometry(LineStringM), loose geometry, odd geometry(Point, 990000), geog geography(PointZ, 4326), anyg geography);
CREATE INDEX ON %[1]s.places USING gist (pt);
CREATE INDEX ON %[1]s.places USING brin (loose);
CREATE INDEX ON %[1]s.places USING gist (track) WHERE id > 0;
CREATE INDEX ON %[1]s.places USING gist (geog);
CREATE INDEX ON %[1]s.places (id, odd);
CREATE TABLE %[1]s.zones (at date NOT NULL, shape geometry(Polygon, 4326)) PARTITION BY RANGE (at);
CREATE TABLE %[1]s.zones_2026 PARTITION OF %[1]s.zones FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE INDEX ON %[1]s.zones USING spgist (shape);
CREATE MATERIALIZED VIEW %[1]s.centroids AS SELECT id, pt FROM %[1]s.places;
CREATE VIEW %[1]s.v AS SELECT pt FROM %[1]s.places`, schema))
		require.NoError(t, err)

		// Another session holds a table and a partition, as ALTER TABLE
		// would: the check takes no lock, so it does not wait for them.
		holder, err := admin.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = holder.Rollback(context.Background()) }()
		_, err = holder.Exec(ctx, fmt.Sprintf("LOCK TABLE %[1]s.places, %[1]s.zones_2026 IN ACCESS EXCLUSIVE MODE", schema))
		require.NoError(t, err)

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		started := time.Now()
		result, err := (&postgisColumnsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Less(t, time.Since(started), 900*time.Millisecond, "the check waited for a lock")
		require.False(t, result.Truncated)
		facts := map[string]Fact{}
		var order []string
		for _, f := range result.Facts {
			if f.Labels["schemaname"] == schema {
				name := f.Labels["relname"] + "." + f.Labels["column"]
				facts[name] = f
				order = append(order, name)
			}
		}
		require.ElementsMatch(t, []string{
			"places.pt", "places.area", "places.track", "places.loose", "places.odd",
			"places.geog", "places.anyg", "zones.shape", "centroids.pt",
		}, order)
		// Unindexed columns first.
		for i, name := range order {
			if facts[name].Labels["index_method"] != "" {
				for _, later := range order[i:] {
					require.NotEmpty(t, facts[later].Labels["index_method"], "%s after an indexed column", later)
				}
				break
			}
		}

		// The decoded subtype and SRID match what PostGIS prints.
		for name, f := range facts {
			var printed string
			parts := strings.SplitN(name, ".", 2)
			require.NoError(t, admin.QueryRow(ctx, `SELECT format_type(a.atttypid, a.atttypmod) FROM pg_attribute a
  WHERE a.attrelid = $1::regclass AND a.attname = $2`, schema+"."+parts[0], parts[1]).Scan(&printed))
			// format_type leaves out SRID 0.
			want := f.ValueText
			switch f.Labels["srid"] {
			case "":
			case "0":
				want = fmt.Sprintf("%s(%s)", f.ValueText, f.Labels["subtype"])
			default:
				want = fmt.Sprintf("%s(%s,%s)", f.ValueText, f.Labels["subtype"], f.Labels["srid"])
			}
			require.Equal(t, printed, want, name)
		}

		require.Equal(t, "gist", facts["places.pt"].Labels["index_method"])
		require.Equal(t, "true", facts["places.pt"].Labels["srid_known"])
		require.Equal(t, "geometry", facts["places.pt"].ValueText)
		require.Equal(t, "brin", facts["places.loose"].Labels["index_method"])
		require.Equal(t, "", facts["places.loose"].Labels["srid"])
		require.Equal(t, "unknown", facts["places.loose"].Labels["srid_known"])
		require.Equal(t, "", facts["places.track"].Labels["index_method"], "a partial index does not serve")
		require.Equal(t, "0", facts["places.track"].Labels["srid"])
		require.Equal(t, "false", facts["places.track"].Labels["srid_known"])
		require.Equal(t, "", facts["places.odd"].Labels["index_method"], "a btree index does not serve")
		require.Equal(t, "false", facts["places.odd"].Labels["srid_known"])
		require.Equal(t, "geography", facts["places.geog"].ValueText)
		require.Equal(t, "gist", facts["places.geog"].Labels["index_method"])
		require.Equal(t, "spgist", facts["zones.shape"].Labels["index_method"])
		require.Equal(t, "", facts["centroids.pt"].Labels["index_method"])
		_, err = strconv.Atoi(facts["places.pt"].Labels["rows"])
		require.NoError(t, err)
	})
}

// INT-GIS-002: in a database without PostGIS the check reports nothing,
// a complete answer.
func TestINTGIS002_NoPostGIS(t *testing.T) {
	pgtest.ForEach(t, func(t *testing.T, pg *pgtest.PG) {
		ctx := context.Background()
		conn, err := pg.Pool(t, "app", pgtest.RoleT0).Acquire(ctx)
		require.NoError(t, err)
		result, err := (&postgisColumnsCheck{}).Scrape(ctx, &registryTestTarget{conn: conn, version: pg.Version, database: "app", permTier: 0})
		require.NoError(t, err)
		require.Empty(t, result.Facts)
		require.Empty(t, result.Metrics)
		require.False(t, result.Truncated)
	})
}
