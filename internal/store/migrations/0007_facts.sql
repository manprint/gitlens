CREATE TABLE metrics_tables (
  ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default',
  cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL,
  schemaname text NOT NULL, relname text NOT NULL,
  seq_scan double precision, seq_tup_read double precision,
  idx_scan double precision, idx_tup_fetch double precision,
  n_tup_ins double precision, n_tup_upd double precision, n_tup_del double precision,
  n_tup_hot_upd double precision, n_live_tup bigint, n_dead_tup bigint,
  n_mod_since_analyze bigint, last_vacuum timestamptz, last_autovacuum timestamptz,
  last_analyze timestamptz, last_autoanalyze timestamptz,
  autovacuum_count double precision, autoanalyze_count double precision,
  relpages bigint, reltuples double precision, relfrozenxid_age bigint,
  total_bytes bigint, table_bytes bigint, toast_bytes bigint
);
SELECT create_hypertable('metrics_tables', by_range('ts', INTERVAL '6 hours'));
CREATE UNIQUE INDEX metrics_tables_dedup_idx ON metrics_tables (tenant_id, instance_id, datname, schemaname, relname, ts);
CREATE INDEX metrics_tables_lookup_idx ON metrics_tables (instance_id, datname, ts DESC);

CREATE TABLE metrics_indexes (
  ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default',
  cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL,
  schemaname text NOT NULL, relname text NOT NULL, indexrelname text NOT NULL,
  idx_scan double precision, idx_tup_read double precision, idx_tup_fetch double precision,
  idx_blks_read double precision, idx_blks_hit double precision, index_bytes bigint,
  is_unique boolean, is_primary boolean, is_valid boolean, def_hash text
);
SELECT create_hypertable('metrics_indexes', by_range('ts', INTERVAL '6 hours'));
CREATE UNIQUE INDEX metrics_indexes_dedup_idx ON metrics_indexes (tenant_id, instance_id, datname, schemaname, indexrelname, ts);
CREATE INDEX metrics_indexes_lookup_idx ON metrics_indexes (instance_id, datname, ts DESC);

CREATE TABLE metrics_bloat (
  ts timestamptz NOT NULL, tenant_id text NOT NULL DEFAULT 'default',
  cluster_id bigint NOT NULL, instance_id uuid NOT NULL, datname text NOT NULL,
  schemaname text NOT NULL, relname text NOT NULL, indexrelname text NOT NULL DEFAULT '',
  object_kind text NOT NULL CHECK (object_kind IN ('table','index')),
  method text NOT NULL CHECK (method IN ('estimate','pgstattuple')),
  real_bytes bigint, expected_bytes bigint, bloat_bytes bigint, bloat_ratio double precision
);
SELECT create_hypertable('metrics_bloat', by_range('ts', INTERVAL '1 day'));
CREATE UNIQUE INDEX metrics_bloat_dedup_idx ON metrics_bloat (tenant_id, instance_id, datname, schemaname, relname, indexrelname, method, ts);

CREATE TABLE object_facts (
  tenant_id text NOT NULL DEFAULT 'default', cluster_id bigint NOT NULL,
  instance_id uuid NOT NULL, datname text NOT NULL DEFAULT '', kind text NOT NULL,
  key text NOT NULL, labels jsonb NOT NULL DEFAULT '{}'::jsonb,
  value_text text, value_json jsonb, first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL, changed_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, instance_id, datname, kind, key)
);
CREATE INDEX object_facts_kind_idx ON object_facts (tenant_id, kind, instance_id);

ALTER TABLE metrics_tables SET (timescaledb.compress, timescaledb.compress_segmentby = 'instance_id, datname, schemaname, relname', timescaledb.compress_orderby = 'ts DESC');
SELECT add_compression_policy('metrics_tables', INTERVAL '48 hours');
ALTER TABLE metrics_indexes SET (timescaledb.compress, timescaledb.compress_segmentby = 'instance_id, datname, schemaname, indexrelname', timescaledb.compress_orderby = 'ts DESC');
SELECT add_compression_policy('metrics_indexes', INTERVAL '48 hours');
ALTER TABLE metrics_bloat SET (timescaledb.compress, timescaledb.compress_segmentby = 'instance_id, datname, relname', timescaledb.compress_orderby = 'ts DESC');
SELECT add_compression_policy('metrics_bloat', INTERVAL '7 days');
SELECT add_retention_policy('metrics_tables', INTERVAL '30 days');
SELECT add_retention_policy('metrics_indexes', INTERVAL '30 days');
SELECT add_retention_policy('metrics_bloat', INTERVAL '90 days');
