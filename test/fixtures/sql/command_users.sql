-- Extra command-channel role used only by the primary/standby E2E topology.
-- The agent fixture assigns this role to the permissive target while the
-- standby keeps using the base pglens T0 role.
\set ON_ERROR_STOP on

CREATE ROLE pglens_t2 LOGIN PASSWORD 'pglens-monitoring-test';
GRANT pg_monitor TO pglens_t2;
GRANT pg_read_all_data TO pglens_t2;
GRANT pg_signal_backend TO pglens_t2;
GRANT EXECUTE ON FUNCTION pg_control_system()     TO pglens_t2;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens_t2;
ALTER ROLE pglens_t2 SET statement_timeout = '15s';
ALTER ROLE pglens_t2 SET lock_timeout = '1s';
ALTER ROLE pglens_t2 SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE pglens_t2 SET application_name = 'pglens';

CREATE EXTENSION IF NOT EXISTS pgstattuple;
