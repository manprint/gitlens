-- Settings the console changes at run time, one row per key (11.4, D22c). A key
-- without a row has its default. The advisor observation windows are the only
-- keys: window.unused_index and window.backup_history, each a jsonb integer
-- number of seconds. The server validates keys and values; the table does not.
CREATE TABLE IF NOT EXISTS server_settings (
  tenant_id text NOT NULL DEFAULT 'default',
  key text NOT NULL,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, key)
);
