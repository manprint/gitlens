CREATE TABLE IF NOT EXISTS lock_snapshots (
  tenant_id text NOT NULL DEFAULT 'default',
  instance_id uuid NOT NULL,
  cluster_id bigint NOT NULL,
  ts timestamptz NOT NULL,
  tree jsonb NOT NULL,
  PRIMARY KEY (tenant_id, instance_id)
);
