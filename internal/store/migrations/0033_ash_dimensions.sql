-- ASH text dimensions (plan 007 phase 2): user, client address, application.
-- '' means unknown or turned off by the agent; rows written before this
-- migration keep ''. The compression segmentby setting is unchanged.
ALTER TABLE metrics_ash
  ADD COLUMN IF NOT EXISTS usename          text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS client_addr      text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS application_name text NOT NULL DEFAULT '';
DROP INDEX IF EXISTS metrics_ash_dedup_idx;
CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
  (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0),
   usename, client_addr, application_name, ts);
