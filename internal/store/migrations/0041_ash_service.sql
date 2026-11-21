-- ASH application dimensions (plan 007 phase 11): service and endpoint read by
-- the agent from sqlcommenter tags. '' means untagged or turned off; rows
-- written before this migration keep ''. Compression segmentby is unchanged.
ALTER TABLE metrics_ash
  ADD COLUMN IF NOT EXISTS service  text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS endpoint text NOT NULL DEFAULT '';
DROP INDEX IF EXISTS metrics_ash_dedup_idx;
CREATE UNIQUE INDEX metrics_ash_dedup_idx ON metrics_ash
  (instance_id, datname, wait_event_type, wait_event, state, COALESCE(queryid, 0),
   usename, client_addr, application_name, service, endpoint, ts);
