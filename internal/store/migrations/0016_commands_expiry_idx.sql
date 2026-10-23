-- The expiry sweep runs on every claim and every GET. It filters on tenant,
-- state and expires_at, and commands_queue_idx covers only pending rows, so
-- each sweep read the whole commands table. This partial index matches the
-- sweep's predicate; it stays small because finished commands leave it.
CREATE INDEX IF NOT EXISTS commands_expiry_idx
  ON commands (tenant_id, expires_at)
  WHERE state IN ('pending', 'claimed');
