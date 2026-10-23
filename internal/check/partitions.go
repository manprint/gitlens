package check

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&partitionsCheck{}) }

// partitionedTableFactKind is the fact kind of one partitioned table at the
// top of its tree. The server replaces a database's facts of the kind on
// every complete result, so a dropped tree stops being reported.
const partitionedTableFactKind = "partitioned_table"

// partitionedTableFactLimit bounds the partition trees of one database,
// largest first.
const partitionedTableFactLimit = 200

// partitionsQuery lists the partitioned tables that are not themselves
// partitions, each with its whole tree rolled up: leaf partitions, depth,
// estimated size of the leaves with their indexes and TOAST, estimated rows,
// the default partition and its estimated rows, the partitioned indexes that
// are not valid (a partition lacks the index), and when the table was last
// analyzed (autovacuum never analyzes a partitioned table). For a table
// range-partitioned on one date or timestamp column, covered_until is the
// highest upper bound of its partitions, read back from the bound
// expressions PostgreSQL prints; covered_forever is set when a partition
// reaches MAXVALUE.
//
// Nothing here takes a lock on a table. pg_partition_tree, the size
// functions and pg_get_expr with a relation each lock every partition they
// touch, so one partition held by ALTER TABLE or VACUUM FULL would make the
// check wait out lock_timeout and fail, and a large tree would fill the
// shared lock table. The tree is walked through pg_inherits instead, sizes
// come from relpages (as of the last VACUUM or ANALYZE), and bounds are
// printed without a relation, which they do not need: they hold constants
// only. pg_get_partkeydef locks the table as well, so the partition key is
// spelled from pg_attribute, an expression in it as "expression".
const partitionsQuery = `WITH RECURSIVE roots AS (
    SELECT c.oid
      FROM pg_catalog.pg_partitioned_table pt
      JOIN pg_catalog.pg_class c ON c.oid = pt.partrelid AND NOT c.relispartition
      JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
       AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
  ), tree (root, relid, level) AS (
    SELECT oid, oid, 0 FROM roots
    UNION ALL
    SELECT t.root, i.inhrelid, t.level + 1
      FROM tree t
      JOIN pg_catalog.pg_inherits i ON i.inhparent = t.relid
  ), rollup AS (
    SELECT t.root,
           pg_catalog.count(*) FILTER (WHERE pc.relkind <> 'p') AS leaves,
           pg_catalog.max(t.level) AS depth,
           COALESCE(pg_catalog.sum(pc.relpages::pg_catalog.int8 + COALESCE(tc.relpages, 0) + COALESCE(ix.pages, 0))
                    FILTER (WHERE pc.relkind <> 'p'), 0)::pg_catalog.int8 AS pages,
           COALESCE(pg_catalog.sum(GREATEST(pc.reltuples, 0)) FILTER (WHERE pc.relkind <> 'p'), 0)::pg_catalog.float8 AS rows
      FROM tree t
      JOIN pg_catalog.pg_class pc ON pc.oid = t.relid
      LEFT JOIN pg_catalog.pg_class tc ON tc.oid = pc.reltoastrelid
      LEFT JOIN LATERAL (
           SELECT pg_catalog.sum(ic.relpages)::pg_catalog.int8 AS pages
             FROM pg_catalog.pg_index i
             JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
            WHERE i.indrelid = pc.oid) ix ON true
     GROUP BY t.root
  ), invalid AS (
    SELECT t.root,
           pg_catalog.array_agg(ic.relname::pg_catalog.text ORDER BY ic.relname) AS names,
           pg_catalog.count(*) FILTER (WHERE i.indisunique) AS uniques
      FROM tree t
      JOIN pg_catalog.pg_class pc ON pc.oid = t.relid AND pc.relkind = 'p'
      JOIN pg_catalog.pg_index i ON i.indrelid = t.relid
      JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
     WHERE NOT i.indisvalid
     GROUP BY t.root
  )
SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname),
       n.nspname, c.relname,
       CASE pt.partstrat WHEN 'r' THEN 'range' WHEN 'l' THEN 'list' WHEN 'h' THEN 'hash' ELSE pt.partstrat::pg_catalog.text END,
       pg_catalog.upper(CASE pt.partstrat WHEN 'r' THEN 'range' WHEN 'l' THEN 'list' WHEN 'h' THEN 'hash' ELSE pt.partstrat::pg_catalog.text END)
         || ' (' || (SELECT pg_catalog.string_agg(CASE WHEN k.attnum = 0 THEN 'expression' ELSE pg_catalog.quote_ident(a.attname) END, ', ' ORDER BY k.ord)
                       FROM pg_catalog.unnest(pt.partattrs::pg_catalog.int2[]) WITH ORDINALITY AS k (attnum, ord)
                       LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum) || ')',
       r.leaves, r.depth,
       r.pages * pg_catalog.current_setting('block_size')::pg_catalog.int8,
       r.rows,
       (SELECT pg_catalog.count(*) FROM pg_catalog.pg_inherits i WHERE i.inhparent = c.oid),
       COALESCE(dn.nspname, ''), COALESCE(d.relname, ''),
       COALESCE(GREATEST(d.reltuples, ds.n_live_tup, 0), 0)::pg_catalog.float8,
       COALESCE(inv.names, '{}'), COALESCE(inv.uniques, 0),
       COALESCE(pg_catalog.date_part('epoch', GREATEST(ps.last_analyze, ps.last_autoanalyze))::pg_catalog.int8, 0),
       bounds.covered_until, COALESCE(bounds.covered_forever, false)
  FROM roots
  JOIN pg_catalog.pg_class c ON c.oid = roots.oid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_partitioned_table pt ON pt.partrelid = c.oid
  JOIN rollup r ON r.root = c.oid
  LEFT JOIN invalid inv ON inv.root = c.oid
  LEFT JOIN pg_catalog.pg_class d ON d.oid = pt.partdefid
  LEFT JOIN pg_catalog.pg_namespace dn ON dn.oid = d.relnamespace
  LEFT JOIN pg_catalog.pg_stat_user_tables ds ON ds.relid = pt.partdefid
  LEFT JOIN pg_catalog.pg_stat_user_tables ps ON ps.relid = c.oid
  LEFT JOIN LATERAL (
       -- Only a range key on one date or timestamp column is read back as
       -- a time; the WHERE keeps the cast away from any other key.
       SELECT pg_catalog.max(pg_catalog.substring(pg_catalog.pg_get_expr(pc.relpartbound, 0), 'TO \(''([^'']*)''\)$')::pg_catalog.timestamptz) AS covered_until,
              pg_catalog.bool_or(pg_catalog.pg_get_expr(pc.relpartbound, 0) ~ 'TO \(MAXVALUE\)$') AS covered_forever
         FROM pg_catalog.pg_inherits i
         JOIN pg_catalog.pg_class pc ON pc.oid = i.inhrelid
        WHERE i.inhparent = c.oid
          AND pt.partstrat = 'r' AND pt.partnatts = 1
          AND (SELECT a.atttypid FROM pg_catalog.pg_attribute a WHERE a.attrelid = c.oid AND a.attnum = pt.partattrs[0])
              IN ('pg_catalog.date'::pg_catalog.regtype, 'pg_catalog.timestamp'::pg_catalog.regtype, 'pg_catalog.timestamptz'::pg_catalog.regtype)) bounds ON true
 ORDER BY 8 DESC, 1
 LIMIT $1`

