-- Ordering that must hold: agent buffer max_age (6h)
--                        < server max_sample_age (12h)
--                        < compress_after (48h)
-- so replayed samples always land in an uncompressed chunk. Inserting into
-- a compressed chunk is possible but slow, and it is avoidable by design.
ALTER TABLE metrics_statements SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'instance_id, datname, queryid',
  timescaledb.compress_orderby   = 'ts DESC'
);
SELECT add_compression_policy('metrics_statements', INTERVAL '48 hours');

ALTER TABLE metrics_ash SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'instance_id, wait_event_type, wait_event',
  timescaledb.compress_orderby   = 'ts DESC'
);
SELECT add_compression_policy('metrics_ash', INTERVAL '48 hours');

ALTER TABLE metrics_replication SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'instance_id, slot_name',
  timescaledb.compress_orderby   = 'ts DESC'
);
SELECT add_compression_policy('metrics_replication', INTERVAL '48 hours');

ALTER TABLE metrics SET (
  timescaledb.compress,
  timescaledb.compress_segmentby = 'instance_id, metric',
  timescaledb.compress_orderby   = 'ts DESC'
);
SELECT add_compression_policy('metrics', INTERVAL '48 hours');

SELECT add_retention_policy('metrics',             INTERVAL '30 days');
SELECT add_retention_policy('metrics_statements',  INTERVAL '30 days');
SELECT add_retention_policy('metrics_ash',         INTERVAL '30 days');
SELECT add_retention_policy('metrics_replication', INTERVAL '30 days');
