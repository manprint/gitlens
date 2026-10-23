-- pglens monitoring role for PostgreSQL 15-19.
-- Run as a superuser, or as rds_superuser on Amazon RDS.
--
-- Defaults to Tier 0. Apply a higher tier by setting its psql flag to 1, for
-- example:
--   psql -v pglens_password='secret' -v dbname=app -v tier1=1 -f monitoring_user.sql
-- Tier 2 also needs tier1=1. This file never creates an extension.
-- The role is named pglens unless -v role=<name> names another one.
-- Large-object creation and WAL messages (pg_logical_emit_message) are revoked
-- from PUBLIC in every run. -v harden_public=1 also revokes the other grants
-- that let every role write outside a read-only transaction (see the end of
-- this file).
--
-- The role it configures can only read. Before granting anything the script
-- checks that an existing role of that name could not write either: it
-- refuses a superuser, a role with CREATEDB, CREATEROLE, REPLICATION or
-- BYPASSRLS, a member of any role other than the read-only predefined ones,
-- a role that owns objects, and a role holding a write privilege in the
-- database psql is connected to. It then names what to revoke and changes
-- nothing, since the whole script runs in one transaction. The pglens
-- server's own storage role is a superuser named pglens in the shipped
-- compose files, so running the script against the storage instance by
-- mistake stops here.
--
-- Privileges on objects are recorded per database. Run the script connected
-- to each database where privileges were ever granted to the role; it warns
-- about the databases it cannot inspect from the current one.
\set ON_ERROR_STOP on

-- An empty password is refused like a missing one: ALTER ROLE would clear the
-- password and leave a LOGIN role without one. The comparisons before BEGIN
-- are qualified to pg_catalog, see below.
\if :{?pglens_password}
SELECT (:'pglens_password' OPERATOR(pg_catalog.<>) '') AS password_set \gset
\else
\set password_set false
\endif
-- The quit meta-command takes no exit status, so the refusal is an error that
-- ON_ERROR_STOP turns into exit 3, before anything below runs.
\if :password_set
\else
DO $$ BEGIN RAISE EXCEPTION 'pglens_password is required and must not be empty (use psql -v pglens_password=...)'; END $$;
\endif
\if :{?dbname}
\else
\set dbname postgres
\endif
\if :{?role}
\else
\set role pglens
\endif

-- A tier is granted by the value 1 only. Testing the variable for existence,
-- as \if :{?tier1} does, would also grant it for tier1=0 or tier1=false.
-- Tier 2 is tier 1 plus signal, so tier2=1 without tier1=1 is refused before
-- anything changes.
-- The comparisons above and in this part run before BEGIN, so before the
-- script pins its search_path. Their operator is qualified to pg_catalog: a
-- schema of the database owner listed first in that database's search_path
-- could otherwise supply its own = and run it as the superuser, with the
-- password or the flag as an argument.
\if :{?tier1}
SELECT (:'tier1' OPERATOR(pg_catalog.=) '1') AS tier1_on \gset
\else
\set tier1_on false
\endif
\if :{?tier2}
SELECT (:'tier2' OPERATOR(pg_catalog.=) '1') AS tier2_on \gset
\else
\set tier2_on false
\endif
\if :tier2_on
\if :tier1_on
\else
DO $$ BEGIN RAISE EXCEPTION 'tier2=1 needs tier1=1: tier 2 is tier 1 plus signal'; END $$;
\endif
\endif

BEGIN;

-- The script runs as a superuser, in a database another role may own.
-- Every function and operator below resolves in pg_catalog, whatever
-- search_path that database or the caller sets: a schema of the owner's
-- listed first would otherwise run its own current_setting() or format()
-- with the caller's privileges.
SET LOCAL search_path = pg_catalog, pg_temp;

-- psql variables do not reach into the DO blocks below; this setting does.
SET LOCAL pglens.role = :'role';

-- Create the role, or reconcile an existing one so rerunning the script is
-- safe.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = current_setting('pglens.role')) THEN
    EXECUTE format('CREATE ROLE %I LOGIN', current_setting('pglens.role'));
  END IF;
END
$$;

-- Refuse a role that could write. Each problem comes with the statement
-- that removes it; nothing is changed while any remains.
DO $$
DECLARE
  monitor oid := (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = current_setting('pglens.role'));
  who text := quote_ident(current_setting('pglens.role'));
  problems text[] := '{}';
  item record;