// partitionsCheck reports each database's partitioned tables as whole
// trees: what a leaf-by-leaf view of table statistics cannot show.
type partitionsCheck struct{}

func (c *partitionsCheck) Name() string { return "partitions" }
func (c *partitionsCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *partitionsCheck) DefaultInterval() time.Duration { return 10 * time.Minute }
func (c *partitionsCheck) Timeout() time.Duration         { return 30 * time.Second }

func (c *partitionsCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, partitionsQuery, partitionedTableFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	var leaves, withDefaultRows, invalid int64
	for rows.Next() {
		var key, schema, table, strategy, keyDef, defaultSchema, defaultTable string
		var leafCount, depth, direct, bytes, uniques, analyzed int64
		var treeRows, defaultRows float64
		var invalidNames []string
		var coveredUntil *time.Time
		var coveredForever bool
		if err := rows.Scan(&key, &schema, &table, &strategy, &keyDef, &leafCount, &depth, &bytes, &treeRows,
			&direct, &defaultSchema, &defaultTable, &defaultRows, &invalidNames, &uniques, &analyzed,
			&coveredUntil, &coveredForever); err != nil {
			return Result{}, err
		}
		if len(result.Facts) == partitionedTableFactLimit {
			result.Truncated = true
			break
		}
		leaves += leafCount
		if defaultTable != "" && defaultRows > 0 {
			withDefaultRows++
		}
		invalid += int64(len(invalidNames))
		labels := map[string]string{
			"schemaname": schema, "relname": table, "strategy": strategy, "partition_key": keyDef,
			"partitions": strconv.FormatInt(leafCount, 10), "direct_partitions": strconv.FormatInt(direct, 10),
			"depth": strconv.FormatInt(depth, 10), "bytes": strconv.FormatInt(bytes, 10),
			"rows":              strconv.FormatFloat(treeRows, 'f', 0, 64),
			"default_partition": "", "default_schema": "", "default_relname": "", "default_rows": "",
			"invalid_indexes": strings.Join(invalidNames, ","), "invalid_unique_indexes": strconv.FormatInt(uniques, 10),
			"analyzed_at": "", "covered_until": "",
		}
		if defaultTable != "" {
			labels["default_partition"] = defaultSchema + "." + defaultTable
			labels["default_schema"], labels["default_relname"] = defaultSchema, defaultTable
			labels["default_rows"] = strconv.FormatFloat(defaultRows, 'f', 0, 64)
		}
		if analyzed > 0 {
			labels["analyzed_at"] = time.Unix(analyzed, 0).UTC().Format(time.RFC3339)
		}
		switch {
		case coveredForever:
			labels["covered_until"] = "unbounded"
		case coveredUntil != nil:
			labels["covered_until"] = coveredUntil.UTC().Format(time.RFC3339)
		}
		result.Facts = append(result.Facts, Fact{Kind: partitionedTableFactKind, Key: key, ValueText: strategy, Labels: labels})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	result.Metrics = append(result.Metrics,
		pgtype.Metric{Name: "pg_partitioned_tables", Value: float64(len(result.Facts)), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_partitions", Value: float64(leaves), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_partitioned_tables_default_rows", Value: float64(withDefaultRows), Kind: pgtype.KindGauge},
		pgtype.Metric{Name: "pg_partitioned_indexes_invalid", Value: float64(invalid), Kind: pgtype.KindGauge},
	)
	return result, nil
}
