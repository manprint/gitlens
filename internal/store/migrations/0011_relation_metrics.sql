ALTER TABLE metrics_tables
  ADD COLUMN IF NOT EXISTS dead_tuple_ratio double precision,
  ADD COLUMN IF NOT EXISTS last_vacuum_age_seconds double precision;