BEGIN
  FOR item IN
    SELECT attribute
    FROM pg_catalog.pg_roles r,
         LATERAL (VALUES ('SUPERUSER', r.rolsuper), ('CREATEDB', r.rolcreatedb),
                         ('CREATEROLE', r.rolcreaterole), ('REPLICATION', r.rolreplication),
                         ('BYPASSRLS', r.rolbypassrls)) AS a(attribute, granted)
    WHERE r.oid = monitor AND a.granted
  LOOP
    problems := problems || format('%1$s has the %2$s attribute: ALTER ROLE %1$s NO%2$s;',
                                   who, item.attribute);
  END LOOP;

  -- Membership in any role but these lends that role's privileges.
  -- pg_has_role(..., 'MEMBER') follows indirect memberships too, and holds
  -- for every role when the role is a superuser, which is reported above.
  FOR item IN
    SELECT g.rolname,
           EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                   WHERE m.roleid = g.oid AND m.member = monitor) AS direct
    FROM pg_catalog.pg_roles g
    WHERE g.oid <> monitor
      AND NOT (SELECT rolsuper FROM pg_catalog.pg_roles WHERE oid = monitor)
      -- Owning the database makes the role a member; the ownership check
      -- below reports it.
      AND g.rolname <> 'pg_database_owner'
      AND g.rolname NOT IN ('pg_monitor', 'pg_read_all_settings', 'pg_read_all_stats',
                            'pg_stat_scan_tables', 'pg_read_all_data', 'pg_signal_backend')
      AND pg_catalog.pg_has_role(monitor, g.oid, 'MEMBER')
    ORDER BY g.rolname
  LOOP
    problems := problems || CASE
      WHEN item.direct THEN format('%1$s is a member of %2$I: REVOKE %2$I FROM %1$s;',
                                   who, item.rolname)
      ELSE format('%s is a member of %I through another role: revoke the membership that leads to it',
                  who, item.rolname)
    END;
  END LOOP;

  -- Ownership is recorded for objects in every database, so this check sees
  -- them all. An object of a database is counted under that database. A
  -- shared object (a database or a tablespace) belongs to no database, so it
  -- is named on its own.
  FOR item IN
    SELECT d.datname, count(*) AS owned
    FROM pg_catalog.pg_shdepend s
    JOIN pg_catalog.pg_database d ON d.oid = s.dbid
    WHERE s.refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass
      AND s.refobjid = monitor AND s.deptype = 'o'
    GROUP BY 1
    ORDER BY 1
  LOOP
    problems := problems || format('%1$s owns %2$s object(s) in %3$s: REASSIGN OWNED BY %1$s TO <owner>; (connected to that database)',
                                   who, item.owned, item.datname);
  END LOOP;
  FOR item IN
    SELECT pg_catalog.pg_describe_object(s.classid, s.objid, s.objsubid) AS object
    FROM pg_catalog.pg_shdepend s
    WHERE s.refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass
      AND s.refobjid = monitor AND s.deptype = 'o' AND s.dbid = 0
    ORDER BY 1
  LOOP
    problems := problems || format('%1$s owns %2$s, a shared object: REASSIGN OWNED BY %1$s TO <owner>;',
                                   who, item.object);
  END LOOP;

  -- Privileges granted to the role itself. Only reads are allowed, plus
  -- CONNECT, USAGE on schemas, and EXECUTE on the two control functions
  -- granted below. What the role holds as an owner is reported above.
  FOR item IN
    SELECT format('%s ON DATABASE %I', a.privilege_type, d.datname) AS what
    FROM pg_catalog.pg_database d, pg_catalog.aclexplode(d.datacl) a
    WHERE a.grantee = monitor AND a.privilege_type <> 'CONNECT' AND d.datdba <> monitor
    UNION ALL
    SELECT format('%s ON TABLESPACE %I', a.privilege_type, t.spcname)
    FROM pg_catalog.pg_tablespace t, pg_catalog.aclexplode(t.spcacl) a
    WHERE a.grantee = monitor AND t.spcowner <> monitor
    UNION ALL
    SELECT format('%s ON SCHEMA %I', a.privilege_type, n.nspname)
    FROM pg_catalog.pg_namespace n, pg_catalog.aclexplode(n.nspacl) a
    WHERE a.grantee = monitor AND a.privilege_type <> 'USAGE' AND n.nspowner <> monitor
    UNION ALL
    SELECT format('%s ON %s', a.privilege_type, c.oid::pg_catalog.regclass)
    FROM pg_catalog.pg_class c, pg_catalog.aclexplode(c.relacl) a
    WHERE a.grantee = monitor AND a.privilege_type <> 'SELECT' AND c.relowner <> monitor
    UNION ALL
    SELECT format('%s (%I) ON %s', a.privilege_type, att.attname, att.attrelid::pg_catalog.regclass)
    FROM pg_catalog.pg_attribute att
    JOIN pg_catalog.pg_class c ON c.oid = att.attrelid,
         pg_catalog.aclexplode(att.attacl) a
    WHERE a.grantee = monitor AND a.privilege_type <> 'SELECT' AND c.relowner <> monitor
    UNION ALL
    SELECT format('%s ON FUNCTION %s', a.privilege_type, p.oid::pg_catalog.regprocedure)
    FROM pg_catalog.pg_proc p, pg_catalog.aclexplode(p.proacl) a
    WHERE a.grantee = monitor AND p.proowner <> monitor
      AND p.oid NOT IN ('pg_catalog.pg_control_system()'::pg_catalog.regprocedure,
                        'pg_catalog.pg_control_checkpoint()'::pg_catalog.regprocedure)
    UNION ALL
    SELECT format('%s ON LARGE OBJECT %s', a.privilege_type, l.oid)
    FROM pg_catalog.pg_largeobject_metadata l, pg_catalog.aclexplode(l.lomacl) a
    WHERE a.grantee = monitor AND a.privilege_type <> 'SELECT' AND l.lomowner <> monitor
    UNION ALL
    SELECT format('%s ON PARAMETER %s', a.privilege_type, p.parname)
    FROM pg_catalog.pg_parameter_acl p, pg_catalog.aclexplode(p.paracl) a
    WHERE a.grantee = monitor
    ORDER BY 1
  LOOP
    problems := problems || format('%1$s holds %2$s: REVOKE %2$s FROM %1$s;', who, item.what);
  END LOOP;

  -- Default privileges hand the role the privilege on every future object.
  FOR item IN
    SELECT o.rolname AS creator, a.privilege_type,
           CASE WHEN d.defaclnamespace <> 0
                THEN format(' IN SCHEMA %s', d.defaclnamespace::pg_catalog.regnamespace) ELSE '' END AS scope,
           CASE d.defaclobjtype WHEN 'r' THEN 'TABLES' WHEN 'S' THEN 'SEQUENCES'
                WHEN 'f' THEN 'FUNCTIONS' WHEN 'T' THEN 'TYPES' WHEN 'n' THEN 'SCHEMAS'
                WHEN 'L' THEN 'LARGE OBJECTS' ELSE d.defaclobjtype::text END AS objects
    FROM pg_catalog.pg_default_acl d
    JOIN pg_catalog.pg_roles o ON o.oid = d.defaclrole,
         pg_catalog.aclexplode(d.defaclacl) a
    WHERE a.grantee = monitor
      AND (d.defaclobjtype, a.privilege_type) NOT IN (('r', 'SELECT'), ('S', 'SELECT'),
                                                      ('T', 'USAGE'), ('n', 'USAGE'), ('L', 'SELECT'))
    ORDER BY 1, 4, 2
  LOOP
    problems := problems || format('%1$s gets %3$s on new %5$s by default: ALTER DEFAULT PRIVILEGES FOR ROLE %2$I%4$s REVOKE %3$s ON %5$s FROM %1$s;',
                                   who, item.creator, item.privilege_type, item.scope, item.objects);
  END LOOP;

  IF cardinality(problems) > 0 THEN
    RAISE EXCEPTION 'refusing to configure %: the role could write to the database', who
      USING DETAIL = array_to_string(problems, E'\n'),
            HINT = 'Run the statements above as an administrator, then run this script again.';
  END IF;

  -- Privileges in the other databases are invisible from this one.
  FOR item IN
    SELECT d.datname, count(*) AS granted
    FROM pg_catalog.pg_shdepend s
    JOIN pg_catalog.pg_database d ON d.oid = s.dbid
    WHERE s.refclassid = 'pg_catalog.pg_authid'::pg_catalog.regclass
      AND s.refobjid = monitor AND s.deptype = 'a'
      AND d.datname <> current_database()
    GROUP BY 1
    ORDER BY 1
  LOOP
    RAISE WARNING '% holds privileges on % object(s) in database %, which this run cannot check',
      who, item.granted, item.datname
      USING HINT = format('Run this script connected to %I as well.', item.datname);
  END LOOP;

  -- The role can connect to these as well, and PUBLIC may still let it create
  -- large objects or emit WAL messages there: the grants revoked above are per
  -- database.
  FOR item IN
    SELECT d.datname
    FROM pg_catalog.pg_database d
    WHERE d.datallowconn AND d.datname <> current_database()
      AND has_database_privilege(monitor, d.datname, 'CONNECT')
    ORDER BY 1
  LOOP
    RAISE WARNING '% can connect to %, where PUBLIC may still let it create large objects or emit WAL messages',
      who, item.datname
      USING HINT = format('Run this script connected to %I as well.', item.datname);
  END LOOP;

  -- A database-specific setting for the role would override the role-wide
  -- read-only default set below.
  FOR item IN
    SELECT d.datname
    FROM pg_catalog.pg_db_role_setting s
    JOIN pg_catalog.pg_database d ON d.oid = s.setdatabase
    WHERE s.setrole = monitor
      AND EXISTS (SELECT 1 FROM unnest(s.setconfig) c
                  WHERE c LIKE 'default\_transaction\_read\_only=%')
  LOOP
    EXECUTE format('ALTER ROLE %s IN DATABASE %I RESET default_transaction_read_only', who, item.datname);
    RAISE NOTICE 'reset the default_transaction_read_only setting of % in database %', who, item.datname;
  END LOOP;
