/** One plain sentence for every advisor rule and alert rule id. */
const ADVISOR_RULE_TEXT = {
  'archive.disabled':
    'WAL archiving is turned off, so backups cannot be restored to a point in time.',
  'archive.failing': 'WAL archiving is failing: finished WAL files are not being copied.',
  'archive.stalled':
    'No WAL file has been archived for a long time although the server writes WAL.',
  'backup.no_basebackup_seen': 'No base backup has been seen for this cluster.',
  'backup.no_strategy':
    'No backup strategy is visible: WAL is not archived and no base backup was seen.',
  'bloat.estimate_blind':
    'The wasted-space estimate cannot be computed for some tables or indexes.',
  'config.checkpoints_too_frequent':
    'More than 20% of the checkpoints are requested before the scheduled time.',
  'config.drift': 'The same setting has different values on instances of this cluster.',
  'config.effective_cache_size_mismatch':
    'effective_cache_size is far from the memory the server has.',
  'config.fsync_off': 'fsync is off: a crash can corrupt data.',
  'config.full_page_writes_off':
    'full_page_writes is off: a crash can leave torn pages and corrupt data.',
  'config.maintenance_work_mem_low':
    'maintenance_work_mem is below 256 MiB on a host with a lot of memory.',
  'config.max_connections_high':
    'max_connections is much higher than the connections actually in use.',
  'config.shared_buffers_high': 'shared_buffers is more than 40% of the usable memory.',
  'config.shared_buffers_low': 'shared_buffers is small for the memory the server has.',
  'config.track_io_timing_off': 'track_io_timing is off, so read and write times are not measured.',
  'config.wal_keep_size_low':
    'wal_keep_size may be too small to keep WAL for a standby that falls behind.',
  'config.work_mem_low': 'work_mem is small and statements write temporary files.',
  'config.work_mem_oversized': 'work_mem times max_connections is a large share of the memory.',
  'conn.idle_share_high': 'A large share of the connections is idle.',
  'conn.saturation': 'Most of the connections allowed by max_connections are in use.',
  'extension.upgrade_available': 'An installed extension has a newer version available.',
  'function.secdef_no_search_path': 'A SECURITY DEFINER function has no fixed search_path.',
  'function.untracked': 'Function calls are not tracked, so function statistics are empty.',
  'index.bloat_high': 'An index holds a lot of space that a rebuild would give back.',
  'index.divergence': 'The same table has different indexes on instances of this cluster.',
  'index.duplicate': 'Two or more indexes have the same definition.',
  'index.fk_unindexed': 'A foreign key has no index on its columns.',
  'index.invalid': 'An index is marked invalid, so queries do not use it.',
  'index.redundant_prefix': 'An index repeats the leading columns of another index.',
  'index.unused': 'An index has had no scans in the observed history.',
  'lock.long_wait': 'A session has waited a long time for a lock.',
  'logical_slot.spill_high': 'A logical slot writes a lot of decoded changes to disk.',
  'partition.default_has_rows': 'The default partition holds rows that match no other partition.',
  'partition.future_missing': 'A partitioned table has no partition ready for upcoming data.',
  'partition.index_invalid': 'An index on a partitioned table is invalid.',
  'partition.parent_not_analyzed': 'A partitioned table has never been analyzed.',
  'postgis.spatial_unindexed': 'A PostGIS geometry column has no spatial index.',
  'postgis.srid_mixed': 'Geometry columns use more than one coordinate system.',
  'postgis.srid_unknown': 'A geometry column has no declared coordinate system.',
  'postgis.upgrade_available': 'The PostGIS extension is older than its installed library.',
  'query.cache_miss_high':
    'A frequent statement reads most of its pages from disk, not from memory.',
  'query.regression': 'A statement is slower than its own history of the last seven days.',
  'query.slow_mean': 'A statement has a high mean execution time.',
  'query.temp_bytes_high': 'A statement writes a lot of temporary files.',
  'query.total_time_share': 'One statement takes a large share of the total execution time.',
  'security.monitoring_role_can_write': 'The monitoring role can write data.',
  'security.monitoring_role_read_only_off':
    'The monitoring role does not default to read-only transactions.',
  'sequence.column_narrower': 'A sequence can count higher than the column that stores its values.',
  'sequence.exhaustion_risk': 'A sequence is close to its maximum value.',
  'sequence.value_hidden': 'pglens cannot read the current value of a sequence.',
  'standby.recovery_conflicts': 'Queries on a standby are cancelled by WAL replay.',
  'standby.replay_paused': 'WAL replay is paused on a standby.',
  'subscription.disabled': 'A logical replication subscription is disabled.',
  'subscription.errors': 'A logical replication subscription reports errors.',
  'subscription.not_receiving': 'An enabled subscription is not receiving changes.',
  'table.autoanalyze_stale':
    'More than 20% of the rows of a table changed since it was last analyzed.',
  'table.bloat_high': 'A table holds a lot of space that a rebuild would give back.',
  'table.dead_tuples_high': 'A table has many dead rows that vacuum has not yet cleared.',
  'table.never_autovacuumed': 'A large table has never been vacuumed.',
  'table.seq_scan_heavy': 'A large table is mostly read in full, not through an index.',
  'table.wraparound_risk':
    'A table holds transaction IDs that are old: it needs a freezing vacuum.',
  'trigger.constraint_disabled': 'The triggers that enforce a constraint are disabled.',
  'trigger.user_disabled': 'A user trigger is disabled.',
  'txn.high_rollback_ratio': 'A large share of transactions end in a rollback.',
  'txn.idle_in_transaction': 'A session has been idle inside an open transaction for a long time.',
  'txn.long_running': 'A transaction has been open for a long time.',
  'txn.prepared_orphan': 'A prepared transaction has been left open for a long time.',
  'txn.wraparound_risk':
    'The oldest transaction ID in a database is getting old: it needs a freezing vacuum.',
  'vacuum.disabled': 'Automatic vacuum (autovacuum) is turned off.',
  'vacuum.starvation': 'Many tables have dead rows while every vacuum worker is busy.',
  'wal.directory_large': 'The WAL directory is larger than the WAL settings explain.',
} as const

