# Monitoring-user permission tiers

`monitoring_user.sql` creates or reconciles the `pglens` login role. It is
safe to run again: it adds the requested memberships and reapplies the session
limits without creating an extension or changing database objects.

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
```

The script does not revoke a previously granted higher tier. To downgrade an
existing role deliberately, run the relevant `REVOKE` statements as an admin,
then run the script again. Tier 3 is never a script flag: extensions are
created manually, in each database where the DBA has approved them.

## Capability matrix

| Tier | Granted capability | Scheduled checks | Advisor rules | What degrades when absent |
|------|--------------------|------------------|---------------|----------------------------|
| T0 | `pg_monitor`, `CONNECT`, and explicit `EXECUTE` on `pg_control_system()` and `pg_control_checkpoint()` | All listed below, subject to role, PostgreSQL-version and extension prerequisites | All rules listed below | No command `EXPLAIN`/`pgstattuple` (requires T1); no cancel/terminate (requires T2) |
| T1 | T0 + `pg_read_all_data` (the script also lists `pg_read_all_stats` for clarity; it is implied by `pg_monitor`) | No additional scheduled check | No additional rule | Plan-only `EXPLAIN` and `pgstattuple` remain rejected until T1 is visible to the running agent; `allow_explain_analyze` is a separate gate |
| T2 | T1 + `pg_signal_backend` | No additional scheduled check | No additional rule | `cancel` and `terminate` remain rejected until T2 is visible and `allow_signal` is true |
| T3 | DBA-created extensions, never created by pglens | `stat_statements` needs `pg_stat_statements`; optional `pgstattuple` is used only by the command executor | Rules needing an unavailable input are reported as `degraded` with the missing extension/input reason | Extension-dependent data is skipped or degraded by name; pglens never silently creates an extension |

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
| Query | `query.slow_mean`, `query.total_time_share`, `query.regression`, `query.temp_bytes_high`, `query.cache_miss_high` |
| Relations | `index.unused`, `index.duplicate`, `index.redundant_prefix`, `index.invalid`, `index.bloat_high`, `index.divergence`, `table.dead_tuples_high`, `table.never_autovacuumed`, `table.autoanalyze_stale`, `table.wraparound_risk`, `table.bloat_high`, `table.seq_scan_heavy`, `vacuum.starvation`, `vacuum.disabled` |
| Configuration and connections | `config.work_mem_oversized`, `config.work_mem_low`, `config.shared_buffers_low`, `config.shared_buffers_high`, `config.effective_cache_size_mismatch`, `config.maintenance_work_mem_low`, `config.max_connections_high`, `config.track_io_timing_off`, `config.checkpoints_too_frequent`, `config.wal_keep_size_low`, `config.fsync_off`, `config.full_page_writes_off`, `config.drift`, `conn.saturation`, `conn.idle_share_high` |
| Archive and backup | `archive.disabled`, `archive.failing`, `archive.stalled`, `backup.no_basebackup_seen`, `backup.no_strategy` |

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
