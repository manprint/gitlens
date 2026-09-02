ALTER TABLE instances ADD COLUMN target_name text;

CREATE INDEX IF NOT EXISTS instances_target_name_idx
  ON instances (tenant_id, cluster_id, target_name);
