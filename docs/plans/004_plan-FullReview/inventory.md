# pglens — inventory of what is monitored and shown (from code)

Scope: code-derived inventory of the agent checks, ASH sampler, advisor rules, alert rules, API endpoints and UI pages.
Source of truth: the working tree of branch `review/full-audit-2026-10` (read directly from files). The tokensave index refused to serve this branch (it serves `main`), so it was not used for this inventory. The README was not used as a source.
Read-only inventory: no database was contacted, no Docker container was started, and no repository file other than this one was written (scratch lists went to the session scratchpad).

Legend: **T0** = read-only tier (pg_monitor + pg_control_system()), **T1** = +pg_read_all_data, **T2** = +pg_signal_backend, **T3** = +CREATE EXTENSION (`internal/pgtype/role.go`). Every check in `internal/check` requires **T0** only. Scopes: `instance` = once per target; `database` = once per monitored database (`cmd/pglens-agent/run.go` `addScheduleEntries`).

## 1. Agent scheduling model

- Scheduler (`internal/agent/scheduler.go`): one schedule entry per (target, check, database) for `database`-scope checks, one per (target, check) for `instance`-scope checks. Each entry has a random start jitter (up to one interval), its own circuit breaker, and a per-check timeout (`context.WithTimeout(check.Timeout())`). Scrapes run on a bounded worker pool (`min(GOMAXPROCS, 8)`).
- Interval: the check's `DefaultInterval()`, overridable with `checks.<name>.interval` (`internal/agent/config.go`, `GetCheckInterval`).
- Gating (`check.Requirements.Supports`, `cmd/pglens-agent/run.go` `addScheduleEntries`): role (`Roles`), `MinPG`/`MaxPG`, permission tier, required extensions. A check that does not apply is not scheduled; a `wire.Result` with `SkipReason` is queued instead, so the server sees the skip.
- Session limits applied on every check connection (`check.ApplySessionLimits`): `statement_timeout` = check timeout, `lock_timeout = '1s'`, `idle_in_transaction_session_timeout = '30s'`. Checks use plain session `SET`, not `SET LOCAL`.
- Database discovery (`internal/agent/discovery.go`, `Manager.Discover`): `pg_database` rows with `datallowconn AND NOT datistemplate`, ordered by `pg_stat_database.xact_commit` desc. Default exclude patterns: `^template\d$`, `^rdsadmin$`, `^azure_.*$` (used only when the target sets no `exclude` list). `include`/`exclude` are regexes. A `databases.max` budget keeps the top-N by xact_commit; the rest get `SkipReason = "db_budget"`. A database created after start is reported with `discovered_after_start` (`reportScheduled`). Default `databases.max` is 10 (`DefaultConnOptions`); `Manager.ForDatabase` also evicts the oldest idle per-database pool when the cap is reached.
- Cardinality (`internal/cardinality/selector.go`, `internal/check/scopesel.go`): each (instance, database) scope has its own selector. Defaults: `TopN` 50 per ranking, `Hysteresis` 5 cycles, `MaxKeys` 200. The union of two rankings is kept, then capped. `Result.Truncated` = hard cap hit; `Result.Partial` = top-N subset of a larger set (normal). `checks.<name>.top_n` is applied only to `index_stats`, `table_stats`, `bloat_estimate`, `stat_statements` (`TopNConfigurable`). `checks.activity.by_application` enables the per-application connection metric. Idle scopes are pruned after 6h.
- `cardinality.NewBudget` (default 20000 series per push) and `check.RelSelector` / `NewRelSelector` (table/index/bloat caps of 200) are defined but have no caller in `internal/`, `cmd/` or `api/`. No caller was found in non-test code under `internal/`, `cmd/` or `api/`, so they are dead code as far as this scan shows. `internal/server/pipeline.go` was not checked for a separate series cap.
- Agent-side flags that gate on-demand commands (`internal/agent/config.go`): `targets[].allow_explain_analyze` (default false) and `targets[].allow_signal` (default false). `commands.enabled` defaults to true; `false` makes the agent stop polling commands.
- `host` sampler (`internal/agent/`, `cmd/pglens-agent/run.go` `hostWireResult`): enabled by default, interval default 30s, reads `/proc` and `/sys` (`HOST_PROC`, `HOST_SYS`). Not a PostgreSQL check; not inventoried further here.

## 2. Scheduled checks (`internal/check`, 18 registered)

All 18 are registered via `check.Register` in `init()`. "Gate" means the `Requires()` result. Interval/timeout are the implementation defaults.

