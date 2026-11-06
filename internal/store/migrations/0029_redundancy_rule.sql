-- GAP-5 (plan 005, 7.5.4): a cluster streaming to fewer standbys than it has
-- members. pg_missing_standby_count is derived by the alert engine
-- (internal/alert/source_sql.go), not emitted by a check. ON CONFLICT DO
-- NOTHING keeps an operator's edit.
INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary) VALUES
  ('replica.redundancy_reduced', 'warning', 'cluster', 'pg_missing_standby_count', 'gt', 0, 60, 'The cluster streams to fewer standbys than it has members')
ON CONFLICT DO NOTHING;
