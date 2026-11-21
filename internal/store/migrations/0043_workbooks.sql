-- Tuning workbooks (plan 007 phase 13): per query, named variants of an
-- explain (planner settings, pg_hint_plan hints, parameter values) and the
-- plan of each variant's latest run. A variant's plan is an experiment and is
-- never written to query_plans.
CREATE TABLE IF NOT EXISTS workbooks (
  workbook_id uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   text        NOT NULL DEFAULT 'default',
  instance_id uuid        NOT NULL,
  datname     text        NOT NULL,
  queryid     bigint      NOT NULL,
  name        text        NOT NULL,
  notes       text        NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS workbooks_lookup_idx ON workbooks (tenant_id, instance_id, queryid);

CREATE TABLE IF NOT EXISTS workbook_variants (
  variant_id   uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  workbook_id  uuid        NOT NULL REFERENCES workbooks ON DELETE CASCADE,
  position     integer     NOT NULL,
  name         text        NOT NULL,
  settings     jsonb       NOT NULL DEFAULT '{}',
  hints        text        NOT NULL DEFAULT '',
  params       jsonb,
  "analyze"    boolean     NOT NULL DEFAULT false,
  status       text        NOT NULL DEFAULT 'never' CHECK (status IN ('never','pending','done','failed')),
  command_id   uuid,
  error        text        NOT NULL DEFAULT '',
  plan         jsonb,
  plan_hash    text,
  total_cost   double precision,
  planning_ms  double precision,
  execution_ms double precision,
  ran_at       timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS workbook_variants_command_idx ON workbook_variants (command_id) WHERE command_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS workbook_variants_workbook_idx ON workbook_variants (workbook_id, position);
