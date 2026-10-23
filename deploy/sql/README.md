# Monitoring-user permission tiers

`monitoring_user.sql` creates or reconciles the `pglens` login role
(`-v role=<name>` names another one). It is safe to run again: it adds the
requested memberships and reapplies the session limits without creating an
extension or changing database objects.

The role can only read. Before granting anything, the script checks that an
existing role of that name could not write, and refuses one that:

- is a superuser or has `CREATEDB`, `CREATEROLE`, `REPLICATION` or `BYPASSRLS`;
- is a member, directly or through another role, of any role other than
  `pg_monitor`, `pg_read_all_settings`, `pg_read_all_stats`,
  `pg_stat_scan_tables`, `pg_read_all_data` and `pg_signal_backend`;
- owns objects in any database;
- holds, in the database `psql` is connected to or on a cluster-wide object,
  any privilege but `CONNECT` on databases, `USAGE` on schemas, `SELECT` on
  relations, columns and large objects, and `EXECUTE` on the two control
  functions; this covers parameters (`SET`, `ALTER SYSTEM`) and default
  privileges for future objects as well.

A refusal lists the statement that removes each problem. The script runs in a
single transaction, so a refusal changes nothing. Privileges on objects of
other databases cannot be read from the connected one: the script warns about
each database where the role holds some, so it can be run connected there.

Two further steps keep the role read-only. A database-specific
`default_transaction_read_only` setting of the role (`ALTER ROLE ... IN
DATABASE ... SET`) overrides the role-wide default, so the script resets it.
And whatever `PUBLIC` holds, the role holds too: by default every role can
create temporary tables and large objects, and on databases upgraded from
before PostgreSQL 15 also objects in the `public` schema, outside a read-only
transaction. The script reports every such path it finds in the connected
database (temporary tables, new schemas, large objects, `CREATE` on a schema,
writes on a relation, `USAGE` or `UPDATE` on a sequence, functions that run
as their owner and `lo_compat_privileges = on`) with the statement that
closes it. By default it runs none of those statements, because they change
what every other role can do: an application that uses temporary tables or
large objects then needs its own grants. pglens itself never leaves a
read-only transaction.

`-v harden_public=1` runs the `REVOKE ... FROM PUBLIC` ones, then looks again
and warns about what is left. Functions that run as their owner and
`lo_compat_privileges` stay as they are: what to do about them depends on
the application. A managed-service admin does not own the large-object
functions either, so on such a service that grant stays and the warning
names it. Schemas, relations and functions belong to one database: run the
script with the flag connected to each database the role can reach.

The script runs as a superuser in a database another role may own, so it
pins its own `search_path` to `pg_catalog`: a schema of the database owner,
listed first in the database's `search_path`, cannot put its own functions
in place of the built-ins the script calls.

Run it as a PostgreSQL superuser (or the equivalent managed-service admin):

```sh
# Tier 0: the default read-only role.
psql "$PGLENS_DSN" \
  -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=app \
  -f deploy/sql/monitoring_user.sql

# Tier 1: add relation reads for plan-only EXPLAIN.
psql "$PGLENS_DSN" \
  -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=app -v tier1=1 \
  -f deploy/sql/monitoring_user.sql

# Tier 2: add cancel/terminate as well; tier1=1 is required here too.
psql "$PGLENS_DSN" \
  -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=app -v tier1=1 -v tier2=1 \
  -f deploy/sql/monitoring_user.sql

# Any tier: also revoke from PUBLIC what lets every role write (see above).
psql "$PGLENS_DSN" \
  -v pglens_password="$PGLENS_MONITORING_PASSWORD" \
  -v dbname=app -v harden_public=1 \
  -f deploy/sql/monitoring_user.sql
```

The script does not revoke a previously granted higher tier. To downgrade an
existing role deliberately, run the relevant `REVOKE` statements as an admin,
then run the script again. Tier 3 is never a script flag: extensions are
created manually, in each database where the DBA has approved them.
`pg_stat_statements` is needed in one database only, the one the agent's DSN
names or any monitored database, because its statistics cover every database;
the agent looks for it when it starts. `pgstattuple` is needed in each database
whose tables should be measurable.

## Capability matrix

