-- Repair Tier 1 seed rules whose metric name never matched a metric the
-- collector emits, which made them silently unevaluable: alert.metricSource
-- queries `metrics` by exact name, so a rule naming a series that is never
-- written produces no samples, never fires, and never appears anywhere as
-- broken.
--
-- conn.idle_in_transaction named pg_max_idle_in_txn_seconds; the metric
-- emitted by internal/check/conn_stats.go is pg_max_idle_in_transaction_seconds.
--
-- Scoped to the exact wrong value so an operator's own edit through the API is
-- preserved, and idempotent on re-run.
UPDATE alert_rules
   SET metric = 'pg_max_idle_in_transaction_seconds',
       updated_at = now()
 WHERE rule_id = 'conn.idle_in_transaction'
   AND metric = 'pg_max_idle_in_txn_seconds';
