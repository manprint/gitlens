-- Data completeness after an agent start (13.2, D22d).
--
-- The agent reports, on every instance of every push, when its process
-- started and the interval of every check it schedules for the target. Both
-- are optional on the wire: an agent that predates them sends neither, and
-- the columns stay NULL.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS agent_started_at timestamptz;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS check_intervals jsonb;

-- The newest result of each check of an instance in each database, written
-- with the samples of the envelope that carried it. A skipped check has no
-- result. An instance-scoped check has one row, with datname ''; a database
-- scoped check reports once per database and has a row per database, so a
-- later success in one database does not hide a failure in another. Only
-- whether that result failed is kept, not its error text. first_result_at is
-- the first result of the current agent run: a result whose agent started
-- after the previous result starts it again. The coverage endpoint merges the
-- rows of a check.
CREATE TABLE IF NOT EXISTS check_results (
  instance_id     uuid NOT NULL REFERENCES instances ON DELETE CASCADE,
  check_name      text NOT NULL,
  datname         text NOT NULL DEFAULT '',
  first_result_at timestamptz NOT NULL,
  last_result_at  timestamptz NOT NULL,
  last_failed     boolean NOT NULL,
  PRIMARY KEY (instance_id, check_name, datname)
);
