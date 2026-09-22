-- command_audit recorded results only. A command that was requested and
-- never claimed (the agent was down, or the command expired in the queue)
-- left no trace at all, and requested_by was always the column default
-- 'api'. The audit now also records the request, with who made it, and the
-- expiry. The table stays append-only.
ALTER TABLE command_audit DROP CONSTRAINT IF EXISTS command_audit_outcome_check;
ALTER TABLE command_audit ADD CONSTRAINT command_audit_outcome_check
  CHECK (outcome IN ('requested', 'expired', 'ok', 'error', 'rejected'));

-- events had no retention at all. The staleness evaluator now purges rows
-- older than its retention window in batches; this index serves that scan.
CREATE INDEX IF NOT EXISTS events_ts_idx ON events (ts);
