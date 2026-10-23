-- Heap-only estimate: the main fork is compared with the pages the analysed
-- live tuples would need. pg_total_relation_size() also counted indexes and
-- TOAST, which no expected size includes, so every indexed table looked
-- bloated.
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
)
SELECT schemaname, relname, '' AS indexrelname, 'table' AS object_kind, real_bytes,
       expected_bytes,
       GREATEST(real_bytes - expected_bytes, 0)::float8 AS bloat_bytes,
       COALESCE(GREATEST(real_bytes - expected_bytes, 0) / NULLIF(real_bytes, 0), 0)::float8 AS bloat_ratio,
       reltuples::float8, oid
  FROM estimates
