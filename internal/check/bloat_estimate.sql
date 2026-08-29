WITH widths AS (
  SELECT schemaname, tablename, COALESCE(sum(avg_width * (1 - null_frac)), 0)::float8 AS tuple_width,
         count(*) FILTER (WHERE null_frac > 0)::int AS nullable_columns
  FROM pg_stats GROUP BY schemaname, tablename
), relations AS (
  SELECT n.nspname AS schemaname, c.relname, c.reltuples, c.relpages, c.oid,
         pg_total_relation_size(c.oid)::float8 AS real_bytes, COALESCE(w.tuple_width, 0)::float8 AS tuple_width,
         COALESCE(w.nullable_columns, 0)::int AS nullable_columns
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  LEFT JOIN widths w ON w.schemaname=n.nspname AND w.tablename=c.relname
  WHERE c.relkind='r' AND n.nspname NOT IN ('pg_catalog','information_schema')
)
SELECT schemaname, relname, '' AS indexrelname, 'table' AS object_kind, real_bytes,
       GREATEST(reltuples * (tuple_width + 23), 0) AS expected_bytes,
       GREATEST(real_bytes - GREATEST(reltuples * (tuple_width + 23), 0), 0) AS bloat_bytes,
       GREATEST(real_bytes - GREATEST(reltuples * (tuple_width + 23), 0), 0) / NULLIF(real_bytes,0) AS bloat_ratio,
       reltuples FROM relations
