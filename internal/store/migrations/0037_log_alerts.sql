-- Plan 007 phase 6: alert rules on the PostgreSQL log counters the agent's
-- log reader (checks.logs) emits every scrape. Any line of these classes
-- fires at once. ON CONFLICT DO NOTHING keeps an operator's edit.
INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary) VALUES
  ('log.panic',         'critical', 'instance', 'pg_log_panic_events',      'gt', 0, 0, 'PANIC or server crash in the PostgreSQL log'),
  ('log.corruption',    'critical', 'instance', 'pg_log_corruption_events', 'gt', 0, 0, 'Data corruption reported in the PostgreSQL log'),
  ('log.disk_full',     'critical', 'instance', 'pg_log_disk_full_events',  'gt', 0, 0, 'Disk full reported in the PostgreSQL log'),
  ('log.out_of_memory', 'warning',  'instance', 'pg_log_oom_events',        'gt', 0, 0, 'Out of memory reported in the PostgreSQL log')
ON CONFLICT DO NOTHING;