END
$$;

ALTER ROLE :"role" LOGIN PASSWORD :'pglens_password';

-- ========================================================================
-- Tier 0 — read-only, no query execution, no extensions. The default.
-- ========================================================================
GRANT pg_monitor TO :"role";
GRANT CONNECT ON DATABASE :"dbname" TO :"role";

-- Required for a stable cluster_id. PostgreSQL lets every role execute these
-- control functions by default, but hardened installations revoke that from
-- PUBLIC, so grant them explicitly.
GRANT EXECUTE ON FUNCTION pg_control_system()     TO :"role";
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO :"role";

-- ========================================================================
-- Tier 1 — query text and EXPLAIN without ANALYZE.
-- ========================================================================
-- pg_read_all_stats is already implied by pg_monitor; it is listed for
-- clarity. pg_read_all_data is the actual table-read privilege required by
-- PostgreSQL for EXPLAIN on relations and is the agent's T1 capability gate.
\if :tier1_on
GRANT pg_read_all_stats TO :"role";
GRANT pg_read_all_data TO :"role";
\endif

-- ========================================================================
-- Tier 2 — cancel and terminate, only with allow_signal enabled.
-- ========================================================================
\if :tier2_on
GRANT pg_signal_backend TO :"role";
\endif

-- ========================================================================
-- Tier 3 — extensions, only when the DBA deliberately enables them.
-- ========================================================================
-- pglens NEVER creates an extension itself. Run these statements manually,
-- because automatic extension creation would break the plan 001 constraint
-- that no monitored instance needs one.
-- pg_stat_statements (also in shared_preload_libraries): in the database the
-- agent's DSN names or in any monitored database; its statistics cover every
-- database, and the agent reads them all through one view. It looks again
-- every 30 seconds, so no agent restart is needed after creating it.
-- CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
-- pgstattuple: in each database whose tables should be measurable.
-- CREATE EXTENSION IF NOT EXISTS pgstattuple;

