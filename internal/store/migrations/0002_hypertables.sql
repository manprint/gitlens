-- Generic: any metric a check emits without a dedicated table.
CREATE TABLE metrics (
  ts          timestamptz      NOT NULL,
  tenant_id   text             NOT NULL DEFAULT 'default',
  cluster_id  bigint           NOT NULL,
  instance_id uuid             NOT NULL,
  datname     text             NOT NULL DEFAULT '',  -- '' = instance scope
  metric      text             NOT NULL,
  labels      jsonb            NOT NULL DEFAULT '{}'::jsonb,
  series_id   bigint           NOT NULL,  -- FNV-1a 64 of the canonical SeriesKey
  value       double precision NOT NULL
);
SELECT create_hypertable('metrics', by_range('ts', INTERVAL '6 hours'));

-- Invariant I-3 enforced by the storage engine rather than by hope:
-- the same series can never be written twice for the same instant.
CREATE UNIQUE INDEX metrics_dedup_idx ON metrics (series_id, ts);
CREATE INDEX metrics_lookup_idx      ON metrics (instance_id, metric, ts DESC);

CREATE TABLE metrics_statements (
  ts          timestamptz NOT NULL,
  tenant_id   text        NOT NULL DEFAULT 'default',
  cluster_id  bigint      NOT NULL,
  instance_id uuid        NOT NULL,
  datname     text        NOT NULL,
  queryid     bigint      NOT NULL,   -- signed; may legitimately be negative
  calls_rate             double precision,
  exec_time_rate_ms      double precision,
  rows_rate              double precision,
  shared_blks_hit_rate   double precision,
  shared_blks_read_rate  double precision,
  wal_bytes_rate         double precision
);
SELECT create_hypertable('metrics_statements', by_range('ts', INTERVAL '6 hours'));
CREATE UNIQUE INDEX metrics_statements_dedup_idx
  ON metrics_statements (instance_id, datname, queryid, ts);

CREATE TABLE metrics_ash (
  ts              timestamptz NOT NULL,
  tenant_id       text        NOT NULL DEFAULT 'default',
  cluster_id      bigint      NOT NULL,
  instance_id     uuid        NOT NULL,
  datname         text        NOT NULL DEFAULT '',
  wait_event_type text        NOT NULL,   -- 'CPU' when the backend was not waiting
  wait_event      text        NOT NULL,
  state           text        NOT NULL,
  queryid         bigint,                 -- NULL when compute_query_id is off
  samples         int         NOT NULL,
  window_seconds  int         NOT NULL
);
SELECT create_hypertable('metrics_ash', by_range('ts', INTERVAL '6 hours'));
CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
  (instance_id, datname, wait_event_type, wait_event, state,
   COALESCE(queryid, 0), ts);

CREATE TABLE metrics_replication (
  ts          timestamptz NOT NULL,
  tenant_id   text        NOT NULL DEFAULT 'default',
  cluster_id  bigint      NOT NULL,
  instance_id uuid        NOT NULL,
  upstream_id uuid,
  slot_name   text        NOT NULL DEFAULT '',
  edge_type   text        NOT NULL DEFAULT 'streaming',
  sync_state  text,
  write_lag_bytes  bigint, flush_lag_bytes bigint, replay_lag_bytes bigint,
  write_lag_sec    double precision,
  flush_lag_sec    double precision,
  replay_lag_sec   double precision,
  slot_active         bool,
  slot_wal_status     text,
  slot_retained_bytes bigint
);
SELECT create_hypertable('metrics_replication', by_range('ts', INTERVAL '6 hours'));
CREATE UNIQUE INDEX metrics_replication_dedup_idx ON metrics_replication
  (instance_id, COALESCE(upstream_id, '00000000-0000-0000-0000-000000000000'::uuid),
   slot_name, ts);
