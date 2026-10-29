import { chooseStep, STEP_LADDER, stepMilliseconds } from './timerange'

export type MetricKind = 'rate' | 'gauge'

/** Seconds between two samples of each check; charts never query finer than this. */
export const SAMPLE_SECONDS: Record<string, number> = {
  database_stats: 30,
  wal: 15,
  activity: 10,
  checkpointer: 30,
  io: 30,
  host: 30,
  statements: 60,
  tables: 300,
  indexes: 300,
}

export const DEFAULT_SAMPLE_SECONDS = 30

/**
 * Agent counters (KindCounter, delta-converted at ingest) whose names do not
 * end in `_total`, from internal/check: table_stats.go, index_stats.go and
 * stat_statements.go. The pg_wal_* counters follow `walCounterColumn` below.
 */
const COUNTERS_WITHOUT_SUFFIX = new Set([
  'pg_stat_statements_total_exec_time_ms',
  'pg_table_seq_scan',
  'pg_table_seq_tup_read',
  'pg_table_idx_scan',
  'pg_table_idx_tup_fetch',
  'pg_table_n_tup_ins',
  'pg_table_n_tup_upd',
  'pg_table_n_tup_del',
  'pg_table_n_tup_hot_upd',
  'pg_table_autovacuum_count',
  'pg_table_autoanalyze_count',
  'pg_index_idx_scan',
  'pg_index_idx_tup_read',
  'pg_index_idx_tup_fetch',
  'pg_index_idx_blks_read',
  'pg_index_idx_blks_hit',
])

const WAL_COUNTER_COLUMNS = new Set([
  'wal_records',
  'wal_fpi',
  'wal_buffers_full',
  'wal_sync',
  'wal_write',
])

/** Mirrors wal.go `walCounterColumn` for `pg_wal_<column>`, plus `pg_wal_lsn_bytes`. */
function walCounterColumn(column: string): boolean {
  return (
    column.endsWith('_count') ||
    column.endsWith('_bytes') ||
    column.endsWith('_time') ||
    WAL_COUNTER_COLUMNS.has(column)
  )
}

/**
 * Whether a metric is a per-second rate derived from a counter or a gauge.
 * This is the only place that decides it.
 */
export function metricKind(name: string): MetricKind {
  if (name.endsWith('_total') || name.includes('_rate')) return 'rate'
  if (COUNTERS_WITHOUT_SUFFIX.has(name)) return 'rate'
  if (name.startsWith('pg_wal_') && walCounterColumn(name.slice('pg_wal_'.length))) return 'rate'
  // io.go stores every pg_stat_io column as a counter; only the timing flag is a gauge.
  if (name.startsWith('pg_io_') && name !== 'pg_io_timing_enabled') return 'rate'
  return 'gauge'
}

const LADDER_SECONDS = STEP_LADDER.map(({ milliseconds }) => milliseconds / 1_000)

/**
 * Query step in seconds: the range's base step, raised so that every bucket
 * holds at least one stored sample. A rate needs two samples (and at least one
 * minute), otherwise every other bucket is empty and the line breaks.
 */
export function stepFor(
  kind: MetricKind,
  from: Date,
  to: Date,
  sampleSeconds = DEFAULT_SAMPLE_SECONDS,
): number {
  const base = stepMilliseconds(chooseStep(from, to)) / 1_000
  const minStep = kind === 'rate' ? Math.max(60, 2 * sampleSeconds) : sampleSeconds
  const wanted = Math.max(base, minStep)
  return LADDER_SECONDS.find((seconds) => seconds >= wanted) ?? LADDER_SECONDS.at(-1) ?? 86_400
}