| Tier | Granted capability | Scheduled checks | Advisor rules | What degrades when absent |
|------|--------------------|------------------|---------------|----------------------------|
| T0 | `pg_monitor`, `CONNECT`, and explicit `EXECUTE` on `pg_control_system()` and `pg_control_checkpoint()` | All listed below, subject to role, PostgreSQL-version and extension prerequisites | All rules listed below | No command `EXPLAIN`/`pgstattuple` (requires T1); no cancel/terminate (requires T2) |
| T1 | T0 + `pg_read_all_data` (the script also lists `pg_read_all_stats` for clarity; it is implied by `pg_monitor`) | No additional scheduled check | No additional rule | Plan-only `EXPLAIN` and `pgstattuple` remain rejected until T1 is visible to the running agent; `allow_explain_analyze` is a separate gate |
| T2 | T1 + `pg_signal_backend` | No additional scheduled check | No additional rule | `cancel` and `terminate` remain rejected until T2 is visible and `allow_signal` is true |
| T3 | DBA-created extensions, never created by pglens | `stat_statements` needs `pg_stat_statements` in the DSN's database or in a monitored one; optional `pgstattuple` is used only by the command executor, in the database that holds the table | Rules needing an unavailable input are reported as `degraded` with the missing extension/input reason | Extension-dependent data is skipped or degraded by name; pglens never silently creates an extension |

### Scheduled checks

At T0 the agent schedules: `instance_info`, `activity`, `locks`,
`database_stats`, `settings`, `wal`, `checkpointer`, `archiver`,
`vacuum_progress`, `replication_slots`, `replication_streaming` on primaries,
`replication_receiver` on standbys, `table_stats`, `index_stats`, and
`bloat_estimate`. `io` is scheduled on PostgreSQL 16 and newer. The dedicated
ASH sampler is available when enabled. `stat_statements` is scheduled when
`pg_stat_statements` is installed and preloaded; installing that extension is
an explicit DBA action, not a pglens action.

Every scheduled check above declares T0. A missing PostgreSQL role, version or
extension prerequisite becomes a named `check_skip` fact and is not treated as
a healthy empty result.

### Advisor rules

All currently shipped advisor rules have `min_tier = T0`. Higher tiers unlock
commands, not additional advisor rules. The complete T0 rule set is:

| Pack | Rule IDs |
|------|----------|
| Query and transactions | `query.slow_mean`, `query.total_time_share`, `query.regression`, `query.temp_bytes_high`, `query.cache_miss_high`, `txn.long_running`, `txn.idle_in_transaction`, `txn.prepared_orphan`, `txn.wraparound_risk`, `txn.high_rollback_ratio` |
| Relations | `index.unused`, `index.duplicate`, `index.redundant_prefix`, `index.invalid`, `index.bloat_high`, `index.divergence`, `table.dead_tuples_high`, `table.never_autovacuumed`, `table.autoanalyze_stale`, `table.wraparound_risk`, `table.bloat_high`, `table.seq_scan_heavy`, `vacuum.starvation`, `vacuum.disabled` |
| Configuration and connections | `config.work_mem_oversized`, `config.work_mem_low`, `config.shared_buffers_low`, `config.shared_buffers_high`, `config.effective_cache_size_mismatch`, `config.maintenance_work_mem_low`, `config.max_connections_high`, `config.track_io_timing_off`, `config.checkpoints_too_frequent`, `config.wal_keep_size_low`, `config.fsync_off`, `config.full_page_writes_off`, `config.drift`, `conn.saturation`, `conn.idle_share_high` |
| Archive and backup | `archive.disabled`, `archive.failing`, `archive.stalled`, `backup.no_basebackup_seen`, `backup.no_strategy` |

The archive and backup rules are cluster-wide: a standby archives only with
`archive_mode=always`, and a base backup counts when it is seen running in
`pg_stat_progress_basebackup` on any member of the cluster (readable through
`pg_monitor`). How each rule reads its data is described under
[Advisor findings](../../README.md#advisor-findings) in the main README.

When an advisor input is unavailable, the API keeps the rule visible with
`state=degraded` and a non-empty `degraded_reason`, for example
`missing extension pg_stat_statements` or a named skipped check. A rule that
has no data is therefore distinguishable from a rule that evaluated healthy.

## Runtime grant acceptance

The E2E test `SYS-PERM-002` starts with the T0 block only, verifies that
plan-only `EXPLAIN` is rejected with a named T1 reason and that signal commands
are rejected with a named T2 reason, then grants `pg_read_all_data` while the
agent is still running. The next capability refresh reports T1 and the same
plan-only `EXPLAIN` completes without restarting the agent. This is the
operator-visible contract for runtime grants.
