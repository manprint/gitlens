CREATE TABLE IF NOT EXISTS commands (
  command_id   uuid        PRIMARY KEY,
  tenant_id    text        NOT NULL DEFAULT 'default',
  agent_id     text        NOT NULL,
  instance_id  uuid        NOT NULL,
  cluster_id   bigint      NOT NULL,
  kind         text        NOT NULL CHECK (kind IN ('explain','cancel','terminate','pgstattuple')),
  args         jsonb       NOT NULL DEFAULT '{}'::jsonb,
  state        text        NOT NULL CHECK (state IN ('pending','claimed','done','failed','expired')),
  requested_by text        NOT NULL DEFAULT 'api',
  claim_token  uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  claimed_at   timestamptz,
  finished_at  timestamptz,
  expires_at   timestamptz NOT NULL,
  result       jsonb,
  error        text
);
CREATE INDEX IF NOT EXISTS commands_queue_idx
  ON commands (tenant_id, agent_id, state, created_at)
  WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS commands_instance_idx
  ON commands (tenant_id, instance_id, created_at DESC);

-- Append-only. Nothing in the codebase updates or deletes from this table.
CREATE TABLE IF NOT EXISTS command_audit (
  audit_id     bigserial   PRIMARY KEY,
  tenant_id    text        NOT NULL DEFAULT 'default',
  command_id   uuid        NOT NULL,
  instance_id  uuid        NOT NULL,
  kind         text        NOT NULL,
  args         jsonb       NOT NULL DEFAULT '{}'::jsonb,
  requested_by text        NOT NULL,
  executed_at  timestamptz NOT NULL DEFAULT now(),
  outcome      text        NOT NULL CHECK (outcome IN ('ok','error','rejected')),
  detail       text
);
CREATE INDEX IF NOT EXISTS command_audit_instance_idx
  ON command_audit (tenant_id, instance_id, executed_at DESC);

CREATE TABLE IF NOT EXISTS query_plans (
  plan_id     bigserial   PRIMARY KEY,
  tenant_id   text        NOT NULL DEFAULT 'default',
  instance_id uuid        NOT NULL,
  cluster_id  bigint      NOT NULL,
  datname     text        NOT NULL,
  queryid     bigint,
  plan_hash   text        NOT NULL,
  analyzed    boolean     NOT NULL DEFAULT false,
  captured_at timestamptz NOT NULL,
  plan        jsonb       NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS query_plans_dedup_idx
  ON query_plans (tenant_id, instance_id, datname, queryid, plan_hash, analyzed);
CREATE INDEX IF NOT EXISTS query_plans_lookup_idx
  ON query_plans (tenant_id, instance_id, queryid, captured_at DESC);
