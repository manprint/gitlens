package check

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/manprint/pglens/internal/pgtype"
	"github.com/stretchr/testify/require"
)

// postgisTypmod builds a typmod as PostGIS's TYPMOD_SET_* macros do.
func postgisTypmod(srid int32, kind int32, z, m bool) int32 {
	typmod := (srid & 0x001FFFFF) << 8
	typmod |= kind << 2
	if z {
		typmod |= 0x2
	}
	if m {
		typmod |= 0x1
	}
	return typmod
}

func TestDecodePostGISTypmod(t *testing.T) {
	require.Equal(t, postgisType{subtype: "Geometry"}, decodePostGISTypmod(-1))
	require.Equal(t, postgisType{subtype: "Point", srid: 4326, constrained: true}, decodePostGISTypmod(postgisTypmod(4326, 1, false, false)))
	require.Equal(t, postgisType{subtype: "MultiPolygonZ", srid: 3857, constrained: true}, decodePostGISTypmod(postgisTypmod(3857, 6, true, false)))
	require.Equal(t, postgisType{subtype: "LineStringZM", srid: 0, constrained: true}, decodePostGISTypmod(postgisTypmod(0, 2, true, true)))
	require.Equal(t, postgisType{subtype: "Geometry", srid: 999999, constrained: true}, decodePostGISTypmod(postgisTypmod(999999, 0, false, false)))
	require.Equal(t, postgisType{subtype: "Unknown", srid: 1, constrained: true}, decodePostGISTypmod(postgisTypmod(1, 40, false, false)))
}

func TestPostGISColumns_Requirements(t *testing.T) {
	c := &postgisColumnsCheck{}
	require.Equal(t, "postgis_columns", c.Name())
	require.Equal(t, ScopeDatabase, c.Requires().Scope)
	require.Equal(t, pgtype.TierReadOnly, c.Requires().PermTier)
	require.Empty(t, c.Requires().Extensions, "PostGIS is per database: gating on the maintenance database would skip it")
	require.Equal(t, time.Hour, c.DefaultInterval())
}

func scrapePostGISForTest(t *testing.T, schemaRow *mockRow, columns [][]any, srids [][]any) (Result, []string) {
	t.Helper()
	var queries []string
	conn := &mockConn{
		queryRowFunc: func(_ context.Context, sql string, _ ...any) pgx.Row {
			require.Equal(t, postgisSchemaQuery, sql)
			return schemaRow
		},
		queryFunc: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queries = append(queries, sql)
			if sql == postgisColumnsQuery {
				require.Equal(t, []any{spatialColumnFactLimit + 1}, args)
				return &mockRows{rows: columns}, nil
			}
			require.Equal(t, `SELECT srid FROM "gis"."spatial_ref_sys" WHERE srid = ANY($1::pg_catalog.int4[])`, sql)
			require.Equal(t, []any{[]int32{3857, 4326}}, args)
			return &mockRows{rows: srids}, nil
		},
	}
	r, err := (&postgisColumnsCheck{}).Scrape(context.Background(), maintenanceTarget(conn))
	require.NoError(t, err)
	require.Equal(t, 1, conn.releaseCalls)
	return r, queries
}

func spatialRow(column string, typmod int32, method string) []any {
	return []any{"public.places." + column, "public", "places", column, "geometry", typmod, float64(25000), method}
}

func TestPostGISColumns_ReportsColumns(t *testing.T) {
	r, queries := scrapePostGISForTest(t, &mockRow{vals: []any{"gis", true}},
		[][]any{
			spatialRow("area", postgisTypmod(3857, 6, false, false), ""),
			spatialRow("track", postgisTypmod(0, 2, false, false), ""),
			spatialRow("pt", postgisTypmod(4326, 1, false, false), "gist"),
			spatialRow("loose", -1, "brin"),
		},
		[][]any{{int32(4326)}})
	require.Len(t, queries, 2)
	require.False(t, r.Truncated)
	byColumn := map[string]Fact{}
	for _, f := range r.Facts {
		require.Equal(t, spatialColumnFactKind, f.Kind)
		byColumn[f.Labels["column"]] = f
	}
	require.Equal(t, map[string]string{
		"schemaname": "public", "relname": "places", "column": "area", "subtype": "MultiPolygon",
		"srid": "3857", "srid_known": "false", "index_method": "", "rows": "25000",
	}, byColumn["area"].Labels)
	require.Equal(t, "false", byColumn["track"].Labels["srid_known"], "SRID 0 is unknown")
	require.Equal(t, "true", byColumn["pt"].Labels["srid_known"])
	require.Equal(t, "", byColumn["loose"].Labels["srid"])
	require.Equal(t, "unknown", byColumn["loose"].Labels["srid_known"])
	require.ElementsMatch(t, []pgtype.Metric{
		{Name: "pg_postgis_spatial_columns", Value: 4, Kind: pgtype.KindGauge},
		{Name: "pg_postgis_unindexed_columns", Value: 2, Kind: pgtype.KindGauge},
		{Name: "pg_postgis_srid_unknown_columns", Value: 2, Kind: pgtype.KindGauge},
	}, r.Metrics)
}

// A role that may not read spatial_ref_sys leaves non-zero SRIDs unknown and
// never queries the table.
func TestPostGISColumns_UnreadableSpatialRefSys(t *testing.T) {
	r, queries := scrapePostGISForTest(t, &mockRow{vals: []any{"gis", false}},
		[][]any{spatialRow("pt", postgisTypmod(4326, 1, false, false), "gist"), spatialRow("track", postgisTypmod(0, 2, false, false), "gist")}, nil)
	require.Equal(t, []string{postgisColumnsQuery}, queries)
	require.Equal(t, "unknown", r.Facts[0].Labels["srid_known"])
	require.Equal(t, "false", r.Facts[1].Labels["srid_known"])
}

// A database without PostGIS is a complete answer with nothing in it, so
// the server removes the facts of a PostGIS since dropped.
func TestPostGISColumns_NoPostGIS(t *testing.T) {
	r, queries := scrapePostGISForTest(t, &mockRow{err: pgx.ErrNoRows}, nil, nil)
	require.Empty(t, queries)
	require.Empty(t, r.Facts)
	require.False(t, r.Truncated)
}

func TestPostGISColumns_QueriesReadCatalogsOnly(t *testing.T) {
	for _, q := range []string{postgisColumnsQuery, postgisSchemaQuery} {
		require.NotContains(t, strings.ToLower(q), "geometry_columns")
		require.NotRegexp(t, `(?i)\b(st|postgis)_\w+\s*\(`, q, "no PostGIS function is called")
	}
}
