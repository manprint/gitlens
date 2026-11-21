-- index_check commands (plan 007 phase 10): the commands CHECK gains the kind,
-- and each validation of a suggested index keeps its latest result per key.
DO $$
DECLARE c text;
BEGIN
  FOR c IN SELECT conname FROM pg_constraint
            WHERE conrelid = 'commands'::regclass AND contype = 'c'
              AND pg_get_constraintdef(oid) LIKE '%pgstattuple%' LOOP
    EXECUTE format('ALTER TABLE commands DROP CONSTRAINT %I', c);
  END LOOP;
END $$;
ALTER TABLE commands ADD CONSTRAINT commands_kind_check
  CHECK (kind IN ('explain','cancel','terminate','pgstattuple','index_check'));

CREATE TABLE IF NOT EXISTS index_validations (
  validation_id    bigserial   PRIMARY KEY,
  tenant_id        text        NOT NULL DEFAULT 'default',
  instance_id      uuid        NOT NULL,
  cluster_id       bigint      NOT NULL,
  datname          text        NOT NULL,
  queryid          bigint      NOT NULL,
  schemaname       text        NOT NULL,
  relname          text        NOT NULL,
  columns          text[]      NOT NULL,
  include          text[]      NOT NULL DEFAULT '{}',
  index_used       boolean     NOT NULL,
  cost_before      double precision NOT NULL,
  cost_after       double precision NOT NULL,
  index_size_bytes bigint,
  plan_after       jsonb,
  validated_at     timestamptz NOT NULL,
  command_id       uuid        NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS index_validations_key_idx
  ON index_validations (tenant_id, instance_id, datname, queryid, schemaname, relname, columns, include);
