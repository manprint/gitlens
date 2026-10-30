-- An advisor rule can be disabled from the console (11.4, D18). A rule without
-- a row is enabled; enabling a rule again deletes its row, so a row means the
-- rule is overridden. The advisor engine reads the table only through the
-- server's overrides source: it skips a disabled rule and resolves its findings.
CREATE TABLE IF NOT EXISTS advisor_rule_overrides (
  tenant_id text NOT NULL DEFAULT 'default',
  rule_id text NOT NULL,
  enabled boolean NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, rule_id)
);
