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
 * end in `_total`. Derived from internal/check: wal.go (`walCounterColumn`
 * over the pg_stat_wal columns of PostgreSQL 14-18, plus `pg_wal_lsn_bytes`)
 * and stat_statements.go.
 */
const COUNTERS_WITHOUT_SUFFIX = new Set([
  'pg_wal_lsn_bytes',
  'pg_wal_wal_records',
  'pg_wal_wal_fpi',
  'pg_wal_wal_bytes',
  'pg_wal_wal_buffers_full',
  'pg_wal_wal_write',
  'pg_wal_wal_sync',
  'pg_wal_wal_write_time',
  'pg_wal_wal_sync_time',
  'pg_stat_statements_total_exec_time_ms',
])

/**
 * Whether a metric is a per-second rate derived from a counter or a gauge.
 * This is the only place that decides it.
 */
export function metricKind(name: string): MetricKind {
  if (name.endsWith('_total') || name.includes('_rate')) return 'rate'
  if (COUNTERS_WITHOUT_SUFFIX.has(name)) return 'rate'
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
