-- pglens monitoring role for PostgreSQL 15-18.
-- Run as a superuser, or as rds_superuser on Amazon RDS.
--
-- Defaults to Tier 0. Apply a higher tier by defining the corresponding
-- psql flag, for example:
--   psql -v pglens_password='secret' -v dbname=app -v tier1=1 -f monitoring_user.sql
-- Tier 2 also needs tier1=1. This file never creates an extension.
\set ON_ERROR_STOP on

\if :{?pglens_password}
\else
\echo 'pglens_password is required (use psql -v pglens_password=...)' >&2
\quit 3
\endif
\if :{?dbname}
\else
\set dbname postgres
\endif

-- Create or reconcile the role so rerunning the script is safe.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pglens') THEN
    CREATE ROLE pglens LOGIN;
  END IF;
END
$$;
ALTER ROLE pglens LOGIN PASSWORD :'pglens_password';

-- ========================================================================
-- Tier 0 — read-only, no query execution, no extensions. The default.
-- ========================================================================
GRANT pg_monitor TO pglens;
GRANT CONNECT ON DATABASE :"dbname" TO pglens;

-- Required for a stable cluster_id. These control functions are restricted
-- to superusers by default and need explicit EXECUTE grants.
GRANT EXECUTE ON FUNCTION pg_control_system()     TO pglens;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO pglens;

-- ========================================================================
-- Tier 1 — query text and EXPLAIN without ANALYZE.
-- ========================================================================
-- pg_read_all_stats is already implied by pg_monitor; it is listed for
-- clarity. pg_read_all_data is the actual table-read privilege required by
-- PostgreSQL for EXPLAIN on relations and is the agent's T1 capability gate.
\if :{?tier1}
GRANT pg_read_all_stats TO pglens;
GRANT pg_read_all_data TO pglens;
\endif

-- ========================================================================
-- Tier 2 — cancel and terminate, only with allow_signal enabled.
-- ========================================================================
\if :{?tier2}
GRANT pg_signal_backend TO pglens;
\endif

-- ========================================================================
-- Tier 3 — extensions, only when the DBA deliberately enables them.
-- ========================================================================
-- pglens NEVER creates an extension itself. Run these statements manually,
-- on each chosen database, because automatic extension creation would break
-- the plan 001 constraint that no monitored instance needs one.
-- CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
-- CREATE EXTENSION IF NOT EXISTS pgstattuple;

-- Bound the blast radius of the monitoring session itself.
ALTER ROLE pglens SET statement_timeout = '15s';
ALTER ROLE pglens SET lock_timeout = '1s';
ALTER ROLE pglens SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE pglens SET application_name = 'pglens';
