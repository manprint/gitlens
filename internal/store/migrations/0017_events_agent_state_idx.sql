-- A replica that takes the staleness lock reads the latest agent_up or
-- agent_down of every instance. The partial index holds only those rows, in
-- the order that read needs, so the read does not scan the whole of events.
--
-- The index is built without CONCURRENTLY, inside the migration's transaction,
-- so writes to events wait at startup for the build (reads do not). The build
-- scans events, so the wait grows with the table. Schedule the first start that
-- applies this migration for a quiet period.
CREATE INDEX IF NOT EXISTS events_agent_state_idx ON events (instance_id, ts DESC)
  WHERE type IN ('agent_up', 'agent_down');