| # | Check | Scope | Gate | Interval / timeout | Sources read (catalog / function) | Metrics emitted (name, kind, labels) | Cardinality |
|---|---|---|---|---|---|---|---|
| 1 | `activity` | instance | T0 | 10s / 2s | `pg_stat_activity` (client backends, not own pid), `current_setting('max_connections')`, `pg_prepared_xacts`, `pg_database` (`age(datfrozenxid)`) | `pg_backends` gauge {state, wait_event_type}; `pg_max_xact_age_seconds`; `pg_max_idle_in_transaction_seconds`; `pg_connections_used`, `pg_connections_limit`, `pg_connections_used_ratio` (only if max_connections > 0); `pg_connections_by_database` {datname} top-10 + `other`; `pg_connections_by_application` {application_name} top-10 + `other` (only with `by_application`); `pg_max_state_age_seconds` {state}; `pg_prepared_xacts` (count); `pg_oldest_prepared_xact_seconds`; `pg_max_datfrozenxid_age` (max XID age over all databases); `pg_backends_waiting_ratio` (active sessions with a non-CPU wait / active sessions) | bounded (state x wait type; top-10 + other) |
| 2 | `archiver` | instance | T0 | 60s / 5s | `current_setting('archive_mode')`; `pg_stat_progress_basebackup`; `pg_stat_archiver` (only when archive_mode on/always); `pg_ls_archive_statusdir()` (needs pg_monitor; errors 42501/42883 tolerated) | `pg_archive_mode_enabled` gauge; `pg_basebackup_progress_ratio` {phase} (only if total > 0); `pg_basebackup_jobs_running`; when archiving: `pg_archiver_archived_total` (counter), `pg_archiver_failed_total` (counter), `pg_archiver_failed_ratio` (1 if last failure is newer than last archive), `pg_archiver_last_archived_age_seconds`, `pg_archiver_last_failed_age_seconds`, `pg_archiver_ready_files` (count of `.ready` files). `stats_reset` from `pg_stat_archiver` is the result reset hint | bounded (one row per running basebackup) |
| 3 | `checkpointer` | instance | T0 | 30s / 5s | PG >= 17: `pg_stat_checkpointer`; PG < 17: `pg_stat_bgwriter` (checkpoint columns); both: `pg_stat_bgwriter` (buffer columns) | all counters: `pg_checkpoints_timed_total`, `pg_checkpoints_requested_total`, `pg_checkpoint_write_time_ms_total`, `pg_checkpoint_sync_time_ms_total`, `pg_bgwriter_buffers_clean_total`, `pg_bgwriter_maxwritten_clean_total`, `pg_buffers_alloc_total` | 7 series, no labels |
| 4 | `database_stats` | instance | T0 | 30s / 2s | `pg_stat_database` (all rows with datname, template DBs included) | counters {database}: `pg_xact_commit_total`, `pg_xact_rollback_total`, `pg_blks_read_total`, `pg_blks_hit_total`, `pg_deadlocks_total`, `pg_temp_bytes_total`, `pg_conflicts_total`; gauge `pg_numbackends`; ratios `pg_xact_rollback_ratio`, `pg_blks_hit_ratio`. Result reset hint = max `stats_reset` | one set per database in the cluster; no cap |
| 5 | `instance_info` | instance | T0 | 10s / 2s | `pg_control_system()` (falls back without system_identifier on permission denied), `pg_is_in_recovery()`, `server_version_num`, `pg_postmaster_start_time()`, `inet_server_addr/port()` | `pg_up` (always 1), `pg_in_recovery`, `pg_version_num`, `pg_uptime_seconds`, `pg_addr` {addr}, `pg_port`; labels `system_identifier`, `role` (primary/standby), `cluster_id_source` (system_identifier or manual) | 1 series per metric |
| 6 | `io` | instance | T0, **MinPG 16** | 30s / 5s | `information_schema.columns` probe of `pg_stat_io`, `current_setting('track_io_timing')`, `pg_stat_io` | `pg_io_timing_enabled`; per `pg_stat_io` row with activity: counters `pg_io_<column>` for every probed column among reads, read_bytes, writes, write_bytes, extends, hits, evictions, fsyncs, read_time, write_time, fsync_time; labels backend_type, object, context | bounded by the pg_stat_io enumeration (backend_type x object x context) |
| 7 | `wal` | instance | T0 | 15s / 5s | `pg_current_wal_lsn()` or `pg_last_wal_replay_lsn()` (on standby), `information_schema.columns` probe of `pg_stat_wal`, `pg_stat_wal` | `pg_wal_lsn_bytes` (counter); `pg_wal_stats_columns_available`; `pg_wal_<column>` for every `pg_stat_wal` column except `stats_reset` (counter if `_count`/`_bytes`/`_time` suffix or wal_records/wal_fpi/wal_buffers_full/wal_sync/wal_write, else gauge) | 1 series per column |
| 8 | `vacuum_progress` | instance | T0 | 30s / 5s | `information_schema.columns` probe of `pg_stat_progress_vacuum` (chooses `max_dead_tuple_bytes` vs `max_dead_tuples`), `pg_stat_progress_vacuum` JOIN `pg_database` LEFT JOIN `pg_class`, `pg_stat_progress_analyze` | per job, first 20 only: `pg_vacuum_heap_blks_total`, `pg_vacuum_heap_blks_scanned`, `pg_vacuum_heap_blks_vacuumed`, `pg_vacuum_index_vacuum_count`, `pg_vacuum_dead_tuples` or `pg_vacuum_dead_tuple_bytes` (labels datname, relname, phase); `pg_vacuum_jobs_running`; per analyze job, first 20: `pg_analyze_sample_blks_total`, `pg_analyze_sample_blks_scanned`; `pg_analyze_jobs_running`. `Truncated` when more than 20 jobs | <= 20 jobs per kind |
| 9 | `replication_receiver` | instance | **Role standby**, T0 | 10s / 2s | `pg_stat_wal_receiver` (LEFT JOIN), `pg_last_wal_receive_lsn()`, `pg_last_wal_replay_lsn()`, `pg_last_xact_replay_timestamp()` | `replication_receiver_status` {status, sender_host, slot_name} (1 streaming, 2 catchup, 0 disconnected, -1 other); `replication_receiver_sender_port`; `replication_receiver_replay_lag_seconds` (only when non-null) | 1 row |
| 10 | `replication_slots` | instance | T0 | 15s / 2s | `pg_replication_slots` (physical AND logical slots), `pg_wal_lsn_diff`, `pg_is_in_recovery()` | `replication_slot_active` {slot_name, slot_type, status}; `replication_slot_active_pid`; `replication_slot_safe_wal_size_bytes` (only when non-null, i.e. max_slot_wal_keep_size limited); `replication_slot_retained_bytes` | 1 set per slot |
| 11 | `replication_streaming` | instance | **Role primary**, T0 | 10s / 2s | `pg_stat_replication` (walsender rows), `pg_current_wal_lsn()` or `pg_last_wal_receive_lsn()` (cascading) | `replication_{write,flush,replay}_lag_bytes` and `replication_{write,flush,replay}_lag_seconds`; labels application_name (made unique as `app@addr:port` when duplicated or empty), client, state, sync_state | 1 set per walsender |
| 12 | `locks` | instance | T0 | 10s / 10s | `pg_stat_activity` (client backends); `pg_blocking_pids(pid)` only when `wait_event_type = 'Lock'` | `pg_blocked_sessions`; `pg_blocking_sessions` (distinct blockers); `pg_max_block_age_seconds`; `pg_lock_waits` {wait_event} top-10 + `other`. Fact `lock_tree` (key `current`, JSON): `nodes` (blocked sessions and their blockers) and `sessions` (every client backend). Query text is shipped, truncated to 2048 bytes (UTF-8 safe) | bounded metrics; facts carry all client sessions |
| 13 | `settings` | instance | T0 | 1h / 10s | `pg_settings` | Facts `setting` (key = name; value text; labels unit, source, vartype, context, pending_restart) for the 43 allowlisted names and for every setting whose value differs from `boot_val`. Values of `*_command` and `*conninfo` are redacted. Metrics `pg_setting_bytes` {name} and `pg_setting_seconds` {name} (numeric settings with byte or time unit, >= 0; only for the facts above); `pg_settings_pending_restart` (count) | non-default settings only |
| 14 | `table_stats` | database | T0 | 5m / 30s | `pg_stat_user_tables` JOIN `pg_class` with `relkind = 'r'` (partitions included, partitioned parents `'p'` excluded), `pg_total_relation_size`, `pg_table_size`, `age(relfrozenxid)` | counters {schemaname, relname}: `pg_table_seq_scan`, `pg_table_seq_tup_read`, `pg_table_idx_scan`, `pg_table_idx_tup_fetch`, `pg_table_n_tup_ins`, `pg_table_n_tup_upd`, `pg_table_n_tup_del`, `pg_table_n_tup_hot_upd`; gauges: `pg_table_n_live_tup`, `pg_table_n_dead_tup`, `pg_table_n_mod_since_analyze`, `pg_table_autovacuum_count`, `pg_table_autoanalyze_count`, `pg_table_relpages`, `pg_table_reltuples`, `pg_table_relfrozenxid_age`, `pg_table_total_bytes`, `pg_table_table_bytes`, `pg_table_toast_bytes`, `pg_table_dead_tuple_ratio` (0 when no live tuples), `pg_table_last_{vacuum,autovacuum,analyze,autoanalyze}_age_seconds` (only when the timestamp is set). Selection: union of top-50 by n_dead_tup and top-50 by total bytes, capped at 200 | top-N + cap per database |
| 15 | `index_stats` | database | T0 | 5m / 30s | `pg_stat_user_indexes` JOIN `pg_index` LEFT JOIN `pg_statio_user_indexes`, `pg_relation_size`, `pg_get_indexdef` | counters {schemaname, relname, indexrelname}: `pg_index_idx_scan`, `pg_index_idx_tup_read`, `pg_index_idx_tup_fetch`, `pg_index_idx_blks_read`, `pg_index_idx_blks_hit`; gauges: `pg_index_index_bytes`, `pg_index_is_unique`, `pg_index_is_primary`, `pg_index_is_valid`. Fact `index_def` (key = schema.index, `def_hash` = SHA-256 of the normalized column list, value = `pg_get_indexdef`). Selection ranks by (bytes - idx_scan) and by bytes, top-50 each, capped at 200 | top-N + cap per database |
| 16 | `bloat_estimate` | database | T0 | 6h / 120s | embedded `bloat_estimate.sql`: `pg_stats` (non-inherited, all columns visible), `pg_class`, `pg_attribute`, `pg_relation_size`, `reloptions` (fillfactor) | `pg_bloat_real_bytes`, `pg_bloat_expected_bytes`, `pg_bloat_bytes`, `pg_bloat_ratio` (gauges; labels schemaname, relname, indexrelname = '', object_kind = 'table', method = 'estimate'). Heap tables only, no index bloat. Tables under 1 MiB are skipped; tables with missing stats get no estimate | top-N + cap per database |
| 17 | `stat_statements` | database | T0 + **extension pg_stat_statements** | 60s / 15s | `pg_stat_statements` aggregated per queryid over userid/toplevel for the dbid of the database; `pg_stat_statements_info.stats_reset` (only for the reset hint); `pg_database` | counters {queryid}: `pg_stat_statements_calls_total`, `pg_stat_statements_total_exec_time_ms`, `pg_stat_statements_rows_total`, `pg_stat_statements_shared_blks_hit_total`, `pg_stat_statements_shared_blks_read_total`, `pg_stat_statements_wal_bytes_total`, `pg_stat_statements_temp_blks_written_total`. Query texts shipped (LEFT(query, 8192)) once per (cluster, database, queryid), resent after 1h or when the text changes; LRU of 4096 per cluster/database. Selection: union of top-50 by total_exec_time and top-50 by calls, capped at 200; SQL pre-filter max(300, 2 x top_n) | top-N + cap per database |
| 18 | `ash` | instance | T0 | 1s / 500ms (registered but **never scheduled**) | see section 3 | `ash_samples` (gauge). The scheduler skips `ash` (`addScheduleEntries`); the live path is the dedicated sampler in `cmd/pglens-agent/ash.go` | see section 3 |

