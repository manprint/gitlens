CREATE TABLE IF NOT EXISTS findings (
  finding_id text NOT NULL,
  tenant_id text NOT NULL DEFAULT 'default',
  rule_id text NOT NULL,
  severity text NOT NULL CHECK (severity IN ('critical', 'warning', 'info')),
  state text NOT NULL CHECK (state IN ('open', 'degraded', 'muted', 'resolved')),
  scope text NOT NULL CHECK (scope IN ('instance', 'cluster', 'database', 'relation')),
  cluster_id bigint,
  instance_id uuid,
  datname text NOT NULL DEFAULT '',
  object_name text NOT NULL DEFAULT '',
  title text NOT NULL,
  detail text NOT NULL,
  remediation text NOT NULL DEFAULT '',
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  degraded_reason text,
  first_seen timestamptz NOT NULL,
  last_seen timestamptz NOT NULL,
  resolved_at timestamptz,
  muted_until timestamptz,
  mute_reason text,
  PRIMARY KEY (tenant_id, finding_id)
);

CREATE INDEX IF NOT EXISTS findings_lookup_idx
  ON findings (tenant_id, state, severity, last_seen DESC);
CREATE INDEX IF NOT EXISTS findings_instance_idx
  ON findings (tenant_id, instance_id, datname, last_seen DESC);
CREATE INDEX IF NOT EXISTS findings_rule_idx
  ON findings (tenant_id, rule_id, state);
