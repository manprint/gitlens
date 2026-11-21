-- Automatic plans from auto_explain (plan 007 phase 7). query_plans keeps one
-- row per plan shape and now records who captured it first; plan_samples keeps
-- one row per logged execution. trace_id is filled from phase 11, parameters
-- from unit 7.3.
ALTER TABLE query_plans ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'command';

CREATE TABLE IF NOT EXISTS plan_samples (
  ts          timestamptz NOT NULL,
  tenant_id   text        NOT NULL DEFAULT 'default',
  instance_id uuid        NOT NULL,
  cluster_id  bigint      NOT NULL,
  datname     text        NOT NULL DEFAULT '',
  queryid     bigint      NOT NULL,
  plan_hash   text        NOT NULL,
  duration_ms double precision NOT NULL,
  analyzed    boolean     NOT NULL DEFAULT false,
  parameters  text,
  trace_id    text
);
SELECT create_hypertable('plan_samples', by_range('ts', INTERVAL '1 day'), if_not_exists => TRUE);
CREATE UNIQUE INDEX IF NOT EXISTS plan_samples_dedup_idx
  ON plan_samples (instance_id, datname, queryid, plan_hash, ts);
CREATE INDEX IF NOT EXISTS plan_samples_lookup_idx
  ON plan_samples (tenant_id, instance_id, queryid, ts DESC);
SELECT add_retention_policy('plan_samples', INTERVAL '30 days', if_not_exists => TRUE);
