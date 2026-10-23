package check

import (
	"context"
	"strconv"
	"time"

	"github.com/manprint/pglens/internal/pgtype"
)

func init() { Register(&fkIndexesCheck{}) }

// fkUnindexedFactKind is the fact kind of one foreign key whose referencing
// columns no index leads with. The server replaces a database's facts of the
// kind on every complete result, so a foreign key that gained an index, or
// was dropped, stops being reported.
const fkUnindexedFactKind = "fk_unindexed"

// fkUnindexedFactLimit bounds the facts of one database, largest
// referencing tables first.
const fkUnindexedFactLimit = 200

// fkUnindexedQuery lists the foreign keys no index serves. Deleting or
// updating a referenced row makes PostgreSQL look for referencing rows; with
// no index leading with the referencing columns, that is a scan of the whole
// referencing table, under a lock, for every such row. An index serves when
// it is valid, not partial, and its first key columns are the foreign key's
// columns in any order (btree), or its one column is (hash). A foreign key a
// partition inherits from its partitioned table is left out: the partitioned
// table's own constraint is checked against its partitioned indexes, which are
// valid only once every partition has the index. The referencing table's size
// is the sum of its leaf partitions. Everything is read from the catalogs and
// pg_relation_size, which any role may call.
const fkUnindexedQuery = `SELECT pg_catalog.quote_ident(n.nspname) || '.' || pg_catalog.quote_ident(c.relname) || '.' || pg_catalog.quote_ident(con.conname),
       n.nspname, c.relname, con.conname,
       pn.nspname || '.' || pc.relname,
       (SELECT pg_catalog.string_agg(a.attname, ', ' ORDER BY k.ord)
          FROM pg_catalog.unnest(con.conkey) WITH ORDINALITY AS k (attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum),
       (SELECT pg_catalog.string_agg(pg_catalog.quote_ident(a.attname), ', ' ORDER BY k.ord)
          FROM pg_catalog.unnest(con.conkey) WITH ORDINALITY AS k (attnum, ord)
          JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum),
       CASE WHEN c.relkind = 'p'
            THEN COALESCE((SELECT pg_catalog.sum(pg_catalog.pg_relation_size(p.relid))
                             FROM pg_catalog.pg_partition_tree(c.oid) p WHERE p.isleaf), 0)
            ELSE pg_catalog.pg_relation_size(c.oid) END::pg_catalog.int8 AS bytes
  FROM pg_catalog.pg_constraint con
  JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_class pc ON pc.oid = con.confrelid
  JOIN pg_catalog.pg_namespace pn ON pn.oid = pc.relnamespace
 WHERE con.contype = 'f'
   AND con.conparentid = 0
   AND n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname !~ '^pg_(toast|temp_|toast_temp_)'
   AND NOT EXISTS (
       SELECT 1
         FROM pg_catalog.pg_index i
         JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
         JOIN pg_catalog.pg_am am ON am.oid = ic.relam
        WHERE i.indrelid = con.conrelid
          AND i.indisvalid
          AND i.indpred IS NULL
          AND am.amname IN ('btree', 'hash')
          AND pg_catalog.cardinality(con.conkey) <= i.indnkeyatts
          AND (pg_catalog.string_to_array(i.indkey::pg_catalog.text, ' ')::pg_catalog.int2[])[1:pg_catalog.cardinality(con.conkey)] @> con.conkey
          AND (pg_catalog.string_to_array(i.indkey::pg_catalog.text, ' ')::pg_catalog.int2[])[1:pg_catalog.cardinality(con.conkey)] <@ con.conkey)
 ORDER BY bytes DESC, 1
 LIMIT $1`

// fkIndexesCheck reports the foreign keys of each database that no index
// serves, with the size of the referencing table.
type fkIndexesCheck struct{}

func (c *fkIndexesCheck) Name() string { return "fk_indexes" }
func (c *fkIndexesCheck) Requires() Requirements {
	return Requirements{Scope: ScopeDatabase, PermTier: pgtype.TierReadOnly}
}
func (c *fkIndexesCheck) DefaultInterval() time.Duration { return 15 * time.Minute }
func (c *fkIndexesCheck) Timeout() time.Duration         { return 15 * time.Second }

func (c *fkIndexesCheck) Scrape(ctx context.Context, t Target) (Result, error) {
	conn, err := t.ConnFor(ctx, t.Database())
	if err != nil {
		return Result{}, err
	}
	defer conn.Release()
	if err := ApplySessionLimits(ctx, conn, c.Timeout()); err != nil {
		return Result{}, err
	}
	rows, err := conn.Query(ctx, fkUnindexedQuery, fkUnindexedFactLimit+1)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	var result Result
	for rows.Next() {
		var key, schema, table, constraint, referenced, columns, columnsSQL string
		var bytes int64
		if err := rows.Scan(&key, &schema, &table, &constraint, &referenced, &columns, &columnsSQL, &bytes); err != nil {
			return Result{}, err
		}
		if len(result.Facts) == fkUnindexedFactLimit {
			result.Truncated = true
			break
		}
		result.Facts = append(result.Facts, Fact{
			Kind: fkUnindexedFactKind, Key: key, ValueText: columns,
			Labels: map[string]string{
				"schemaname": schema, "relname": table, "constraint": constraint,
				"referenced": referenced, "columns_sql": columnsSQL,
				"table_bytes": strconv.FormatInt(bytes, 10),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	// The count is a lower bound when the list was truncated.
	result.Metrics = append(result.Metrics, pgtype.Metric{
		Name: "pg_fk_unindexed", Value: float64(len(result.Facts)), Kind: pgtype.KindGauge,
	})
	return result, nil
}