-- pglens only ever reads. The role default makes every session read-only,
-- which catches a write from a client that did not ask for a read-only
-- transaction. It is a guard against mistakes, not a boundary: the role can
-- turn the setting off, so a write is stopped by privileges. The script grants
-- none that allow one, and revokes from PUBLIC the grants that let any role
-- create large objects or emit WAL messages. The other PUBLIC grants that allow
-- a write are reported below. The agent also sets this on each connection it
-- opens.
ALTER ROLE :"role" SET default_transaction_read_only = on;

-- Bound the blast radius of the monitoring session itself.
ALTER ROLE :"role" SET statement_timeout = '15s';
ALTER ROLE :"role" SET lock_timeout = '1s';
ALTER ROLE :"role" SET idle_in_transaction_session_timeout = '30s';
ALTER ROLE :"role" SET application_name = 'pglens';

-- Creating a large object writes pg_largeobject. The read-only default does
-- not stop that on PostgreSQL 15, and on every version once a session turns
-- the default off, so no default can keep it from the role. Nothing else the
-- role holds lets it create one, so the grant goes in every run. PUBLIC's
-- grant is per database: run the script connected to each database the role
-- can reach (the script warns about them).
DO $$
DECLARE
  monitor oid := (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = current_setting('pglens.role'));
  who text := quote_ident(current_setting('pglens.role'));
  creator text;
