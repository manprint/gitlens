-- Alert rules. Tier 0 rules are built into the code and are NOT stored here
-- (plan 002 D12); this table holds only Tier 1 rules, seeded below and
-- editable through the API.
CREATE TABLE IF NOT EXISTS alert_rules (
  rule_id      text        NOT NULL,
  tenant_id    text        NOT NULL DEFAULT 'default',
  enabled      boolean     NOT NULL DEFAULT true,
  severity     text        NOT NULL CHECK (severity IN ('critical','warning','info')),
  scope        text        NOT NULL CHECK (scope IN ('instance','cluster','database')),
  metric       text,
  comparator   text        CHECK (comparator IN ('gt','ge','lt','le','eq','ne')),
  threshold    double precision,
  for_seconds  integer     NOT NULL DEFAULT 0,
  event_type   text,
  summary      text        NOT NULL,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, rule_id)
);

-- One row per distinct alert instance. alert_key is derived by the code
-- (see sub-phase 0.5) and is stable for as long as the condition holds.
CREATE TABLE IF NOT EXISTS alerts (
  alert_key    text        NOT NULL,
  tenant_id    text        NOT NULL DEFAULT 'default',
  rule_id      text        NOT NULL,
  severity     text        NOT NULL CHECK (severity IN ('critical','warning','info')),
  state        text        NOT NULL CHECK (state IN ('pending','firing','resolved')),
  cluster_id   bigint,
  instance_id  uuid,
  datname      text,
  labels       jsonb       NOT NULL DEFAULT '{}'::jsonb,
  value        double precision,
  summary      text        NOT NULL,
  started_at   timestamptz NOT NULL,
  last_eval_at timestamptz NOT NULL,
  resolved_at  timestamptz,
  PRIMARY KEY (tenant_id, alert_key, started_at)
);
CREATE INDEX IF NOT EXISTS alerts_state_idx
  ON alerts (tenant_id, state, severity, last_eval_at DESC);
CREATE INDEX IF NOT EXISTS alerts_instance_idx
  ON alerts (tenant_id, instance_id, last_eval_at DESC);

-- Silences suppress notification, never evaluation: a silenced alert still
-- appears in the API with suppressed = true.
CREATE TABLE IF NOT EXISTS silences (
  silence_id  uuid        PRIMARY KEY,
  tenant_id   text        NOT NULL DEFAULT 'default',
  matchers    jsonb       NOT NULL,
  reason      text        NOT NULL,
  created_by  text        NOT NULL DEFAULT 'api',
  starts_at   timestamptz NOT NULL,
  ends_at     timestamptz NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS silences_window_idx
  ON silences (tenant_id, ends_at DESC);

-- Delivery log. The unique index is what makes invariant I-4 true: one
-- delivery per (alert_key, started_at, channel), whatever replica tried.
CREATE TABLE IF NOT EXISTS notifications (
  notification_id bigserial PRIMARY KEY,
  tenant_id       text        NOT NULL DEFAULT 'default',
  alert_key       text        NOT NULL,
  started_at      timestamptz NOT NULL,
  channel         text        NOT NULL,
  phase           text        NOT NULL CHECK (phase IN ('fire','resolve')),
  sent_at         timestamptz NOT NULL DEFAULT now(),
  ok              boolean     NOT NULL,
  attempts        integer     NOT NULL DEFAULT 1,
  error           text
);
CREATE UNIQUE INDEX IF NOT EXISTS notifications_once_idx
  ON notifications (tenant_id, alert_key, started_at, channel, phase);

-- Tier 1 seed rules. ON CONFLICT DO NOTHING keeps the migration idempotent
-- and preserves any edit an operator already made through the API.
INSERT INTO alert_rules (rule_id, severity, scope, metric, comparator, threshold, for_seconds, summary) VALUES
  ('replica.lag_high',             'warning',  'instance', 'pg_replication_lag_seconds',  'gt', 30,   120, 'Standby replay lag above 30 seconds'),
  ('replica.all_standbys_lagging', 'critical', 'cluster',  'pg_replication_lag_seconds',  'gt', 30,   120, 'Every standby in the cluster is lagging'),
  ('replica.no_sync_standby',      'critical', 'cluster',  'pg_sync_standby_count',       'lt', 1,     60, 'No synchronous standby available'),
  ('conn.near_max',                'warning',  'instance', 'pg_connections_used_ratio',   'gt', 0.8,  120, 'Connections above 80 percent of max_connections'),
  ('conn.idle_in_transaction',     'warning',  'instance', 'pg_max_idle_in_txn_seconds',  'gt', 300,  120, 'A session has been idle in transaction for over 5 minutes'),
  ('txn.long_running',             'warning',  'instance', 'pg_max_xact_age_seconds',      'gt', 900,  120, 'A transaction has been open for over 15 minutes'),
  ('txn.wraparound_risk',          'critical', 'instance', 'pg_max_datfrozenxid_age',      'gt', 1e9,  300, 'Transaction ID age above one billion'),
  ('db.deadlock_rate',             'warning',  'instance', 'pg_deadlocks_total',           'gt', 0.1,  300, 'Deadlocks are occurring'),
  ('archive.failing',              'critical', 'instance', 'pg_archiver_failed_ratio',     'gt', 0,    300, 'WAL archiving is failing'),
  ('disk.free_low',                'critical', 'instance', 'host_disk_free_ratio',         'lt', 0.15, 300, 'Less than 15 percent free space on the data filesystem')
ON CONFLICT DO NOTHING;
