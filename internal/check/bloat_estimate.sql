-- Table estimate: the main fork is compared with the pages the analysed live
-- tuples would need. pg_total_relation_size() also counted indexes and TOAST,
-- which no expected size includes, so every indexed table looked bloated.
--
-- Index estimate (the UNION ALL at the end): a btree index's real size is its
-- own main fork, and its expected size follows the table's live tuple count.
-- Only btree indexes whose every column has visible statistics are estimated.
--
-- pg_stats only shows columns the monitoring role may SELECT, and the
-- inherited=true rows of a parent duplicate its own. A table whose columns
-- are not all visible gets no estimate at all instead of a tuple width of
-- zero, which read as ~100% bloat.
WITH widths AS (
  SELECT schemaname, tablename,
         sum(avg_width * (1 - null_frac))::float8 AS tuple_width,
         bool_or(null_frac > 0) AS has_nulls,
         count(*)::int AS stat_columns
    FROM pg_stats
   WHERE NOT inherited
   GROUP BY schemaname, tablename
), relations AS (
  -- real_bytes is the size the last VACUUM or ANALYZE recorded. The exact size
  -- locks the table, so the check reads it for the reported tables only.
  SELECT n.nspname AS schemaname, c.relname, c.oid, c.reltuples,
         (GREATEST(c.relpages, 0)::float8 * current_setting('block_size')::float8) AS real_bytes,
         current_setting('block_size')::float8 AS block_size,
         COALESCE((SELECT substring(o FROM '^fillfactor=([0-9]+)$')::float8
                     FROM unnest(c.reloptions) o WHERE o LIKE 'fillfactor=%'), 100) AS fillfactor,
         w.tuple_width, w.has_nulls, w.stat_columns,
         (SELECT count(*) FROM pg_attribute a
           WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped)::int AS columns
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN widths w ON w.schemaname = n.nspname AND w.tablename = c.relname
   WHERE c.relkind = 'r' AND c.relpersistence <> 't'
     AND n.nspname NOT IN ('pg_catalog', 'information_schema')
), tuples AS (
  -- A heap tuple is its 23-byte header plus null bitmap and its data, each
  -- padded to MAXALIGN, plus a 4-byte line pointer; a page loses 24 bytes to
  -- its header and keeps (100 - fillfactor)% free by design.
  SELECT *,
         GREATEST(floor((block_size - 24) * fillfactor / 100 /
           (ceil((23 + CASE WHEN has_nulls THEN ceil(columns / 8.0) ELSE 0 END) / 8.0) * 8
            + ceil(tuple_width / 8.0) * 8 + 4)), 1) AS tuples_per_page
    FROM relations
   WHERE stat_columns >= columns
), estimates AS (
  SELECT schemaname, relname, oid, reltuples, real_bytes,
         (ceil(GREATEST(reltuples, 0) / tuples_per_page) * block_size)::float8 AS expected_bytes
    FROM tuples
), index_widths AS (
  -- Every column of the index, INCLUDE columns too, must have a pg_stats row:
  -- an expression column is attribute 0 and has none, and a column the role
  -- cannot read is hidden. The widths are the same statistics the heap
  -- estimate uses, read per column of the index.
  SELECT ix.indexrelid, ix.indrelid, ix.indnatts,
         count(*)::int AS key_columns,
         count(s.attname)::int AS stat_columns,
         bool_and(k.attnum > 0) AS plain_columns,
         COALESCE(sum(s.avg_width), 0)::float8 AS key_width,
         COALESCE(bool_or(s.null_frac > 0), false) AS has_nulls
    FROM pg_index ix
    CROSS JOIN LATERAL unnest(ix.indkey::int2[]) AS k(attnum)
    JOIN pg_class t ON t.oid = ix.indrelid
    JOIN pg_namespace tn ON tn.oid = t.relnamespace
    LEFT JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = k.attnum
    LEFT JOIN pg_stats s ON s.schemaname = tn.nspname AND s.tablename = t.relname
                        AND s.attname = a.attname AND NOT s.inherited
   GROUP BY ix.indexrelid, ix.indrelid, ix.indnatts
), index_relations AS (
  -- Only btree indexes are estimated: the formulas below are btree's page
  -- layout. A partial index holds fewer rows than its table, and an expression
  -- index has no column statistics, so neither gets an estimate. Deduplication
  -- makes a btree smaller than this estimate when keys repeat, so the estimate
  -- can understate bloat but never invent it.
  SELECT n.nspname AS schemaname, t.relname AS tablename, ic.relname AS indexname, ic.oid,
         t.reltuples,
         (GREATEST(ic.relpages, 0)::float8 * current_setting('block_size')::float8) AS real_bytes,
         current_setting('block_size')::float8 AS block_size,
         COALESCE((SELECT substring(o FROM '^fillfactor=([0-9]+)$')::float8
                     FROM unnest(ic.reloptions) o WHERE o LIKE 'fillfactor=%'), 90) AS fillfactor,
         w.key_width, w.has_nulls, w.indnatts
    FROM pg_index ix
    JOIN pg_class ic ON ic.oid = ix.indexrelid
    JOIN pg_am am ON am.oid = ic.relam AND am.amname = 'btree'
    JOIN pg_class t ON t.oid = ix.indrelid AND t.relkind = 'r' AND t.relpersistence <> 't'
    JOIN pg_namespace n ON n.oid = t.relnamespace
    JOIN index_widths w ON w.indexrelid = ix.indexrelid
   WHERE ix.indisvalid AND ix.indisready AND ix.indpred IS NULL AND ix.indexprs IS NULL
     AND w.plain_columns AND w.stat_columns = w.key_columns
     AND n.nspname NOT IN ('pg_catalog', 'information_schema')
), index_tuples AS (
  -- A btree leaf page keeps 24 bytes of page header and 16 of btree special
  -- space (40 in all), and fillfactor percent of the rest (90 by default). An
  -- index tuple is its 8-byte header, the null bitmap when a key may be null,
  -- the key data padded to MAXALIGN, and a 4-byte line pointer.
  SELECT *,
         GREATEST(floor((block_size - 40) * fillfactor / 100 /
           (ceil((8 + CASE WHEN has_nulls THEN ceil(indnatts / 8.0) ELSE 0 END + key_width) / 8.0) * 8 + 4)), 1) AS tuples_per_page
    FROM index_relations
), index_estimates AS (
  -- The index holds one entry per live heap row, so the leaf pages follow the
  -- table's reltuples, not the index's own (which only VACUUM refreshes). The
  -- inner pages are about one per tuples_per_page leaves, and the metapage is one.
  SELECT schemaname, tablename, indexname, oid, reltuples, real_bytes,
         ((ceil(GREATEST(reltuples, 0) / tuples_per_page)
           + ceil(ceil(GREATEST(reltuples, 0) / tuples_per_page) / tuples_per_page) + 1) * block_size)::float8 AS expected_bytes
    FROM index_tuples
)
SELECT schemaname, relname, '' AS indexrelname, 'table' AS object_kind, real_bytes,
       expected_bytes,
       GREATEST(real_bytes - expected_bytes, 0)::float8 AS bloat_bytes,
       COALESCE(GREATEST(real_bytes - expected_bytes, 0) / NULLIF(real_bytes, 0), 0)::float8 AS bloat_ratio,
       reltuples::float8, oid
  FROM estimates
UNION ALL
SELECT schemaname, tablename, indexname, 'index', real_bytes,
       expected_bytes,
       GREATEST(real_bytes - expected_bytes, 0)::float8,
       COALESCE(GREATEST(real_bytes - expected_bytes, 0) / NULLIF(real_bytes, 0), 0)::float8,
       reltuples::float8, oid
  FROM index_estimates
