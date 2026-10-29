/**
 * Series names of `GET /api/v1/instances/{id}/workload/series`, per source.
 * They mirror the server whitelist (`workloadSources` in
 * internal/server/workload_series.go); the server refuses any other name.
 */
export const WORKLOAD_SERIES = {
  statements: [
    'calls',
    'exec_time',
    'rows',
    'blks_hit',
    'blks_read',
    'hit_ratio',
    'wal_bytes',
    'temp_blks',
  ],
  tables: [
    'seq_scans',
    'idx_scans',
    'tup_ins',
    'tup_upd',
    'tup_del',
    'tup_hot_upd',
    'dead_tuples',
    'live_tuples',
    'autovacuums',
  ],
  indexes: [
    'idx_scans',
    'idx_tup_read',
    'idx_tup_fetch',
    'idx_blks_read',
    'idx_blks_hit',
    'index_bytes',
  ],
} as const

export type WorkloadSource = keyof typeof WORKLOAD_SERIES
export type WorkloadSeriesName<S extends WorkloadSource> = (typeof WORKLOAD_SERIES)[S][number]

/** Seconds between two agent scrapes of each source (statements 60 s, tables and indexes 5 min). */
export const WORKLOAD_SAMPLE_SECONDS: Record<WorkloadSource, number> = {
  statements: 60,
  tables: 300,
  indexes: 300,
}

/** The gauges of the whitelist; every other series is a per-second rate. */
export const WORKLOAD_GAUGES: ReadonlySet<string> = new Set([
  'dead_tuples',
  'live_tuples',
  'index_bytes',
])

export function isWorkloadSeries(source: WorkloadSource, name: string): boolean {
  return (WORKLOAD_SERIES[source] as readonly string[]).includes(name)
}