Notes on the table:

- **Not present as checks**: pg_stat_user_functions, pg_sequences, triggers (pg_trigger), PostGIS, logical replication subscriptions (pg_stat_subscription, pg_subscription), recovery conflict detail (pg_stat_database_conflicts), SLRU (pg_stat_slru), progress views for create_index / cluster / copy, pg_stat_replication_slots (the check reads pg_replication_slots), pg_stat_all_tables, pg_locks (locks are read through pg_stat_activity + pg_blocking_pids), temp file count (only `pg_temp_bytes_total`), JIT counters, multixact age (no relminmxid / mxid anywhere), constraints (no pg_constraint, so no missing-FK-index detection), partitioned-table metadata (relispartition, pg_partitioned_table). Confirmed with a literal scan of `internal/`, `cmd/` and `api/` (non-test .go, .yaml).
- **Version gating**: `io` (MinPG 16); `checkpointer` (`>= PG17` picks `pg_stat_checkpointer`); no check sets `MaxPG`, so the monitoring path never refuses PG 19 on version. The only PG 19-specific code is `pgtype.Supported()` (`internal/pgtype/version.go`: major 15 to 18, decision D11), which is called only by `pglens-agent check` (`cmd/pglens-agent/main.go:176`) to print "NOT SUPPORTED" for 19. `internal/agent/exec_explain.go:77` has a `PG < 16` branch (purpose not reviewed). Behaviour on PG 19 is therefore: collected like PG 18 (checkpointer on the PG 17+ path), but reported as unsupported by the diagnostic. The PG 19 catalog changes are not validated by any code here.
- **Information leaks to note for the inventory**: `locks` ships the query text of every client backend (2 KB each); `stat_statements` ships query texts; `settings` ships non-default values (redacted for *_command and *conninfo).
- **Partitions**: `table_stats` reads each partition as a regular table (relkind `r`) and never reads the partitioned parent (`p`). The same `relkind = 'r'` filter also excludes materialized views and foreign tables. No partition-tree, detach, or default-partition metric exists.
- **Indexes**: `index_stats` reports usage (scans, tuple reads, block reads/hits), size, uniqueness, primary and validity flags, plus the definition hash. Unused, duplicate and invalid index detection, if any, is in the advisor (section 5), not in the check.
- **Deadlocks**: only the cumulative per-database counter `pg_deadlocks_total` (database_stats). No deadlock event detail.
- **Recovery conflicts**: only the cumulative per-database counter `pg_conflicts_total` (database_stats, from `pg_stat_database.conflicts`).

