-- pglens monitoring role. PostgreSQL 15-18.
-- Run as a superuser, or as rds_superuser on Amazon RDS.
\set ON_ERROR_STOP on

CREATE ROLE pglens LOGIN PASSWORD :'pw';
GRANT pg_monitor TO pglens;

-- Required for a stable cluster_id. NOT covered by pg_monitor: these
-- functions are restricted to superusers by default and EXECUTE must be
-- granted explicitly.
GRANT EXECUTE ON FUNCTION pg_control_system()     TO pglens;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens;

-- Optional tier T1: enables EXPLAIN (plan only).
-- GRANT pg_read_all_data TO pglens;

-- Optional tier T2: enables cancel/terminate from the UI.
-- GRANT pg_signal_backend TO pglens;

-- Bound the blast radius of the monitoring session itself.
ALTER ROLE pglens SET statement_timeout = '15s';
ALTER ROLE pglens SET lock_timeout = '1s';
ALTER ROLE pglens SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE pglens SET application_name = 'pglens';
