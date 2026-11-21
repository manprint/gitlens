-- PostgreSQL log events read by the agent (plan 007 phase 6). One row per
-- shipped log line; the dedup index makes a pushed-twice result one row.
-- trace_id is filled from phase 11.
CREATE TABLE IF NOT EXISTS log_events (
  ts               timestamptz NOT NULL,
  tenant_id        text        NOT NULL DEFAULT 'default',
  cluster_id       bigint      NOT NULL,
  instance_id      uuid        NOT NULL,
  datname          text        NOT NULL DEFAULT '',
  usename          text        NOT NULL DEFAULT '',
  application_name text        NOT NULL DEFAULT '',
  pid              integer,
  session_id       text        NOT NULL DEFAULT '',
  line_num         bigint      NOT NULL DEFAULT 0,
  severity         text        NOT NULL,
  sqlstate         text        NOT NULL DEFAULT '',
  class            text        NOT NULL,
  queryid          bigint,
  message          text        NOT NULL,
  extra            jsonb,
  trace_id         text
);
SELECT create_hypertable('log_events', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS log_events_dedup_idx ON log_events (instance_id, session_id, line_num, ts);
CREATE INDEX IF NOT EXISTS log_events_class_idx ON log_events (tenant_id, instance_id, class, ts DESC);
SELECT add_retention_policy('log_events', INTERVAL '14 days', if_not_exists => TRUE);
