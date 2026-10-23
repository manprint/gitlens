-- A replica that takes the staleness lock reads the latest agent_up or
-- agent_down of every instance. The partial index holds only those rows, in
-- the order that read needs, so the read does not scan the whole of events.
CREATE INDEX IF NOT EXISTS events_agent_state_idx ON events (instance_id, ts DESC)
  WHERE type IN ('agent_up', 'agent_down');