## 3. ASH sampler (`internal/ash`, `cmd/pglens-agent/ash.go`)

- Enabled by default for each target; disabled by `checks.ash.enabled: false` (the target then reports `ASHEnabled = false` on the instance, no `metrics_ash` rows).
- Sampling: a dedicated connection per target (never the shared pool). Every `checks.ash.interval` (default 1s) it runs `pg_stat_activity` filtered on `backend_type = 'client backend' AND state <> 'idle' AND pid <> pg_backend_pid()`, selecting datname, state, wait_event_type (NULL -> 'CPU'), wait_event (NULL -> 'CPU') and query_id. Tick deadline 500 ms; session `statement_timeout = 400` ms. Reconnect backoff 5 s.
- `compute_query_id` discovery: latched "on" on the first non-null query_id; latched "off" after 5 consecutive samples with rows and every query_id null; a warning is logged once when known off.
- Aggregation: 10-second windows (`ashWindow`). Key = (datname, wait_event_type, wait_event, state, queryid). At most 100 keys per window; the rest fold into one `other` bucket. The sum of samples is conserved.
- Emitted to the server as `ash_samples` gauge per bucket with labels datname, wait_event_type, wait_event, state, queryid (when present), window_seconds (10), window_ticks (successful ticks in the window). Failed ticks do not count in the denominator.
- Not captured: idle sessions and query text (only queryid). The agent keeps no ASH history beyond the open window.

## 4. Other agent-side work (short)

- Commands (`internal/agent/command.go`, `exec_explain.go`, `exec_signal.go`, `exec_pgstattuple.go`, `internal/command/gate.go`): on-demand `explain`, `cancel`, `terminate`, `pgstattuple`. Gated by the agent config flags above (`allow_explain_analyze`, `allow_signal`, both default false) and by the command kind and tier. Detailed hardening review is not part of this inventory; see the command section of the audit plan.

## 5. Advisor (`internal/advisor`, server side): 44 rules

Engine (`engine.go`): runs in the server, one evaluator elected through `ensureLeader`. Default interval 15 min, overridable with `PGLENS_ADVISOR_INTERVAL` (Go duration, or integer seconds). Input is a per-instance `Snapshot` loaded from store tables (`snapshot.go`: metrics, facts, tables, indexes, bloat, statements, baseline, siblings, replication, settings, host). Rule gating: a rule whose `Needs` input is missing yields a **degraded** finding ("Advisor input unavailable"); a panic yields degraded "rule failed"; a rule whose `MinTier` is above the instance tier yields degraded. Every rule has `MinTier = T0`. Finding ID = `rule_id/<datname>/<object>`. States: open, degraded, muted, resolved. Thresholds are hard-coded constants; no configuration key exists for them.

### 5.1 Config, connections, archive, backup (20 rules, scope instance)

| Rule | Sev | Inputs | Condition |
|---|---|---|---|
| `config.work_mem_oversized` | warning | settings work_mem_bytes, max_connections; host memory (cgroup-aware) | work_mem x max_connections > 25 % of usable memory |
| `config.work_mem_low` | info | settings; finding `query.temp_bytes_high` | work_mem < 4 MiB while temp-bytes findings exist |
| `config.shared_buffers_low` | warning | settings shared_buffers_bytes; host memory | < 15 % of usable memory |
| `config.shared_buffers_high` | warning | same | > 40 % of usable memory |
| `config.effective_cache_size_mismatch` | info | effective_cache_size_bytes; host memory | < 25 % or > 150 % of usable memory |
| `config.maintenance_work_mem_low` | info | maintenance_work_mem_bytes; host memory | host > 8 GiB and maintenance_work_mem < 256 MiB |
| `config.max_connections_high` | warning | max_connections; pg_connections_used_ratio | max_connections > 200 and used ratio < 0.30 |
| `config.track_io_timing_off` | info | track_io_timing | off |
| `config.checkpoints_too_frequent` | warning | pg_checkpoints_requested_ratio (derived: requested / (requested + timed) over 24 h) | > 0.20 |
| `config.wal_keep_size_low` | warning | settings wal_keep_size_bytes, wal_segment_size_bytes; Replication (standby count, active physical slots, max lag) | a standby lags more than wal_keep_size and more than one segment, and fewer physical slots than standbys |
| `config.fsync_off` | critical | fsync | off |
| `config.full_page_writes_off` | critical | full_page_writes | off |
| `config.drift` | warning | settings of every sibling instance of the cluster | same setting differs on a sibling (contexts in `SettingDriftExcluded` skipped) |
| `conn.saturation` | warning | pg_connections_used_ratio | > 0.80 |
| `conn.idle_share_high` | info | pg_connections_idle_share (derived: idle / total client backends), pg_connections_used | idle share > 0.70 and used > 100 |
| `archive.disabled` | info | archive_mode; cluster archiving state | no member of the cluster archives WAL |
| `archive.failing` | critical | archive_mode; pg_archiver_failed_ratio | ratio > 0.999; degraded if archiving is on but no archiver stats arrived |
| `archive.stalled` | critical | pg_archiver_last_archived_age_seconds, pg_archiver_ready_files, archive_timeout_seconds | last archive older than max(3600 s, 10 x archive_timeout) and ready files != 0 |
| `backup.no_basebackup_seen` | warning | cluster archiving; base-backup sightings (`backup` facts, key basebackup_running, from siblings too); monitored duration | no base backup seen in 7 days; degraded if monitored < 7 days |
| `backup.no_strategy` | critical | archive_mode; cluster archiving; base-backup sightings | no archiving and no base backup seen in 30 days |