const ALERT_RULE_TEXT = {
  agent_down: 'No report has arrived from the agent of an instance.',
  instance_unreachable:
    'The server has had no report from an instance within the expected interval.',
  check_failing: 'A check has been failing for over five minutes.',
  no_primary_in_cluster: 'No instance of the cluster reports the primary role.',
  agent_buffer_full: 'The agent is dropping samples because its buffer is full.',
  clock_skew: 'The clock of the agent differs from the server clock by more than 30 seconds.',
  cardinality_budget_exceeded:
    'An instance sent more distinct series than its limit, so some were dropped.',
  failover_detected: 'A failover was observed: another instance became the primary.',
  split_brain_detected: 'More than one instance of the cluster reports the primary role.',
  slot_inactive: 'A replication slot is inactive and keeps WAL on the primary.',
  slot_lost: 'A replication slot lost WAL its consumer still needed.',
  'replica.lag_high': 'A standby is more than 30 seconds behind its primary.',
  'replica.all_standbys_lagging': 'Every standby of the cluster is more than 30 seconds behind.',
  'replica.no_sync_standby': 'The cluster has no synchronous standby.',
  'replica.redundancy_reduced':
    'Fewer standbys stream than the cluster has members: a standby is down, disconnected or not yet rejoined; a member silent for over an hour is no longer counted.',
  'conn.near_max': 'More than 80% of the connections allowed by max_connections are in use.',
  'conn.idle_in_transaction': 'A session has been idle in a transaction for over 5 minutes.',
  'db.deadlock_rate': 'Deadlocks are occurring.',
  'disk.free_low': 'Less than 15% of the data disk is free.',
} as const

export const ADVISOR_RULE_IDS = Object.keys(ADVISOR_RULE_TEXT) as readonly string[]
export const ALERT_RULE_IDS = Object.keys(ALERT_RULE_TEXT) as readonly string[]

const RULE_TEXT: Readonly<Record<string, string>> = { ...ALERT_RULE_TEXT, ...ADVISOR_RULE_TEXT }

/** The plain sentence for a rule id, or `undefined` for an id this build does not know. */
export function ruleText(id: string): string | undefined {
  return RULE_TEXT[id]
}

const NEED_TEXT: Readonly<Record<string, string>> = {
  Baseline: 'seven-day query history',
  Bloat: 'wasted-space estimates',
  Indexes: 'index statistics',
  LockTree: 'lock waits',
  Replication: 'replication status',
  Siblings: 'other instances of the cluster',
  Statements: 'query statistics',
  Tables: 'table statistics',
  extensions: 'installed extensions',
  fk_indexes: 'foreign key indexes',
  function_security: 'function security settings',
  function_stats: 'function call statistics',
  function_tracking: 'function tracking setting',
  fk_unindexed: 'foreign key indexes',
  extension: 'installed extensions',
  host: 'host metrics',
  logical_replication: 'logical replication state',
  monitoring_role: 'monitoring role settings',
  partitioned_table: 'partitioned tables',
  partitions: 'partitioned tables',
  pg_connections_idle_share: 'share of idle connections',
  pg_connections_used: 'connections in use',
  pg_connections_used_ratio: 'share of connections in use',
  pg_vacuum_jobs_running: 'running vacuum jobs',
  pg_xact_rollback_ratio: 'share of rolled back transactions',
  postgis_columns: 'PostGIS columns',
  'query.temp_bytes_high': 'temporary file use of each statement',
  recovery: 'standby recovery state',
  security_definer_function: 'SECURITY DEFINER functions',
  sequence: 'sequences',
  sequences: 'sequences',
  settings: 'server settings',
  spatial_column: 'PostGIS columns',
  subscription: 'logical replication subscriptions',
  trigger: 'triggers',
  trigger_inventory: 'triggers and constraints',
  wal_directory: 'size of the WAL directory',
  xmin_horizon: 'oldest transaction holding back vacuum',
}

/** Plain words for one input a rule needs; an unknown name is shown with its underscores removed. */
export function needText(name: string): string {
  return NEED_TEXT[name] ?? name.replaceAll('_', ' ')
}

/** The inputs a rule needs, in plain words and separated by commas. */
export function needsText(needs: readonly string[]): string {
  return needs.length === 0 ? 'None' : needs.map(needText).join(', ')
}
