import type { CaptionEntry } from '../captions'

/** Captions of the instance route group (D30); rules in the plan, phase 17. */
export const instanceCaptions = {
  'instance.database-scope': {
    what: 'The monitored database whose per-database figures the page shows, and the databases of the instance that the agent does not monitor.',
    source:
      'The instance inventory the agent sends (databases: datname, monitored, skip_reason), limited by the agent databases.max budget and include filters.',
    unit: 'databases',
    window: 'Latest inventory the agent sent.',
    howToRead:
      'The default is the most active monitored database when activity is available, otherwise the first one. A database that is not monitored is a blind spot in these metrics.',
  },

  'overview.section': {
    what: 'Summary tiles and charts for one instance: workload, memory and I/O, WAL and checkpoints, queries, tables and indexes.',
    source:
      'Metrics stored by the database_stats, activity, wal, checkpointer, statements, tables and indexes checks; per-database figures use the selected database.',
    window: 'Selected time range; each chart states its query step.',
    howToRead:
      'Transactions, cache hit ratio, temporary files and bytes, and deadlocks are for the selected database; connections, WAL and checkpoints are instance-wide; query, table and index charts total what the agent tracks in every database. Null buckets remain gaps; dashed lines mark data gaps or counter resets.',
  },
  'overview.tx-rate': {
    what: 'Transactions per second in the selected database, counting both commits and rollbacks.',
    source:
      'pg_stat_database.xact_commit plus xact_rollback (pg_xact_commit_total and pg_xact_rollback_total), stored as per-second rates; the tile adds the newest bucket of each.',
    unit: 'per second',
    window: 'Newest bucket of the selected time range.',
    howToRead:
      'The figure is commits plus rollbacks, so it is at least the commit rate. A dash means no sample arrived recently enough.',
  },
  'overview.wal-in-range': {
    what: 'WAL written by the whole instance during the selected range, estimated from the growth of the write-ahead log position.',
    source:
      'pg_current_wal_lsn() (pg_last_wal_replay_lsn() on a standby) as pg_wal_lsn_bytes; the tile adds each bucket rate times the time since the previous measured bucket.',
    unit: 'bytes',
    window: 'Selected time range.',
    howToRead: 'Estimated: a stretch without samples is filled from the next measured bucket.',
  },
  'overview.checkpoints-in-range': {
    what: 'Checkpoints started during the selected range, timed and requested together, estimated from their per-second rates.',
    source:
      'pg_stat_checkpointer num_timed plus num_requested (pg_stat_bgwriter checkpoints_timed plus checkpoints_req before PostgreSQL 17), stored as pg_checkpoints_timed_total and pg_checkpoints_requested_total.',
    unit: 'checkpoints',
    window: 'Selected time range.',
    howToRead: 'Estimated by adding each bucket rate times the time since the previous bucket.',
  },
  'overview.temp-bytes-in-range': {
    what: 'Bytes written to temporary files by queries of the selected database during the selected range, estimated from per-second rates.',
    source:
      'pg_stat_database.temp_bytes (pg_temp_bytes_total), stored as a per-second rate and added up over the range.',
    unit: 'bytes',
    window: 'Selected time range.',
    howToRead: 'Zero means no query wrote a temporary file in the range.',
  },
  'overview.temp-files-in-range': {
    what: 'Temporary files created by queries of the selected database during the selected range, estimated from per-second rates.',
    source:
      'pg_stat_database.temp_files (pg_temp_files_total), stored as a per-second rate and added up over the range.',
    unit: 'files',
    window: 'Selected time range.',
    howToRead: 'Zero means no query created a temporary file in the range.',
  },
  'overview.deadlocks-in-range': {
    what: 'Deadlocks detected in the selected database during the selected range, estimated from per-second rates.',
    source:
      'pg_stat_database.deadlocks (pg_deadlocks_total), stored as a per-second rate and added up over the range.',
    unit: 'deadlocks',
    window: 'Selected time range.',
    howToRead: 'Zero means PostgreSQL detected no deadlock in the range.',
  },

  'overview.chart-transactions': {
    what: 'Committed and rolled-back transactions per second in the selected database, as two lines.',
    source:
      'pg_stat_database.xact_commit and xact_rollback (pg_xact_commit_total, pg_xact_rollback_total), counter deltas as per-second rates.',
    unit: 'per second',
    window: 'Selected time range, one point per query step; sampled every 30 s.',
    howToRead:
      'A flat line at zero means no transactions; a gap means no sample was stored; a dashed line marks a gap or a counter reset.',
  },
  'overview.chart-connections': {
    what: 'Client connections open and the configured maximum, for the whole instance.',
    source:
      'Client backends counted from pg_stat_activity (pg_connections_used) and the max_connections setting (pg_connections_limit).',
    unit: 'connections',
    window: 'Selected time range; sampled every 10 s.',
    thresholds: 'The advisor rule conn.saturation warns above 80 % of max_connections.',
  },
  'overview.chart-deadlocks-conflicts': {
    what: 'Deadlocks and recovery conflicts per second in the selected database.',
    source:
      'pg_stat_database.deadlocks and conflicts (pg_deadlocks_total, pg_conflicts_total), counter deltas as per-second rates.',
    unit: 'per second',
    window: 'Selected time range; sampled every 30 s.',
    howToRead:
      'Recovery conflicts are counted on a standby, so that line stays at zero on a primary.',
  },
  'overview.chart-cache-hit-ratio': {
    what: "Share of page reads found in PostgreSQL's own memory cache (shared buffers) rather than requested from the operating system, per query step.",
    source:
      'pg_stat_database blks_hit / (blks_hit + blks_read), from the pg_blks_hit_total and pg_blks_read_total rates of each step.',
    unit: '%',
    window: 'Selected time range; sampled every 30 s.',
    howToRead:
      'Reads that miss shared buffers may still come from the operating system cache, so a lower ratio is not always disk I/O.',
  },
  'overview.chart-blocks': {
    what: 'Blocks read from outside shared buffers and blocks found in shared buffers per second, in the selected database.',
    source:
      'pg_stat_database.blks_read and blks_hit (pg_blks_read_total, pg_blks_hit_total), counter deltas as per-second rates.',
    unit: 'blocks per second',
    window: 'Selected time range; sampled every 30 s.',
    howToRead:
      'A read may be served by the operating system cache, so it is not always a disk read.',
  },
  'overview.chart-temp': {
    what: 'Temporary files created and bytes written to them per second in the selected database; bytes use a second axis.',
    source:
      'pg_stat_database.temp_files and temp_bytes (pg_temp_files_total, pg_temp_bytes_total), counter deltas as per-second rates.',
    unit: 'files per second; bytes per second on the second axis',
    window: 'Selected time range; sampled every 30 s.',
  },
  'overview.chart-wal': {
    what: 'Write-ahead log (WAL, the journal PostgreSQL writes before changing data files): bytes, records and full-page images the whole instance writes per second.',
    source:
      'pg_stat_wal wal_bytes, wal_records and wal_fpi (pg_wal_wal_bytes, pg_wal_wal_records, pg_wal_wal_fpi), counter deltas; only the columns this server has are collected.',
    unit: 'bytes per second; records and images per second on the second axis',
    window: 'Selected time range; sampled every 15 s.',
    howToRead:
      'A full-page image is a whole page copied into the WAL the first time it changes after a checkpoint. A line is missing when the PostgreSQL version has no such pg_stat_wal column.',
  },
  'overview.chart-checkpoints': {
    what: 'Checkpoints, the moments PostgreSQL writes all changed pages to the data files, started in each step: timed by the clock, or requested earlier.',
    source:
      'pg_stat_checkpointer num_timed and num_requested (pg_stat_bgwriter before PostgreSQL 17), as pg_checkpoints_timed_total and pg_checkpoints_requested_total.',
    unit: 'checkpoints per step',
    window: 'Selected time range; sampled every 30 s.',
    howToRead:
      'Each point is the count in that step (rate times step length), so a rare event reads as 1.',
  },
  'overview.chart-checkpoint-time': {
    what: 'Time spent writing and syncing files during checkpoints, added up in each query step.',
    source:
      'pg_stat_checkpointer write_time and sync_time (pg_stat_bgwriter before PostgreSQL 17), as pg_checkpoint_write_time_ms_total and pg_checkpoint_sync_time_ms_total.',
    unit: 'ms per step',
    window: 'Selected time range; sampled every 30 s.',
  },
  'overview.chart-calls': {
    what: 'Statement executions per second, totalled over the statements pg_stat_statements tracks in every database.',
    source:
      'pg_stat_statements.calls of the top statements the agent keeps at each scrape (calls_rate), summed per scrape, averaged per step and added over databases.',
    unit: 'calls per second',
    window: 'Selected time range; scraped every 60 s.',
    howToRead:
      'Statements outside the top list the agent keeps are not counted, so the line can sit below the server total.',
  },
  'overview.chart-exec-time': {
    what: 'Milliseconds of execution time used per second by the tracked statements of every database.',
    source:
      'pg_stat_statements total execution time of the top statements the agent keeps, as a per-second rate (exec_time_rate_ms).',
    unit: 'ms per second',
    window: 'Selected time range; scraped every 60 s.',
    howToRead:
      'Statements outside the top list the agent keeps are not counted, so the line can sit below the server total.',
  },
  'overview.chart-rows': {
    what: 'Rows returned or affected per second by the tracked statements of every database.',
    source:
      'pg_stat_statements.rows of the top statements the agent keeps, as a per-second rate (rows_rate).',
    unit: 'rows per second',
    window: 'Selected time range; scraped every 60 s.',
    howToRead:
      'Statements outside the top list the agent keeps are not counted, so the line can sit below the server total.',
  },
  'overview.chart-statement-reads': {
    what: 'Shared blocks read per second by the tracked statements of every database, blocks that were not in shared buffers.',
    source:
      'pg_stat_statements.shared_blks_read of the top statements the agent keeps, as a per-second rate (shared_blks_read_rate).',
    unit: 'blocks per second',
    window: 'Selected time range; scraped every 60 s.',
    howToRead:
      'Statements outside the top list the agent keeps are not counted, so the line can sit below the server total.',
  },
  'overview.chart-statement-temp': {
    what: 'Temporary blocks written per second by the tracked statements of every database.',
    source:
      'pg_stat_statements.temp_blks_written of the top statements the agent keeps, as a per-second rate (temp_blks_written_rate).',
    unit: 'blocks per second',
    window: 'Selected time range; scraped every 60 s.',
    howToRead:
      'Statements outside the top list the agent keeps are not counted, so the line can sit below the server total.',
  },
  'overview.chart-scans': {
    what: 'Sequential scans and index scans started per second on the tracked tables of every database.',
    source:
      'pg_stat_user_tables.seq_scan and idx_scan of the tables the agent selects by dead tuples, size and transaction age, as per-second rates.',
    unit: 'scans per second',
    window: 'Selected time range; sampled every 5 minutes.',
    howToRead: 'Tables the agent does not track are not counted.',
  },
  'overview.chart-tuple-churn': {
    what: 'Rows inserted, updated, HOT updated and deleted per second on the tracked tables of every database.',
    source:
      'pg_stat_user_tables.n_tup_ins, n_tup_upd, n_tup_hot_upd and n_tup_del of the tables the agent tracks, as per-second rates.',
    unit: 'rows per second',
    window: 'Selected time range; sampled every 5 minutes.',
    howToRead:
      'HOT updates (the new row version stays on the same page, with no index change) are also counted in Updated.',
  },
  'overview.chart-dead-tuples': {
    what: 'Estimated dead rows (old row versions left by updates and deletes that vacuum has not yet removed), added up over the tracked tables of every database.',
    source: 'pg_stat_user_tables.n_dead_tup of the tables the agent tracks, summed per sample.',
    unit: 'rows',
    window: 'Selected time range; sampled every 5 minutes.',
    howToRead: 'A level, not a rate: a falling line means fewer dead tuples than before.',
  },
  'overview.chart-index-scans': {
    what: 'Index scans started per second on the tracked indexes of every database.',
    source: 'pg_stat_user_indexes.idx_scan of the indexes the agent tracks, as a per-second rate.',
    unit: 'scans per second',
    window: 'Selected time range; sampled every 5 minutes.',
    howToRead: 'Indexes the agent does not track are not counted.',
  },
  'overview.chart-index-size': {
    what: 'Size of the tracked indexes of every database, added together.',
    source:
      'pg_relation_size() of each index the agent tracks (index_stats check), summed per sample.',
    unit: 'bytes',
    window: 'Selected time range; sampled every 5 minutes.',
    howToRead: 'A level, not a rate; indexes the agent does not track are not counted.',
  },

  'host.section': {
    what: 'Readings of the machine the agent runs on: CPU, memory, swap, load average and the data directory filesystem.',
    source:
      'Operating system readings taken by the agent host collector and sent as host_* metrics; a container CPU or memory limit replaces the host figure when the agent finds one.',
    window: 'Tiles show the newest reading of the last day; charts use the selected time range.',
    howToRead:
      'Per-device I/O rates, disk latency and network metrics are not collected. Host collection is local to the agent, so a remote server has no host data.',
  },
  'host.cpu-used': {
    what: 'Share of CPU time in use since the previous sample, of the container CPU limit when one is set, otherwise of the whole host.',
    source:
      'host_cpu_used_ratio: operating system CPU times between two samples, or the cgroup CPU usage against its quota.',
    unit: '%',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead:
      'The first sample after an agent start has no value. A memory limit alone does not change it: the ratio then stays host-wide.',
  },
  'host.cpu-count': {
    what: 'Logical CPUs available to the agent: the container CPU quota rounded up when one is set, otherwise the host count.',
    source: 'host_cpu_count: logical CPU count from the operating system, or the cgroup CPU quota.',
    unit: 'CPUs',
    window: 'Newest reading of the last day; sampled every 30 s.',
  },
  'host.mem-total': {
    what: 'Total memory of the host, or the container memory limit when one is set.',
    source: 'host_mem_total_bytes: operating system memory total, or the cgroup memory limit.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
  },
  'host.mem-available': {
    what: 'Memory available to new work without swapping: the operating system figure, or in a container the limit minus its current use.',
    source:
      'host_mem_available_bytes: operating system available memory; in a container the limit minus usage, counting inactive file cache as available.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
  },
  'host.swap-total': {
    what: 'Total swap space configured on the host.',
    source: 'host_swap_total_bytes: operating system swap total.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead: 'Zero means no swap is configured.',
  },
  'host.swap-used': {
    what: 'Swap space in use on the host.',
    source: 'host_swap_used_bytes: operating system swap used.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
  },
  'host.load-average': {
    what: 'Linux load average over the last 1, 5 or 15 minutes: processes ready to run plus those in uninterruptible wait, usually disk I/O, as the tile label says.',
    source: 'host_load1, host_load5 and host_load15: the operating system load average.',
    unit: 'processes',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead:
      'Compare it with the CPU count only when no container CPU limit is set: the load average is host-wide, while the CPU count is then the container quota rounded up.',
  },
  'host.disk-total': {
    what: 'Total size of the filesystem that holds the PostgreSQL data directory.',
    source:
      'host_disk_total_bytes: filesystem usage of the data directory path, which the agent must see.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead:
      'Not visible to the agent means the data directory is not mounted where the agent reads it.',
  },
  'host.disk-free': {
    what: 'Free space on the filesystem that holds the PostgreSQL data directory.',
    source: 'host_disk_free_bytes: filesystem usage of the data directory path.',
    unit: 'bytes',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead:
      'Not visible to the agent means the data directory is not mounted where the agent reads it.',
  },
  'host.disk-free-ratio': {
    what: 'Free space as a share of the filesystem that holds the PostgreSQL data directory.',
    source:
      'host_disk_free_ratio: free bytes divided by total bytes of the data directory filesystem.',
    unit: '%',
    window: 'Newest reading of the last day; sampled every 30 s.',
    howToRead:
      'Not visible to the agent means the data directory is not mounted where the agent reads it.',
  },
  'host.chart-cpu': {
    what: 'Share of CPU time in use over the selected range, of the container CPU limit when one is set, otherwise of the whole host.',
    source:
      'host_cpu_used_ratio: operating system CPU times between two samples, or the cgroup CPU usage against its quota, shown as a percentage.',
    unit: '%',
    window: 'Selected time range; sampled every 30 s.',
    howToRead: 'The axis runs from 0 to 100 %.',
  },
  'host.chart-load': {
    what: 'Load average over 1, 5 and 15 minutes, with a dashed line at the CPU count.',
    source:
      'host_load1, host_load5, host_load15 and host_cpu_count: the operating system load average.',
    unit: 'processes',
    window: 'Selected time range; sampled every 30 s.',
    howToRead:
      'A load above the CPU count line means more processes ready to run or waiting on I/O than CPUs; under a container CPU limit the line is the quota while the load is host-wide.',
  },
  'host.chart-memory': {
    what: 'Memory in use and total memory of the host, or of the container when it has a memory limit.',
    source:
      'host_mem_total_bytes minus host_mem_available_bytes for Used, and host_mem_total_bytes for Total.',
    unit: 'bytes',
    window: 'Selected time range; sampled every 30 s.',
    howToRead: 'Used is derived: total minus available, not the operating system used counter.',
  },
  'host.chart-swap': {
    what: 'Swap space in use and total swap space of the host.',
    source: 'host_swap_used_bytes and host_swap_total_bytes: operating system swap.',
    unit: 'bytes',
    window: 'Selected time range; sampled every 30 s.',
    howToRead: 'With no swap configured the chart is replaced by a note.',
  },
  'host.chart-filesystem': {
    what: 'Used bytes and free share of the filesystem that holds the PostgreSQL data directory.',
    source:
      'host_disk_total_bytes minus host_disk_free_bytes for Used, and host_disk_free_ratio for Free, shown as a percentage on the second axis.',
    unit: 'bytes; free share in % on the second axis',
    window: 'Selected time range; sampled every 30 s.',
    howToRead: 'The chart is replaced by a note when the agent cannot see the data directory.',
  },

  'catalog.section': {
    what: 'Schema objects of each database: triggers, sequences, partitions, functions, security definer functions, unindexed foreign keys, extensions and spatial columns.',
    source:
      'Catalog checks of the agent: trigger_inventory, sequences, partitions, function_stats, function_security, fk_indexes, extensions and postgis_columns.',
    window: 'Latest result of each check; they run every 5 minutes to every hour.',
    howToRead:
      'Each tab lists one kind of object; a count badge on a tab is the number of items that need a look.',
  },
  'catalog.triggers': {
    what: 'User triggers on user tables of each database, plus internal constraint triggers only when they are disabled.',
    source:
      'pg_trigger joined to pg_class, pg_proc and pg_constraint (trigger_inventory check); up to 1000 per database, disabled ones first.',
    unit: 'triggers',
    window: 'Latest check result; the check runs every 5 minutes.',
    howToRead:
      'A disabled internal trigger means its foreign key or constraint is no longer enforced. A trigger a partition inherits is listed only when its state differs from its parent.',
  },
  'catalog.sequences': {
    what: 'Sequences of each database with the column each one feeds, the last value handed out and how much of its range is used.',
    source:
      'pg_sequence and pg_class catalogs with the last value of each sequence and the integer type of the column it feeds (sequences check); up to 500 per database, closest to the limit first.',
    unit: 'values; used share in %',
    window: 'Latest check result; the check runs every 10 minutes.',
    howToRead:
      'Used is a badge once a sequence that can run out reaches the warning share. A cycling sequence counts as at risk only when its column bounds it.',
    thresholds: 'Warning from 75 % used and critical from 90 % used, as in the advisor.',
  },
  'catalog.partitions': {
    what: 'Partitioned tables of each database with the whole partition tree rolled up: partitions, estimated size and rows, default partition and coverage.',
    source:
      'Partition trees walked through pg_inherits, sizes and rows estimated from relpages and reltuples as of the last vacuum or analyze (partitions check); up to 200 trees, largest first.',
    unit: 'partitions; bytes and rows are estimated',
    window: 'Latest check result; the check runs every 10 minutes.',
    howToRead:
      'Partitions reach is the end of the last date range covered by a partition: it is marked ends soon within 7 days and ended once it has passed.',
    thresholds:
      'Needs attention: the default partition holds rows, a partitioned index is invalid, or coverage ends within 7 days (or ended within the last 30 days).',
  },
  'catalog.functions': {
    what: 'Functions that track_functions counts: calls per second, time in their own body, total time and mean time per call.',
    source:
      'pg_stat_user_functions counters (function_stats check) turned into per-second rates; the 200 busiest by own time and calls are listed.',
    unit: 'calls per second; ms per second; ms per call',
    window: 'Rates averaged over the last hour; the check runs every 5 minutes.',
    howToRead:
      'Own time leaves out the functions a function calls, total time includes them. With track_functions set to none no function is counted.',
  },
  'catalog.security-definer': {
    what: 'Functions and procedures declared security definer, which run with their owner privileges, with owner, pinned search_path and who may execute them.',
    source:
      'pg_proc rows with prosecdef, the owner, the search_path in proconfig and the expanded access list, outside extensions and system schemas (function_security check); up to 200 per database, unpinned first.',
    unit: 'functions',
    window: 'Latest check result; the check runs every 15 minutes.',
    howToRead:
      'A function without a pinned search_path resolves names through the search_path of its caller.',
    thresholds:
      'Marked critical when the function has no pinned search_path, its owner is a superuser and every role may execute it.',
  },
  'catalog.foreign-keys': {
    what: 'Foreign keys whose referencing columns no valid index leads with, so changing a referenced row scans the whole referencing table.',
    source:
      'pg_constraint foreign keys checked against pg_index for a valid, non-partial btree or hash index that leads with their columns (fk_indexes check); up to 200 per database, largest tables first.',
    unit: 'foreign keys; table size in bytes (estimated)',
    window: 'Latest check result; the check runs every 15 minutes.',
    howToRead:
      'Table size is estimated from relpages as of the last vacuum or analyze. A foreign key a partition inherits is not listed on its own.',
  },
  'catalog.extensions': {
    what: 'Extensions installed in each database, their installed version and the default version the installed packages carry.',
    source:
      'pg_extension joined to pg_available_extensions() (extensions check); up to 200 per database.',
    unit: 'extensions',
    window: 'Latest check result; the check runs every hour.',
    howToRead:
      'Update shows available when the packages default version is newer than the installed one. pglens never updates an installed extension.',
  },
  'catalog.spatial-columns': {
    what: 'Geometry and geography columns of each database with their type, coordinate system and the index that serves spatial searches.',
    source:
      'pg_attribute columns typed in the postgis schema with the access method of a valid, non-partial GiST, space-partitioned GiST or BRIN index leading with them (postgis_columns check); up to 500 per database, unindexed first.',
    unit: 'columns; rows are estimated',
    window: 'Latest check result; the check runs every hour.',
    howToRead:
      'Missing means no such valid index leads with the column. A coordinate system marked unknown or undefined names no registered one.',
  },

  'internals.section': {
    what: 'Server internals the agent reports: the WAL kept in pg_wal, SLRU caches, temporary files and, on a standby, WAL replay.',
    source:
      'Checks wal_directory, database_size, slru, database_stats and recovery, read from the stored metrics of the instance.',
    window: 'Rates are averaged over the last hour.',
  },
  'internals.wal-directory': {
    what: 'Space the pg_wal directory uses on disk: WAL segments, partial segments and history files.',
    source:
      'Total size and file count of pg_ls_waldir() (wal_directory check, pg_wal_dir_bytes and pg_wal_dir_files); it needs the pg_monitor role or an explicit grant.',
    unit: 'bytes',
    window: 'Newest value of the last hour; the check runs every minute.',
    howToRead:
      'It can exceed max_wal_size when WAL is held back; replication slots, archiving and wal_keep_size are the places to look.',
  },
  'internals.wal-replay': {
    what: 'Whether this standby is applying the WAL it receives or has paused replay.',
    source:
      'pg_get_wal_replay_pause_state() read by the recovery check (pg_recovery_replay_paused); shown only on a server in recovery.',
    window: 'Newest value of the last hour; the check runs every 30 s.',
    howToRead: 'Paused means received WAL is not being applied until replay is resumed.',
  },
  'internals.database-sizes': {
    what: 'Size on disk of each database and how much it grew over the last 24 hours and the last 7 days.',
    source:
      'pg_database_size() of each database (database_size check, pg_database_size_bytes); growth compares with the size measured closest to 24 hours and 7 days earlier, within two hours.',
    unit: 'bytes',
    window: 'Latest size measured in the last 2 hours; the check runs every 15 minutes.',
    howToRead: 'A dash in a growth column means no size was measured near that time.',
  },
  'internals.slru': {
    what: 'Hit ratio and activity of each SLRU cache, the small memory caches behind transaction status and row locks.',
    source:
      'pg_stat_slru counters (slru check) as per-second rates averaged over the last hour; hit ratio is hits divided by hits plus reads.',
    unit: '% hit ratio; blocks and events per second',
    window: 'Last hour; the check runs every minute.',
    howToRead:
      'A read that misses a cache goes to disk under the cache lock and stalls every session that needs it: watch the subtransaction and multixact caches. The caches hold transaction status, row-lock groups (multixact), subtransactions and serializable isolation.',
  },
  'internals.temp-files': {
    what: 'Databases whose queries spilled to temporary files: files created per minute and bytes written per second.',
    source:
      'pg_stat_database.temp_files and temp_bytes (database_stats check) as rates averaged over the last hour; databases with an average of zero are left out.',
    unit: 'files per minute; bytes per second',
    window: 'Last hour; the check runs every 30 s.',
  },
  'internals.recovery-conflicts': {
    what: 'Queries cancelled on this standby because WAL replay conflicted with them, by database and reason.',
    source:
      'pg_stat_database_conflicts (recovery check, pg_recovery_conflicts_total) as a rate averaged over the last hour.',
    unit: 'queries per minute',
    window: 'Last hour; the check runs every 30 s.',
    howToRead: 'An empty list means replay cancelled no query in the last hour.',
  },

  'logical.section': {
    what: 'Logical replication on this instance: the subscriptions it applies and the logical slots it decodes for.',
    source:
      'Logical_replication check: pg_subscription, pg_stat_subscription, pg_stat_subscription_stats and pg_stat_replication_slots; connection strings are never read.',
    window: 'Subscriptions: latest check result; slots: rates averaged over the last hour.',
  },
  'logical.subscriptions': {
    what: 'Logical replication subscriptions this instance applies: state, publications, slot, last message from the publisher, error and conflict counts.',
    source:
      'pg_subscription (never the connection string), pg_stat_subscription and pg_stat_subscription_stats (logical_replication check), stored as subscription facts.',
    unit: 'subscriptions; errors and conflicts counted since the statistics reset',
    window: 'Latest check result; the check runs every minute.',
    howToRead:
      'State is disabled, not running (no apply worker), silent (nothing heard from the publisher for 5 minutes or more) or running. Conflicts exist from PostgreSQL 18.',
    thresholds: 'Silent means the last message is 5 minutes old or older, as in the advisor.',
  },
  'logical.slots': {
    what: 'Decoding of each logical slot on this instance: bytes decoded, transactions, bytes spilled to disk and bytes streamed per second.',
    source:
      'pg_stat_replication_slots (logical_replication check), counter deltas as per-second rates averaged over the last hour.',
    unit: 'bytes per second; transactions per second',
    window: 'Last hour; the check runs every minute.',
    thresholds: 'A spill rate of 1 MiB per second or more is marked, as in the advisor.',
  },
  'instance.settings': {
    what: 'PostgreSQL settings as last read from the server: five durability settings shown first, then the settings changed in the selected range.',
    source:
      'pg_settings read by the settings check (name, setting, unit, source, context, pending_restart), stored as setting facts with the time of their last change.',
    unit: 'settings',
    window:
      'Durability settings: the latest scrape; the change table: settings whose value changed since the chosen date, by default the start of the selected range.',
    howToRead:
      'A durability setting that weakens crash safety carries a critical or warning badge; its ⓘ says what the value means.',
  },
  'instance.settings-changes': {
    what: 'Settings whose value changed since the chosen date, with where the value was set, when a change takes effect and whether a restart is pending.',
    source:
      'pg_settings facts of the instance (settings check): name, value, unit, source, context, pending_restart and the time the value last changed.',
    window:
      'Changes since the date in Changed since; the value shown is the latest scrape. Use selected range resets the date to the start of the range.',
    howToRead:
      'Pending restart means the configuration holds a new value that only a restart puts in force. Search and filters change no setting.',
  },

  'relations.section': {
    what: 'Tables, indexes and estimated bloat of every monitored database, as last reported by the agent; one tab per list.',
    source:
      'Agent checks table_stats, index_stats and bloat_estimate, read from the relation metrics stored for the instance.',
    unit: 'rows, scans per second and bytes',
    window:
      'Tables and indexes: reports of the last 15 minutes. Bloat estimates: the last 13 hours; exact measurements: the last 7 days.',
    howToRead:
      'Each list is limited by the agent budget of its check (checks.table_stats.top_n, checks.index_stats.top_n, checks.bloat_estimate.top_n), so relations beyond that budget are not reported.',
  },
  'relations.tables': {
    what: 'Each table of the monitored databases with its live and dead rows, how often it is read in full, and when vacuum and analyze last ran.',
    source:
      'pg_stat_user_tables (table_stats check): n_live_tup, n_dead_tup, seq_scan and idx_scan as rates, last vacuum and analyze times; size from pg_total_relation_size.',
    unit: 'rows; scans per second; bytes',
    window: 'Reports of the last 15 minutes; the check runs every 5 minutes.',
    howToRead:
      'Dead ratio is dead rows divided by live rows. Scan columns are rates over the last scrape interval, not totals. The agent reports only the top tables of each database (checks.table_stats.top_n, 50 by default).',
  },
  'relations.indexes': {
    what: 'Each index of the monitored databases with how often it is used, when it was last used, its size, and whether it is unique, primary and valid.',
    source:
      'pg_stat_user_indexes and pg_index (index_stats check): idx_scan as a rate, last_idx_scan (PostgreSQL 16 and later), pg_relation_size, indisunique, indisprimary and indisvalid.',
    unit: 'scans per second; bytes',
    window: 'Reports of the last 15 minutes; the check runs every 5 minutes.',
    howToRead:
      'The Relation column names the table the index belongs to. Bloat estimates come from another check with a longer window, so they can list indexes while this tab is empty.',
  },
  'relations.table-bloat': {
    what: 'Estimated wasted space inside each table, held by deleted or old row versions, that a rebuild would give back.',
    source:
      'bloat_estimate check: real size from pg_relation_size against an expected size worked out from row counts and column statistics (pg_stats); pgstattuple rows are exact.',
    unit: 'bytes; bloat ratio in %',
    window:
      'Newest estimate of the last 13 hours, refreshed every 6 hours; an exact measurement is kept for 7 days.',
    howToRead:
      'Estimated, not measured. Tables without column statistics (never analysed) and tables the monitoring role cannot read in full are not estimated; Measure exactly runs pgstattuple on one table.',
  },
  'relations.index-bloat': {
    what: 'Estimated wasted space inside each btree index, held by deleted or old entries, that rebuilding the index would give back.',
    source:
      'bloat_estimate check: real size from pg_relation_size against an expected size worked out from row counts and column statistics (pg_stats).',
    unit: 'bytes; bloat ratio in %',
    window: 'Newest estimate of the last 13 hours, refreshed every 6 hours.',
    howToRead:
      'Estimated, not measured. Only valid btree indexes that are not partial, have no expression and sit on columns with statistics are estimated.',
  },
} as const satisfies Record<string, CaptionEntry>
