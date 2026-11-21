-- Plan 007 phase 1: multixact ID wraparound risk, the twin of
-- txn.wraparound_risk on pg_max_datminmxid_age (emitted by the activity
-- check). ON CONFLICT DO NOTHING keeps an operator's edit.
INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary) VALUES
  ('txn.multixact_wraparound_risk', 'critical', 'instance', 'pg_max_datminmxid_age', 'gt', 1e9, 300, 'Multixact ID age above one billion')
ON CONFLICT DO NOTHING;