### 5.2 Query and transaction (10 rules)

| Rule | Sev | Inputs | Condition |
|---|---|---|---|
| `query.slow_mean` | warning | Statements (pg_stat_statements, per sample window) | calls >= 10 and mean exec time > 500 ms |
| `query.total_time_share` | warning | Statements | statement share of total exec time > 30 % |
| `query.regression` | critical | Statements, Baseline (8-day window, mean per queryid) | mean >= 3 x baseline and >= 100 ms; degraded when no baseline |
| `query.temp_bytes_high` | warning | Statements; work_mem_bytes (default 4 MiB) | average temp bytes per call > work_mem |
| `query.cache_miss_high` | info | Statements | calls > 1000 and shared-read share > 30 % |
| `txn.long_running` | warning | pg_max_xact_age_seconds | > 900 s |
| `txn.idle_in_transaction` | warning | pg_max_idle_in_transaction_seconds | > 300 s |
| `txn.prepared_orphan` | critical | pg_oldest_prepared_xact_seconds | > 3600 s |
| `txn.high_rollback_ratio` | info | pg_xact_rollback_ratio (per database) | > 0.10 (scope database) |
| `txn.wraparound_risk` | critical | pg_max_datfrozenxid_age | > 1e9 |

### 5.3 Relation, index and vacuum (14 rules)

| Rule | Sev | Inputs | Condition |
|---|---|---|---|
| `index.unused` | warning | Indexes (usage history, peak scan rate) | no scan in history, size > 10 MiB, not primary or unique; degraded if history < 7 days |
| `index.duplicate` | warning | Indexes (definition hash, access method) | same table, same method, same definition hash |
| `index.redundant_prefix` | info | Indexes (definition columns, predicate, method) | non-unique btree whose columns are a prefix of another btree on the same table with the same predicate |
| `index.invalid` | critical | Indexes (indisvalid) | index not valid |
| `index.bloat_high` | warning | Bloat rows with kind `index` | ratio > 0.40 and > 50 MiB. **No producer**: `bloat_estimate` emits only `object_kind = 'table'`, so this rule cannot fire on current data |
| `index.divergence` | warning | Siblings (index_def hashes per sibling) | index absent on a sibling, or with a different definition |
| `table.dead_tuples_high` | warning | Tables (n_dead_tup, n_live_tup) | dead/live > 0.20 and dead >= 10 000 |
| `table.never_autovacuumed` | warning | Tables (last vacuum timestamps) | live > 100 000 and no vacuum or autovacuum timestamp; not evaluated on a standby |
| `table.autoanalyze_stale` | info | Tables (n_mod_since_analyze) | modifications > 20 % of live tuples |
| `table.wraparound_risk` | critical | Tables (relfrozenxid age) | age > 1e9 |
| `table.bloat_high` | warning | Bloat rows with kind `table` | ratio > 0.30 and > 100 MiB |
| `table.seq_scan_heavy` | info | Tables (seq and index scan rates) | live > 100 000, seq rate > 0, index rate <= 10 % of seq rate (missing-index candidate) |
| `vacuum.starvation` | critical | Tables, pg_vacuum_jobs_running, autovacuum_max_workers | more than 10 tables above 20 % dead ratio while all autovacuum workers are busy |
| `vacuum.disabled` | critical | autovacuum | off |

Advisor coverage gaps (absent as rules): missing foreign-key index, sequences, partitions, triggers, function statistics, extensions inventory, PostGIS, logical replication, recovery conflicts, SLRU, JIT, temp file count, multixact age, lock contention, progress views, ASH-derived findings.

## 6. Alerts (`internal/alert`, server side)

Engine (`engine.go`): evaluates every 30 s (`defaultEngineInterval`); metric lookback = max(2 x interval, 2 min). Silences (`silence.go`) suppress alerts (matched alerts keep state, `Suppressed = true`). Notification channels (`internal/alert/notify`): **slack** and **webhook** (JSON), with claim/mark deduplication per (alert, channel, phase) in the store. Source kinds: `metric` (table `metrics`; `pg_replication_*` names go to `metrics_replication`; `up` reads `instances.last_seen`, revoked agents are inapplicable), `event` (table `events`), and two cluster aggregations (`replica.all_standbys_lagging`, `replica.no_sync_standby`). Event types persisted to the `events` table (complete list from code): `staleness.go`: `agent_down`, `agent_up`, `instance_unreachable`, `instance_reachable`, `no_primary_in_cluster`, `slot_inactive`; `inventory.go`: `cluster_id_changed`, `role_change`, `cluster_identity_conflict`, `duplicate_instance_suspected`; `pipeline.go`: `cardinality_truncated`, `counter_reset_detected` (also written by `store/write.go` `WriteEvents`); topology engine (`internal/topology/engine.go`, forwarded by `pipeline.go`): `failover_detected`, `role_change`, `orphan_standby`, `split_brain_detected`. `cascade_changed` is defined in the topology engine but never emitted. The API edits only `enabled`, `severity`, `threshold`, `for_seconds` of stored rules (`PUT /api/v1/alert-rules/{rule_id}`).

