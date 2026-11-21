-- CPU, memory and disk I/O of each PostgreSQL process over a host interval
-- (plan 007 phase 12), joined to its session; one row per process and sample.
CREATE TABLE IF NOT EXISTS metrics_processes (
  ts                timestamptz NOT NULL,
  tenant_id         text        NOT NULL DEFAULT 'default',
  instance_id       uuid        NOT NULL,
  cluster_id        bigint      NOT NULL,
  pid               integer     NOT NULL,
  backend_type      text        NOT NULL DEFAULT '',
  datname           text        NOT NULL DEFAULT '',
  usename           text        NOT NULL DEFAULT '',
  application_name  text        NOT NULL DEFAULT '',
  state             text        NOT NULL DEFAULT '',
  queryid           bigint,
  cpu_ratio         double precision NOT NULL,
  rss_bytes         bigint      NOT NULL,
  read_bytes_per_s  double precision,
  write_bytes_per_s double precision
);
SELECT create_hypertable('metrics_processes', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS metrics_processes_dedup_idx ON metrics_processes (instance_id, pid, ts);
SELECT add_retention_policy('metrics_processes', INTERVAL '7 days', if_not_exists => TRUE);
