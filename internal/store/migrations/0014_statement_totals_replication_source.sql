-- metrics_statements held per-second rates only, and the statements API
-- summed them and called the result a total: off by the scrape interval
-- (60x with the default one). sample_seconds is the interval each rate
-- covers, so rate * sample_seconds is what the interval actually observed.
-- temp_blks_written_rate carries a counter the agent collected nowhere.
ALTER TABLE metrics_statements
  ADD COLUMN IF NOT EXISTS sample_seconds double precision,
  ADD COLUMN IF NOT EXISTS temp_blks_written_rate double precision;

-- metrics_replication mixes three kinds of rows: a standby's view of its own
-- receiver, a primary's view of each of its standbys (pg_stat_replication)
-- and one row per replication slot. Nothing told them apart, so readers
-- treated a slot as a standby and a primary's row as nobody's. source is
-- 'receiver', 'sender' or 'slot'; NULL on rows written before this migration.
-- No CHECK constraint: the table is a compressed hypertable.
ALTER TABLE metrics_replication
  ADD COLUMN IF NOT EXISTS source text,
  ADD COLUMN IF NOT EXISTS slot_type text,
  ADD COLUMN IF NOT EXISTS slot_safe_wal_size_bytes bigint;