### 6.1 Built-in rules (code, `builtin.go`, Tier 0, always enabled): 10

| Rule | Sev | Scope | Source and condition |
|---|---|---|---|
| `agent_down` | critical | instance | `up` < 1 for 90 s (last_seen older than 60 s) |
| `instance_unreachable` | critical | instance | event `instance_unreachable` |
| `check_failing` | warning | instance | `pglens_check_error_rate` > 0 for 5 min |
| `no_primary_in_cluster` | critical | cluster | event |
| `agent_buffer_full` | warning | instance | `pglens_samples_dropped_rate` > 0 for 1 min (emitted by the agent self-result) |
| `clock_skew` | warning | instance | `pglens_agent_clock_skew_seconds` > 30 for 1 min |
| `cardinality_budget_exceeded` | warning | instance | `pglens_cardinality_truncated_rate` > 0 for 5 min |
| `failover_detected` | critical | cluster | event |
| `split_brain_detected` | critical | cluster | event |
| `slot_inactive` | warning | instance | event `slot_inactive` |

### 6.2 Seeded rules (table `alert_rules`, migration 0006, repaired by 0013): 10. All are enabled by default (`enabled boolean NOT NULL DEFAULT true`, `0006_alerts.sql:7`), and the seed uses `ON CONFLICT DO NOTHING`.

| Rule | Sev | Scope | Metric and threshold | For |
|---|---|---|---|---|
| `replica.lag_high` | warning | instance | `pg_replication_lag_seconds` > 30 (replay lag, per standby) | 120 s |
| `replica.all_standbys_lagging` | critical | cluster | min standby replay lag > 30 | 120 s |
| `replica.no_sync_standby` | critical | cluster | count of sync/quorum standbys < 1 | 60 s |
| `conn.near_max` | warning | instance | `pg_connections_used_ratio` > 0.8 | 120 s |
| `conn.idle_in_transaction` | warning | instance | `pg_max_idle_in_transaction_seconds` > 300 | 120 s |
| `txn.long_running` | warning | instance | `pg_max_xact_age_seconds` > 900 | 120 s |
| `txn.wraparound_risk` | critical | instance | `pg_max_datfrozenxid_age` > 1e9 | 300 s |
| `db.deadlock_rate` | warning | instance | `pg_deadlocks_total` > 0.1, positive samples only | 300 s |
| `archive.failing` | critical | instance | `pg_archiver_failed_ratio` > 0 | 300 s |
| `disk.free_low` | critical | instance | `host_disk_free_ratio` < 0.15 | 300 s |

Alerting gaps (no rule): lock waits and blocked sessions (locks check), recovery conflicts, temp files, checkpoints, ASH saturation, replication slot retained bytes (`pg_replication_slot_retained_bytes` is mapped but unused by any seeded rule), table or index bloat, check-level errors other than the rate, and any database-level rule other than the deadlock rate.

## 7. API endpoints (`internal/server`, `api/openapi.yaml`)

Code registrations (`r.Get`/`r.Post`/`r.Put`/`r.Delete`, 45 path+method pairs) match the 45 path+method pairs in `api/openapi.yaml`. Auth: `RequireCredential` is installed as a router-wide middleware (`internal/server/http.go`, "the credential gate is always installed"); the exempt paths are defined inside `RequireCredential` and were not reviewed here. Agent routes (push, command poll and result) and browser routes (session, UI data) are both behind it. Unknown `/api/` paths return 404 `not_found`; the SPA is served for other paths when the UI is enabled (`webui.go`).

| Area | Method and path | What it returns (openapi summary) |
|---|---|---|
| Fleet | GET `/api/v1/clusters` | List monitored clusters |
| Fleet | GET `/api/v1/instances` | List monitored instances |
| Fleet | GET `/api/v1/instances/{id}` | One instance and its databases |
| Cluster | GET `/api/v1/clusters/{id}/topology` | Replication topology and failover events |
| Cluster | GET `/api/v1/clusters/{id}/replication` | Replication lag series |
| Cluster | GET `/api/v1/clusters/{id}/slots` | State of every replication slot |
| Cluster | GET `/api/v1/clusters/{id}/settings-drift` | Settings that differ between instances |
| Instance | GET `/api/v1/instances/{id}/activity` | Recent activity metrics grouped by metric name |
| Instance | GET `/api/v1/instances/{id}/databases` | Databases and monitoring status |
| Instance | GET `/api/v1/instances/{id}/host` | Host metrics |
| Instance | GET `/api/v1/instances/{id}/settings` | Observed settings |
| Instance | GET `/api/v1/instances/{id}/tables` | Table relation statistics |
| Instance | GET `/api/v1/instances/{id}/indexes` | Index relation statistics |
| Instance | GET `/api/v1/instances/{id}/bloat` | Bloat estimates |
| Instance | GET `/api/v1/instances/{id}/command-audit` | Immutable command audit stream |
| Instance | POST `/api/v1/instances/{id}/commands` | Queue a command (kind: explain, cancel, terminate, pgstattuple) |
| Locks | GET `/api/v1/locks` | Latest sampled blocking tree |
| Metrics | GET `/api/v1/metrics/query` | Metric as a time series |
| Events | GET `/api/v1/events` | Events, newest first |
| Statements | GET `/api/v1/statements` | Top statements joined to query text |
| Plans | GET `/api/v1/plans` | Plan history for a query |
| ASH | GET `/api/v1/ash` | Activity samples grouped by dimensions |
| ASH | GET `/api/v1/ash/top` | Top ASH queries with text |
| Alerts | GET `/api/v1/alerts` | Alerts with suppression state |
| Alerts | GET `/api/v1/alerts/{alert_key}` | One alert |
| Alerts | GET `/api/v1/alert-rules` | Alert rules |
| Alerts | PUT `/api/v1/alert-rules/{rule_id}` | Update enabled, severity, threshold, for_seconds |
| Alerts | GET `/api/v1/silences` | Active (or all) silences |
| Alerts | POST `/api/v1/silences` | Create silence |
| Alerts | DELETE `/api/v1/silences/{id}` | End silence |
| Findings | GET `/api/v1/findings` | Advisor findings |
| Findings | GET `/api/v1/findings/{finding-id}` | One finding |
| Findings | POST and DELETE `/api/v1/findings/{finding-id}/mute` | Mute until a timestamp / remove mute |
| Findings | GET `/api/v1/advisor/rules` | Advisor rule catalogue |
| Commands | GET `/api/v1/commands/{id}` | State and result of a command |
| Commands | GET `/api/v1/agents/{agent_id}/commands` | Agent polls and claims next command |
| Commands | POST `/api/v1/commands/{id}/result` | Agent submits command result |
| Session | POST, GET, DELETE `/api/v1/session` | Create, report, delete UI session |
| Ingest | POST `/api/v1/push` | Agent envelope push |
| Ops | GET `/healthz`, `/readyz`, `/metrics` | Liveness, DB and migration readiness, Prometheus self-metrics |