BEGIN
  FOREACH creator IN ARRAY ARRAY['pg_catalog.lo_create(oid)', 'pg_catalog.lo_creat(integer)',
                                 'pg_catalog.lo_from_bytea(oid, bytea)'] LOOP
    IF has_function_privilege(monitor, creator, 'EXECUTE') THEN
      EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC', creator);
      -- A REVOKE by a role that neither owns the function nor holds the grant
      -- only warns, as on a managed service, so look again.
      IF has_function_privilege(monitor, creator, 'EXECUTE') THEN
        RAISE WARNING '% can still create large objects through %: REVOKE EXECUTE ON FUNCTION % FROM PUBLIC needs the owner of the function',
          who, creator, creator;
      ELSE
        RAISE NOTICE 'revoked EXECUTE on % from PUBLIC: % could create large objects', creator, who;
      END IF;
    END IF;
  END LOOP;
END
$$;

-- pg_logical_emit_message writes WAL, and the read-only default does not stop
-- it: a message is emitted even inside BEGIN READ ONLY. Nothing else the role
-- holds lets it emit, so the revoke goes in every run, like the large-object
-- one above. The function has one overload per argument list (PostgreSQL 19
-- adds a fourth argument), so each is revoked by its own signature. Logical
-- decoding consumers that emit their own messages need an explicit grant.
DO $$
DECLARE
  monitor oid := (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = current_setting('pglens.role'));
  who text := quote_ident(current_setting('pglens.role'));
  emitter record;
BEGIN
  FOR emitter IN
    SELECT p.oid, p.oid::pg_catalog.regprocedure::text AS signature
    FROM pg_catalog.pg_proc p
    WHERE p.proname = 'pg_logical_emit_message'
      AND p.pronamespace = 'pg_catalog'::pg_catalog.regnamespace
  LOOP
    IF has_function_privilege(monitor, emitter.oid, 'EXECUTE') THEN
      EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC', emitter.signature);
      -- As for large objects: a REVOKE by a role that neither owns the function
      -- nor holds the grant only warns, so look again.
      IF has_function_privilege(monitor, emitter.oid, 'EXECUTE') THEN
        RAISE WARNING '% can still write WAL messages through %: REVOKE EXECUTE ON FUNCTION % FROM PUBLIC needs the owner of the function',
          who, emitter.signature, emitter.signature;
      ELSE
        RAISE NOTICE 'revoked EXECUTE on % from PUBLIC: % could write WAL messages', emitter.signature, who;
      END IF;
    END IF;
  END LOOP;
END
$$;

-- What PUBLIC holds, the role holds too. These grants let any role write
-- outside a read-only transaction; pglens never leaves one. The rest are
-- reported, and revoked from PUBLIC only with -v harden_public=1, because
-- revoking them changes what every other role of the database can do: an
-- application that uses temporary tables then needs its own grants. Functions
-- that run as their owner and lo_compat_privileges are only reported: what to
-- do about them depends on the application.
\if :{?harden_public}
SET LOCAL pglens.harden_public = on;
\else
SET LOCAL pglens.harden_public = off;
\endif
DO $$
DECLARE
  monitor oid := (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = current_setting('pglens.role'));
  harden boolean := current_setting('pglens.harden_public')::boolean;
  item record;
  paths text[] := '{}';
