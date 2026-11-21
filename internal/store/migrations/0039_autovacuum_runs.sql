-- Autovacuum and autoanalyze runs read from the server log (plan 007 phase 8),
-- one row per run; a number the log line did not carry stays NULL.
CREATE TABLE IF NOT EXISTS autovacuum_runs (
  ts          timestamptz NOT NULL,
  tenant_id   text        NOT NULL DEFAULT 'default',
  instance_id uuid        NOT NULL,
  cluster_id  bigint      NOT NULL,
  datname     text        NOT NULL,
  schemaname  text        NOT NULL,
  relname     text        NOT NULL,
  kind        text        NOT NULL CHECK (kind IN ('vacuum','analyze')),
  aggressive  boolean     NOT NULL DEFAULT false,
  wraparound  boolean     NOT NULL DEFAULT false,
  index_scans bigint, pages_removed bigint, pages_remain bigint, pages_scanned bigint,
  tuples_removed bigint, tuples_remain bigint, tuples_dead_not_removable bigint,
  buffer_hits bigint, buffer_reads bigint, buffer_dirtied bigint,
  wal_records bigint, wal_fpi bigint, wal_bytes bigint,
  cpu_user_seconds double precision, cpu_system_seconds double precision,
  elapsed_seconds double precision
);
SELECT create_hypertable('autovacuum_runs', by_range('ts', INTERVAL '7 days'), if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS autovacuum_runs_dedup_idx
  ON autovacuum_runs (instance_id, datname, schemaname, relname, kind, ts);
CREATE INDEX IF NOT EXISTS autovacuum_runs_lookup_idx
  ON autovacuum_runs (tenant_id, instance_id, datname, schemaname, relname, ts DESC);
SELECT add_retention_policy('autovacuum_runs', INTERVAL '90 days', if_not_exists => TRUE);
