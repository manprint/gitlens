-- Hourly rollups for long retention (plan 007 phase 5): three continuous
-- aggregates, filled by their refresh policy (refresh_continuous_aggregate
-- cannot run inside the migration transaction), kept 730 days by default.
-- metrics_statements_1h keeps the hourly average of every rate plus the
-- scraped seconds, so rate x sample_seconds still gives the hour's total.
-- metrics_ash_1h drops state, user, client and application.

CREATE MATERIALIZED VIEW IF NOT EXISTS metrics_statements_1h
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT time_bucket(INTERVAL '1 hour', ts) AS bucket,
       tenant_id, cluster_id, instance_id, datname, queryid,
       avg(calls_rate) AS calls_rate,
       avg(exec_time_rate_ms) AS exec_time_rate_ms,
       avg(rows_rate) AS rows_rate,
       avg(shared_blks_hit_rate) AS shared_blks_hit_rate,
       avg(shared_blks_read_rate) AS shared_blks_read_rate,
       avg(wal_bytes_rate) AS wal_bytes_rate,
       avg(temp_blks_written_rate) AS temp_blks_written_rate,
       sum(sample_seconds) AS sample_seconds,
       count(*) AS samples
FROM metrics_statements
GROUP BY bucket, tenant_id, cluster_id, instance_id, datname, queryid
WITH NO DATA;

CREATE MATERIALIZED VIEW IF NOT EXISTS metrics_ash_1h
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT time_bucket(INTERVAL '1 hour', ts) AS bucket,
       tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, queryid,
       sum(samples) AS samples,
       sum(window_ticks) AS window_ticks,
       sum(window_seconds) AS window_seconds
FROM metrics_ash
GROUP BY bucket, tenant_id, cluster_id, instance_id, datname, wait_event_type, wait_event, queryid
WITH NO DATA;

CREATE MATERIALIZED VIEW IF NOT EXISTS metrics_1h
WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS
SELECT time_bucket(INTERVAL '1 hour', ts) AS bucket,
       tenant_id, cluster_id, instance_id, datname, metric, labels, series_id,
       avg(value) AS value_avg,
       max(value) AS value_max,
       min(value) AS value_min,
       count(*) AS samples
FROM metrics
GROUP BY bucket, tenant_id, cluster_id, instance_id, datname, metric, labels, series_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_statements_1h',
    start_offset => INTERVAL '3 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes', if_not_exists => true);
SELECT add_continuous_aggregate_policy('metrics_ash_1h',
    start_offset => INTERVAL '3 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes', if_not_exists => true);
SELECT add_continuous_aggregate_policy('metrics_1h',
    start_offset => INTERVAL '3 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes', if_not_exists => true);

SELECT add_retention_policy('metrics_statements_1h', INTERVAL '730 days', if_not_exists => true);
SELECT add_retention_policy('metrics_ash_1h', INTERVAL '730 days', if_not_exists => true);
SELECT add_retention_policy('metrics_1h', INTERVAL '730 days', if_not_exists => true);