The openapi document also defines `CreateCommandRequest` with `kind` enum `[explain, cancel, terminate, pgstattuple]`; "Query text is never sent to this API" (openapi description).

## 8. UI pages (`web/src/routes/index.tsx`, `web/src/features`)

| Route | Surface | Sections and what they display |
|---|---|---|
| `/` | Fleet overview (`fleet/FleetPage`) | Cluster cards (health, instances up/down, firing alerts), summary filters (healthy, degraded, critical, instances up/down, firing alerts), agent health strip |
| `/clusters/:clusterId` | Cluster detail (`cluster/ClusterPage`) | Replication topology graph (`TopologyGraph`), replication lag (`LagSection`), replication slots with retention warning (`SlotsSection`), settings drift (`DriftSection`), event timeline with type filter (`EventTimeline`) |
| `/instances/:instanceId` | Instance detail (`instance/InstancePage`) | Header; database scope selector; overview KPIs in range (connections in use, TPS, cache hit ratio, WAL generated, checkpoints, temp bytes, deadlocks); host metrics (`HostSection`); settings and durability (`SettingsSection`); relations: tables, indexes, bloat estimates, truncation flags; per-relation "Measure exactly" action (`RelationsSection`, `ExactMeasurement`) submits a `pgstattuple` command, tables only |
| `/instances/:instanceId/ash` | ASH and wait analysis (`ash/AshPage`) | ASH chart, breakdowns by dimension with drill path, top queries (`AshTopQueries`) |
| `/instances/:instanceId/queries` | Query inspector (`queries/QueryListPage`) | Statements list from pg_stat_statements |
| `/instances/:instanceId/queries/:queryid` | Query detail (`queries/ExplainPanel`, `PlanHistory`) | Plan only and plan with ANALYZE actions (`ExplainPanel`), plan history with comparison (`PlanHistory`) |
| `/instances/:instanceId/locks` | Locks and activity (`locks/*`) | Activity KPIs: connections used / max, longest transaction, longest idle-in-transaction, oldest state age, connections by database and by application, backends by state, prepared transactions, oldest prepared transaction, frozen-XID age, deadlock rate (`ActivitySection`); blocking tree from the latest sample (`BlockingTree`); cancel query and terminate backend actions (`SignalActions`) |
| `/findings` | Advisor findings (`findings/*`) | Summary (critical, warning, info, rules not evaluated); finding cards with evidence (`FindingCard`); mute dialog (`MuteDialog`); rule catalogue (`RuleCatalogue`) |
| `/alerts` | Alerts (`alerts/AlertsPage`) | Summary, filters, alert list and detail, notification delivery channels (`DeliverySection`) |
| `/events` | Fleet event timeline (`alerts/EventsPage`) | Events across clusters with alert health and suppression status |
| `/alerts/rules` | Alert rules (`alerts/RulesPage`, `RuleEditor`) | Built-in and stored rules; edit enabled, severity, threshold, for |
| `/alerts/silences` | Alert silences (`alerts/SilencesPage`, `SilenceEditor`) | Stored silences; create with matchers, reason, time range; end silence |
| `/settings` | Settings and inventory (`settings/*`) | Server information, authentication, references; instances table; databases by instance; permission tiers; agent telemetry; command audit per instance (`CommandAudit`) |
| `/login`, `*` | Login (`auth/LoginPage`), not found | Session login, 404 |

## 9. On-demand commands (summary; hardening review not in this inventory)

- Kinds: `explain` (queryid, optional datname, optional `analyze`), `cancel` (pid), `terminate` (pid), `pgstattuple` (schema, relation, optional datname). Query text is never sent by the API.
- Gate (`internal/command/gate.go`, `Allowed`): `explain` needs tier T1, and `analyze` also needs `allow_explain_analyze` (default false). `cancel` and `terminate` need tier T2 and `allow_signal` (default false). `pgstattuple` needs tier T1 only, with no opt-in flag; its extension is checked by the executor in the relation's own database. `commands.enabled` false stops polling entirely.
- EXPLAIN ANALYZE executes the statement, so it can write data unless it is wrapped to prevent that. The current wrapping was not reviewed here.
- Explain execution code: `internal/agent/exec_explain.go`; pgstattuple: `exec_pgstattuple.go`; signals: `exec_signal.go`.

## 10. Requested topics: what exists today

