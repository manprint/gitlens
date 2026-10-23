package check

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&postgisColumnsCheck{}) }

// spatialColumnFactKind is the fact kind of one geometry or geography column.
// The server replaces a database's facts of the kind on every complete
// result, so a dropped column, or a database whose PostGIS was dropped, stops
// being reported.
const spatialColumnFactKind = "spatial_column"

// spatialColumnFactLimit bounds the spatial columns of one database.
// Unindexed columns come first, the largest tables first.
const spatialColumnFactLimit = 500

// postgisColumnsQuery lists the geometry and geography columns of tables,
// partitioned tables and materialized views, with the access method of the
// index that serves spatial searches on each: a valid, non-partial GiST,
// SP-GiST or BRIN index whose first key is the column (BRIN last, as it only
// suits data laid out in spatial order). A partition is left out: its
// partitioned table's index is valid only once every partition has it. The
// types are matched in the schema of the postgis extension, so a database
// without PostGIS returns no rows; the typmod is decoded by the agent, not by
// PostGIS functions. Only the catalogs are read; geometry_columns would
// filter out every table the role cannot SELECT.
const postgisColumnsQuery = `SELECT s.key, s.nspname, s.relname, s.attname, s.typname, s.atttypmod, s.rows, s.method
  FROM (
    SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname) || '.' || pg_catalog.quote_ident(a.attname) AS key,
           n.nspname, c.relname, a.attname, t.typname::pg_catalog.text AS typname, a.atttypmod,
           CASE WHEN c.relkind = 'p'
                THEN COALESCE((SELECT pg_catalog.sum(GREATEST(pc.reltuples, 0))
                                 FROM pg_catalog.pg_partition_tree(c.oid) p
                                 JOIN pg_catalog.pg_class pc ON pc.oid = p.relid
                                WHERE p.isleaf), 0)
                ELSE GREATEST(c.reltuples, 0) END::pg_catalog.float8 AS rows,
           COALESCE((SELECT am.amname::pg_catalog.text
                       FROM pg_catalog.pg_index i
                       JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
                       JOIN pg_catalog.pg_am am ON am.oid = ic.relam
                      WHERE i.indrelid = c.oid AND i.indisvalid AND i.indpred IS NULL
                        AND i.indkey[0] = a.attnum
                        AND am.amname IN ('gist', 'spgist', 'brin')
                      ORDER BY am.amname = 'brin', am.amname
                      LIMIT 1), '') AS method
      FROM pg_catalog.pg_extension e
      JOIN pg_catalog.pg_type t ON t.typnamespace = e.extnamespace AND t.typname IN ('geometry', 'geography')
      JOIN pg_catalog.pg_attribute a ON a.atttypid = t.oid AND a.attnum > 0 AND NOT a.attisdropped
      JOIN pg_catalog.pg_class c ON c.oid = a.attrelid AND c.relkind IN ('r', 'p', 'm') AND NOT c.relispartition
      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
     WHERE e.extname = 'postgis'
       AND n.nspname NOT IN ('pg_catalog', 'information_schema')
       AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
  ) s
 ORDER BY s.method = '' DESC, s.rows DESC, s.key
 LIMIT $1`

// postgisSchemaQuery returns the schema of the postgis extension and whether
// the role may read its spatial_ref_sys table, which PostGIS grants to
// PUBLIC.
const postgisSchemaQuery = `SELECT n.nspname,
       COALESCE(pg_catalog.has_table_privilege(c.oid, 'SELECT'), false)
  FROM pg_catalog.pg_extension e
  JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
  LEFT JOIN pg_catalog.pg_class c ON c.relnamespace = n.oid AND c.relname = 'spatial_ref_sys' AND c.relkind = 'r'
 WHERE e.extname = 'postgis'`

// postgisColumnsCheck reports the spatial columns of each database that has
// PostGIS: their type, subtype, SRID, whether the SRID is defined, and the
// index that serves spatial searches on them.
type postgisColumnsCheck struct{}

func (c *postgisColumnsCheck) Name() string { return "postgis_columns" }

// Requires names no extension: PostGIS is installed per database, and the
// agent checks extensions in its maintenance database only. A database
// without PostGIS returns no spatial column, a complete answer.
func (c *postgisColumnsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *postgisColumnsCheck) DefaultInterval() time.Duration { return time.Hour }
func (c *postgisColumnsCheck) Timeout() time.Duration         { return 30 * time.Second }

type spatialColumn struct {
	key, schema, table, column, typname, method string
	typmod                                      int32
	rows                                        float64
}

