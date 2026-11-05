package check

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&tableColumnsCheck{}) }

// tableColumnsFactKind is the fact kind of one table's column inventory. The
// server replaces a database's facts of the kind on every complete result, so
// a dropped table, or a dropped column, stops being reported.
const tableColumnsFactKind = "table_columns"

// tableIndexesFactKind is the fact kind of one table's valid index definitions,
// reported with the table's columns. Unlike index_def, which covers only the
// indexes index_stats selects, it lists every index of the table, so the
// server can tell that no index leads with a column.
const tableIndexesFactKind = "table_indexes"

// tableColumnsFactLimit bounds the facts of one database, largest tables
// first. The inventory sizes the indexes the server suggests, and a table
// outside the limit gets a suggestion without a size estimate.
const tableColumnsFactLimit = 1000

// tableColumnsQuery lists the tables of a database with the columns of each as
// a JSON array: name, attribute number, type, fixed length (-1 for a variable
// length type, -2 for a C string), NOT NULL and, where the role may read
// pg_stats, the planner's n_distinct, null_frac and avg_width. At the
// read-only tier pg_stats has no row for a table the role cannot select from,
// so the statistics are left out and the inventory still reports every
// column; the server then says the selectivity is unknown. The size of a table
// is relpages and reltuples as of the last VACUUM or ANALYZE: only catalogs
// are read, no relation is measured or locked. The last column is the JSON
// array of the table's valid index definitions (pg_get_indexdef). A view or a foreign table has
// no index to size, so only ordinary, partitioned and materialized tables are
// listed.
const tableColumnsQuery = `WITH st AS MATERIALIZED (
  SELECT DISTINCT ON (schemaname, tablename, attname)
         schemaname, tablename, attname, n_distinct, null_frac, avg_width
    FROM pg_catalog.pg_stats
   WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
   ORDER BY schemaname, tablename, attname, inherited)
SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname),
       n.nspname, c.relname, c.relkind::pg_catalog.text,
       c.reltuples::pg_catalog.float8, c.relpages::pg_catalog.int8,
       COALESCE((SELECT pg_catalog.jsonb_strip_nulls(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
                         'name', a.attname, 'attnum', a.attnum,
                         'type', pg_catalog.format_type(a.atttypid, a.atttypmod),
                         'typlen', t.typlen, 'notnull', a.attnotnull,
                         'n_distinct', st.n_distinct, 'null_frac', st.null_frac,
                         'avg_width', st.avg_width) ORDER BY a.attnum))::pg_catalog.text
                   FROM pg_catalog.pg_attribute a
                   JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
                   LEFT JOIN st ON st.schemaname = n.nspname AND st.tablename = c.relname AND st.attname = a.attname
                  WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped), '[]'),
       COALESCE((SELECT pg_catalog.jsonb_agg(pg_catalog.pg_get_indexdef(i.indexrelid) ORDER BY i.indexrelid)::pg_catalog.text
                   FROM pg_catalog.pg_index i
                  WHERE i.indrelid = c.oid AND i.indisvalid), '[]')
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
 WHERE c.relkind IN ('r', 'p', 'm')
   AND n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
 ORDER BY c.relpages DESC, 1
 LIMIT $1`

// tableColumnsCheck reports the columns of each table of a database, for the
// size estimates and the selectivity of the index suggestions.
type tableColumnsCheck struct{}

func (c *tableColumnsCheck) Name() string { return "table_columns" }
func (c *tableColumnsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}

// DefaultInterval is long: a schema changes rarely, and the statistics move
// slowly between two ANALYZE runs.
func (c *tableColumnsCheck) DefaultInterval() time.Duration { return time.Hour }
func (c *tableColumnsCheck) Timeout() time.Duration         { return 30 * time.Second }

func (c *tableColumnsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, tableColumnsQuery, tableColumnsFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	for rows.Next() {
		var key, schema, table, kind, columns, indexes string
		var tuples float64
		var pages int64
		if err := rows.Scan(&key, &schema, &table, &kind, &tuples, &pages, &columns, &indexes); err != nil {
			return Result{}, err
		}
		// Two facts per table: its columns and its indexes.
		if len(result.Facts) == 2*tableColumnsFactLimit {
			result.Truncated = true
			break
		}
		result.Facts = append(result.Facts, Fact{
			Kind: tableColumnsFactKind, Key: key, ValueJSON: json.RawMessage(columns),
			Labels: map[string]string{
				"schemaname": schema, "relname": table, "relkind": kind,
				"reltuples": strconv.FormatFloat(tuples, 'f', -1, 64),
				"relpages":  strconv.FormatInt(pages, 10),
			},
		}, Fact{
			Kind: tableIndexesFactKind, Key: key, ValueJSON: json.RawMessage(indexes),
			Labels: map[string]string{"schemaname": schema, "relname": table},
		})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}