| Topic | Exists today | Where | Gap |
|---|---|---|---|
| Partitions / partitioned tables | Partial: each leaf partition is a normal table (relkind `r`) in table_stats | `table_stats` | No partitioned-parent view, no partition count, no default-partition or detach checks |
| Sequences | No | none | No `pg_sequences` access |
| Triggers | No | none | No `pg_trigger` access |
| Functions | No | none | No `pg_stat_user_functions` |
| Indexes: unused | Yes (advisor) | `index.unused`, `index_stats` counters | Needs 7-day history |
| Indexes: duplicate | Yes (advisor) | `index.duplicate`, `index.redundant_prefix` | Btree prefix only |
| Indexes: invalid | Yes | `index.invalid` (critical), `pg_index_is_valid` | None |
| Indexes: bloat | Heap tables only | `bloat_estimate` (kind table); `index.bloat_high` has no producer | Index bloat is not produced |
| Indexes: missing FK index | No | none | No pg_constraint access |
| Extensions inventory | Presence flag only | agent `getExtensions`, list = [pg_stat_statements] | No inventory list in API or UI |
| PostGIS | No | none | None |
| Logical replication | Partial: logical slots only | `replication_slots` (slot_type logical) | No pg_stat_subscription, pg_subscription, or publications |
| Recovery conflicts | Counter only | `pg_conflicts_total` per database (database_stats) | No per-reason breakdown (pg_stat_database_conflicts absent); no rule |
| SLRU | No | none | None |
| Progress: vacuum | Yes | `vacuum_progress` | First 20 jobs only |
| Progress: analyze | Yes | `vacuum_progress` (sample blocks) | First 20 jobs only |
| Progress: basebackup | Yes | `archiver` (`pg_basebackup_progress_ratio`, jobs running) | None |
| Progress: create_index, cluster, copy | No | none | Not read |
| Prepared transactions | Yes | `activity` (`pg_prepared_xacts`, oldest age); `txn.prepared_orphan` (> 3600 s, critical) | None |
| XID age | Yes | `pg_max_datfrozenxid_age` (activity); `relfrozenxid_age` per table; `txn.wraparound_risk`, `table.wraparound_risk`, alert `txn.wraparound_risk` | None |
| Multixact age | No | none | No relminmxid or mxid reads |
| Temp files | Bytes only | `pg_temp_bytes_total` (database_stats); `query.temp_bytes_high` per statement | No temp file count |
| Deadlocks | Counter | `pg_deadlocks_total` (database_stats); alert `db.deadlock_rate` | No event detail |
| JIT | No | none | jit settings only if non-default (raw facts) |
| pg_stat_statements_info | Reset time only | `stats_reset` read by stat_statements (reset hint) | No dealloc or info metric |

Cross-cutting facts for the audit: the 18 checks are T0 only (read-only tier). The write-capable paths are the on-demand commands (section 9) and the server's own writes to its TimescaleDB, not monitored databases.

## 11. Collected versus displayed (UI scan)

Method: literal scan of `web/src` for metric-name strings (`pg_`, `replication_`, `ash_`, `host_`, `pglens_`), excluding tests and the generated client (`web/src/api/generated.ts`). A name that is absent here can still be shown, because several views consume API fields rather than metric names (ASH, statements, relations, bloat, replication lag, slots, settings drift, lock tree, plans, findings). Those views are listed per endpoint in section 7.

Named in the UI:
- Instance overview (`features/instance/OverviewSection.tsx`): `pg_blks_hit_total`, `pg_blks_read_total`, `pg_checkpoints_requested_total`, `pg_checkpoints_timed_total`, `pg_connections_limit`, `pg_connections_used`, `pg_deadlocks_total`, `pg_temp_bytes_total`, `pg_wal_lsn_bytes`, `pg_xact_commit_total`.
- Activity (`features/locks/ActivitySection.tsx`, `lib/activity.ts`): `pg_backends`, `pg_connections_by_application`, `pg_connections_by_database`, `pg_connections_used_ratio`, `pg_max_datfrozenxid_age`, `pg_max_idle_in_transaction_seconds`, `pg_max_state_age_seconds`, `pg_max_xact_age_seconds`, `pg_oldest_prepared_xact_seconds`, `pg_prepared_xacts`.
- Host (`features/instance/HostSection.tsx`): `host_cpu_count`, `host_cpu_used_ratio`, `host_disk_free_bytes`, `host_disk_free_ratio`, `host_disk_total_bytes`, `host_load1`, `host_load5`, `host_load15`, `host_mem_available_bytes`, `host_mem_total_bytes`, `host_swap_total_bytes`, `host_swap_used_bytes`.
- Other: `pg_version` (`settings/InventoryTables.tsx`), `instance_unreachable` (`alerts/AlertRow.tsx`), `cluster_id_changed` (`cluster/EventTimeline.tsx`).

Collected, with no literal reference in `web/src`:
- `pg_io_*` (io check): no UI name found. Not shown by name.
- `pg_wal_<column>` except `pg_wal_lsn_bytes`: not shown by name.
- Archiver: `pg_archive_mode_enabled`, `pg_archiver_*`, `pg_basebackup_*`: not shown by name (archive state reaches the UI only through advisor findings, if at all).
- Progress: `pg_vacuum_*`, `pg_analyze_*`, `pg_vacuum_jobs_running`, `pg_analyze_jobs_running`: not shown by name.
- `replication_receiver_*`: not shown by name.
- `pg_settings_pending_restart`: not shown by name.
- `pg_backends_waiting_ratio`: not shown by name.
- Recovery conflicts `pg_conflicts_total`: not shown by name; no advisor or alert uses it.
- `pg_numbackends`, `pg_xact_rollback_total`, `pg_xact_rollback_ratio`, `pg_blks_hit_ratio`: not shown by name (rollback ratio feeds only the advisor rule `txn.high_rollback_ratio`).
- Bgwriter and buffers: `pg_bgwriter_*`, `pg_buffers_alloc_total`, `pg_checkpoint_*_time_ms_total`: not shown by name.
- Lock metrics `pg_blocked_sessions`, `pg_blocking_sessions`, `pg_max_block_age_seconds`, `pg_lock_waits`: not shown by name; the blocking tree comes from `/api/v1/locks`.
- Instance identity `pg_up`, `pg_in_recovery`, `pg_version_num`, `pg_uptime_seconds`, `pg_addr`, `pg_port`: not shown by name.
- Statements, tables, indexes, bloat, ASH and replication series: shown through their endpoints (section 7), not by name.