BEGIN
  -- With harden_public the grants are revoked, then looked for again: a
  -- REVOKE by a role that neither owns the object nor holds the grant only
  -- warns, which is what a managed-service admin gets on pg_catalog.
  LOOP
    FOR item IN
      SELECT 'temporary tables' AS what,
             format('REVOKE TEMPORARY ON DATABASE %I FROM PUBLIC', current_database()) AS fix,
             true AS revocable
      WHERE has_database_privilege(monitor, current_database(), 'TEMPORARY')
      UNION ALL
      SELECT 'new schemas',
             format('REVOKE CREATE ON DATABASE %I FROM PUBLIC', current_database()),
             true
      WHERE has_database_privilege(monitor, current_database(), 'CREATE')
      UNION ALL
      SELECT 'large objects',
             'REVOKE EXECUTE ON FUNCTION pg_catalog.lo_create(oid), pg_catalog.lo_creat(integer), pg_catalog.lo_from_bytea(oid, bytea) FROM PUBLIC',
             true
      WHERE has_function_privilege(monitor, 'pg_catalog.lo_create(oid)', 'EXECUTE')
         OR has_function_privilege(monitor, 'pg_catalog.lo_creat(integer)', 'EXECUTE')
         OR has_function_privilege(monitor, 'pg_catalog.lo_from_bytea(oid, bytea)', 'EXECUTE')
      UNION ALL
      SELECT 'large objects of every owner',
             'ALTER SYSTEM SET lo_compat_privileges = off',
             false
      WHERE current_setting('lo_compat_privileges')::boolean
      UNION ALL
      SELECT format('new objects in schema %I', n.nspname),
             format('REVOKE CREATE ON SCHEMA %I FROM PUBLIC', n.nspname),
             true
      FROM pg_namespace n
      WHERE has_schema_privilege(monitor, n.oid, 'CREATE')
        AND n.nspname !~ '^pg_(temp|toast_temp)_'
      UNION ALL
      SELECT format('rows of %s', c.oid::regclass),
             format('REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON %s FROM PUBLIC', c.oid::regclass),
             true
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE c.relkind IN ('r', 'p', 'v', 'f', 'm')
        AND n.nspname NOT IN ('pg_catalog', 'information_schema')
        AND has_table_privilege(monitor, c.oid, 'INSERT, UPDATE, DELETE, TRUNCATE')
      UNION ALL
      SELECT format('sequence %s', c.oid::regclass),
             format('REVOKE USAGE, UPDATE ON SEQUENCE %s FROM PUBLIC', c.oid::regclass),
             true
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      WHERE c.relkind = 'S'
        AND n.nspname NOT IN ('pg_catalog', 'information_schema')
        -- has_sequence_privilege() fails on anything but a sequence, and
        -- nothing orders the conditions of a WHERE clause.
        AND CASE WHEN c.relkind = 'S' THEN has_sequence_privilege(monitor, c.oid, 'USAGE, UPDATE') END
      UNION ALL
      SELECT format('%s, which runs as %I', p.oid::regprocedure, pg_get_userbyid(p.proowner)),
             format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC', p.oid::regprocedure),
             false
      FROM pg_proc p
      JOIN pg_namespace n ON n.oid = p.pronamespace
      WHERE p.prosecdef
        AND n.nspname NOT IN ('pg_catalog', 'information_schema')
        AND has_function_privilege(monitor, p.oid, 'EXECUTE')
    LOOP
      IF harden AND item.revocable THEN
        EXECUTE item.fix;
        RAISE NOTICE 'harden_public: %', item.fix;
      ELSIF NOT harden THEN
        paths := paths || format('%s (%s;)', item.what, item.fix);
      END IF;
    END LOOP;
    EXIT WHEN NOT harden;
    harden := false;
  END LOOP;
  IF cardinality(paths) > 0 AND current_setting('pglens.harden_public')::boolean THEN
    RAISE WARNING 'after harden_public, % can still, like every role, write outside a read-only transaction through: %',
      quote_ident(current_setting('pglens.role')), array_to_string(paths, ', ')
      USING HINT = 'harden_public revokes grants to PUBLIC it is allowed to revoke and nothing else. Review the statements in parentheses with the owner of each object: they change what every role can do.';
  ELSIF cardinality(paths) > 0 THEN
    RAISE NOTICE '%, like every role, can write outside a read-only transaction through: %',
      quote_ident(current_setting('pglens.role')), array_to_string(paths, ', ')
      USING HINT = 'pglens never leaves one: every session it opens is read-only. Rerun this script with -v harden_public=1 to revoke the grants to PUBLIC, or review the statements in parentheses.';
  END IF;
END
$$;

COMMIT;