func (c *postgisColumnsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}

	var schema string
	var canReadSRS bool
	err = conn.QueryRow(ctx, postgisSchemaQuery).Scan(&schema, &canReadSRS)
	if errors.Is(err, pgx.ErrNoRows) {
		// No PostGIS here: no spatial column, and no fact left over from
		// before it was dropped.
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}

	rows, err := conn.Query(ctx, postgisColumnsQuery, spatialColumnFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	var columns []spatialColumn
	var truncated bool
	for rows.Next() {
		var col spatialColumn
		if err := rows.Scan(&col.key, &col.schema, &col.table, &col.column, &col.typname, &col.typmod, &col.rows, &col.method); err != nil {
			rows.Close()
			return Result{}, err
		}
		if len(columns) == spatialColumnFactLimit {
			truncated = true
			break
		}
		columns = append(columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	known, err := c.knownSRIDs(ctx, conn, schema, canReadSRS, columns)
	if err != nil {
		return Result{}, err
	}

	result := Result{Truncated: truncated}
	var unindexed, unknownSRID int
	for _, col := range columns {
		typ := decodePostGISTypmod(col.typmod)
		srid, sridKnown := "", "unknown"
		if typ.constrained {
			srid = strconv.Itoa(int(typ.srid))
			switch {
			case typ.srid == 0:
				sridKnown = "false"
			case known != nil:
				sridKnown = strconv.FormatBool(known[typ.srid])
			}
		}
		if sridKnown == "false" {
			unknownSRID++
		}
		if col.method == "" {
			unindexed++
		}
		result.Facts = append(result.Facts, Fact{
			Kind: spatialColumnFactKind, Key: col.key, ValueText: col.typname,
			Labels: map[string]string{
				"schemaname": col.schema, "relname": col.table, "column": col.column,
				"subtype": typ.subtype, "srid": srid, "srid_known": sridKnown,
				"index_method": col.method, "rows": strconv.FormatFloat(col.rows, 'f', 0, 64),
			},
		})
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_postgis_spatial_columns", Value: float64(len(columns)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_postgis_unindexed_columns", Value: float64(unindexed), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_postgis_srid_unknown_columns", Value: float64(unknownSRID), Kind: pgtype.KindGauge},
	)
	return result, nil
}

// knownSRIDs returns which of the columns' SRIDs spatial_ref_sys defines, or
// nil when the role may not read it.
func (c *postgisColumnsCheck) knownSRIDs(ctx context.Context, conn Conn, schema string, canRead bool, columns []spatialColumn) (map[int32]bool, error) {
	if !canRead {
		return nil, nil
	}
	wanted := map[int32]bool{}
	for _, col := range columns {
		if typ := decodePostGISTypmod(col.typmod); typ.constrained && typ.srid != 0 {
			wanted[typ.srid] = false
		}
	}
	if len(wanted) == 0 {
		return wanted, nil
	}
	srids := make([]int32, 0, len(wanted))
	for srid := range wanted {
		srids = append(srids, srid)
	}
	sort.Slice(srids, func(i, j int) bool { return srids[i] < srids[j] })
	rows, err := conn.Query(ctx, "SELECT srid FROM "+pgx.Identifier{schema, "spatial_ref_sys"}.Sanitize()+" WHERE srid = ANY($1::pg_catalog.int4[])", srids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var srid int32
		if err := rows.Scan(&srid); err != nil {
			return nil, err
		}
		wanted[srid] = true
	}
	return wanted, rows.Err()
}

// postgisType is a decoded geometry or geography typmod.
type postgisType struct {
	// subtype is the geometry type with its dimensions ("PointZ"), or
	// "Geometry" for any type.
	subtype string
	srid    int32
	// constrained is false for a column declared without a typmod
	// (geometry, not geometry(Point,4326)): each value carries its own
	// SRID, which only reading the rows would tell.
	constrained bool
}

// postgisTypeNames are the geometry types by their PostGIS type number.
var postgisTypeNames = []string{
	"Geometry", "Point", "LineString", "Polygon", "MultiPoint", "MultiLineString",
	"MultiPolygon", "GeometryCollection", "CircularString", "CompoundCurve",
	"CurvePolygon", "MultiCurve", "MultiSurface", "PolyhedralSurface", "Triangle", "Tin",
}

// decodePostGISTypmod decodes a typmod as PostGIS lays it out
// (liblwgeom/gserialized.h): the SRID in bits 8 to 28, sign-extended from
// bit 28, the geometry type in bits 2 to 7, Z in bit 1 and M in bit 0.
func decodePostGISTypmod(typmod int32) postgisType {
	if typmod < 0 {
		return postgisType{subtype: "Geometry"}
	}
	srid := ((typmod & 0x0FFFFF00) - (typmod & 0x10000000)) >> 8
	kind := int((typmod & 0xFC) >> 2)
	name := "Unknown"
	if kind < len(postgisTypeNames) {
		name = postgisTypeNames[kind]
	}
	if typmod&0x2 != 0 {
		name += "Z"
	}
	if typmod&0x1 != 0 {
		name += "M"
	}
	return postgisType{subtype: name, srid: srid, constrained: true}
}
