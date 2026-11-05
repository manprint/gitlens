import type { MetricUnit } from '@/components/charts/metric.options'
import type { CaptionId } from '@/lib/captions'
import { metricKind, SAMPLE_SECONDS, stepFor, type MetricKind } from '@/lib/chartStep'
import type { ChartNavLink } from './ChartNav'
import { WORKLOAD_GAUGES, WORKLOAD_SAMPLE_SECONDS, type WorkloadSource } from '@/lib/workloadSeries'

/**
 * Where a `pg_*` metric is stored: per database (pg_stat_database counters,
 * requested with the selected database) or instance-wide (stored without one).
 */
export type MetricScope = 'database' | 'instance'

export interface MetricLine {
  metric: string
  /** Legend and data-table name of the line. */
  label: string
  scope: MetricScope
  /** Unit of this line when it differs from the card's; it then uses a right axis. */
  unit?: MetricUnit
}

export interface WorkloadLine {
  series: string
  label: string
  unit?: MetricUnit
}

interface CardBase {
  id: string
  /** The card's caption, one entry of the instance caption set. */
  captionId: CaptionId
  title: string
  /** Unit of the values, shown after the title, e.g. `per second`. */
  unitHint?: string
  unit: MetricUnit
  /** Span both columns of the group grid. */
  wide?: boolean
  /** Dashed reset lines make no sense on a derived ratio or a gauge. */
  annotateResets?: boolean
}

export interface MetricCardSpec extends CardBase {
  source: 'metrics'
  metrics: readonly MetricLine[]
  /** `cache-hit-ratio`: the two lines (hit, read) become one hit / (hit + read) line. */
  derive?: 'cache-hit-ratio'
  /**
   * Show the count in each step (rate × step) instead of the per-second rate:
   * rare events such as checkpoints read as "1", not as a 60/h spike.
   */
  perStep?: boolean
}

export interface WorkloadCardSpec extends CardBase {
  source: 'workload'
  workloadSource: WorkloadSource
  series: readonly WorkloadLine[]
}

export type ChartCardSpec = MetricCardSpec | WorkloadCardSpec

export interface ChartGroupSpec {
  id: string
  title: string
  description: string
  cards: readonly ChartCardSpec[]
}

/** The check that samples each instance metric; the others come from database_stats. */
const METRIC_CHECK: Record<string, string> = {
  pg_connections_used: 'activity',
  pg_connections_limit: 'activity',
  pg_checkpoints_timed_total: 'checkpointer',
  pg_checkpoints_requested_total: 'checkpointer',
  pg_checkpoint_write_time_ms_total: 'checkpointer',
  pg_checkpoint_sync_time_ms_total: 'checkpointer',
  pg_wal_lsn_bytes: 'wal',
  pg_wal_wal_bytes: 'wal',
  pg_wal_wal_records: 'wal',
  pg_wal_wal_fpi: 'wal',
}

/**
 * Query step of one metric over the range, in seconds: never finer than its
 * sampling (two samples for a rate), so no bucket is empty for lack of data.
 */
export function overviewStepSeconds(metric: string, from: Date, to: Date): number {
  return stepFor(
    metricKind(metric),
    from,
    to,
    SAMPLE_SECONDS[METRIC_CHECK[metric] ?? 'database_stats'],
  )
}

/** One step for every line of a card, so the lines share their buckets. */
export function cardStepSeconds(card: ChartCardSpec, from: Date, to: Date): number {
  if (card.source === 'metrics') {
    return Math.max(...card.metrics.map(({ metric }) => overviewStepSeconds(metric, from, to)))
  }
  // The server never steps below two samples of the source, for gauges too.
  const sample = WORKLOAD_SAMPLE_SECONDS[card.workloadSource]
  return Math.max(stepFor(workloadCardKind(card), from, to, sample), 2 * sample)
}

/** A card of gauges only is a gauge; one rate makes it a rate (coarser step). */
export function workloadCardKind(card: WorkloadCardSpec): MetricKind {
  return card.series.every(({ series }) => WORKLOAD_GAUGES.has(series)) ? 'gauge' : 'rate'
}

const db = (metric: string, label: string, extra: Partial<MetricLine> = {}): MetricLine => ({
  metric,
  label,
  scope: 'database',
  ...extra,
})
const instance = (metric: string, label: string, extra: Partial<MetricLine> = {}): MetricLine => ({
  metric,
  label,
  scope: 'instance',
  ...extra,
})

/** The Overview chart groups, in page order (D4). */
export const OVERVIEW_GROUPS: readonly ChartGroupSpec[] = [
  {
    id: 'workload',
    title: 'Workload',
    description:
      'Transactions, connections and the errors that abort them. Transactions, deadlocks and conflicts are for the selected database; connections are instance-wide.',
    cards: [
      {
        id: 'transactions',
        captionId: 'overview.chart-transactions',
        title: 'Transactions per second',
        unit: 'count',
        wide: true,
        source: 'metrics',
        metrics: [db('pg_xact_commit_total', 'Commits'), db('pg_xact_rollback_total', 'Rollbacks')],
      },
      {
        id: 'connections',
        captionId: 'overview.chart-connections',
        title: 'Connections',
        unit: 'count',
        annotateResets: false,
        source: 'metrics',
        metrics: [
          instance('pg_connections_used', 'Connections in use'),
          instance('pg_connections_limit', 'Configured maximum'),
        ],
      },
      {
        id: 'deadlocks-conflicts',
        captionId: 'overview.chart-deadlocks-conflicts',
        title: 'Deadlocks and conflicts',
        unitHint: 'per second',
        unit: 'count',
        source: 'metrics',
        metrics: [
          db('pg_deadlocks_total', 'Deadlocks'),
          db('pg_conflicts_total', 'Recovery conflicts'),
        ],
      },
    ],
  },
  {
    id: 'memory-io',
    title: 'Memory & I/O',
    description:
      'How often reads are served from shared buffers, and the work spilled to temporary files, for the selected database.',
    cards: [
      {
        id: 'cache-hit-ratio',
        captionId: 'overview.chart-cache-hit-ratio',
        title: 'Cache hit ratio',
        unit: 'ratio',
        annotateResets: false,
        source: 'metrics',
        derive: 'cache-hit-ratio',
        metrics: [db('pg_blks_hit_total', 'Blocks hit'), db('pg_blks_read_total', 'Blocks read')],
      },
      {
        id: 'blocks',
        captionId: 'overview.chart-blocks',
        title: 'Blocks read vs hit',
        unitHint: 'blocks per second',
        unit: 'count',
        source: 'metrics',
        metrics: [
          db('pg_blks_read_total', 'Read from disk or OS cache'),
          db('pg_blks_hit_total', 'Hit in shared buffers'),
        ],
      },
      {
        id: 'temp',
        captionId: 'overview.chart-temp',
        title: 'Temp files and bytes',
        unitHint: 'per second',
        unit: 'count',
        wide: true,
        source: 'metrics',
        metrics: [
          db('pg_temp_files_total', 'Temporary files'),
          db('pg_temp_bytes_total', 'Temporary bytes', { unit: 'bytes' }),
        ],
      },
    ],
  },
  {
    id: 'wal-checkpoints',
    title: 'WAL & checkpoints',
    description:
      'Write-ahead log generated by the whole instance, and the checkpoints that flush it.',
    cards: [
      {
        id: 'wal',
        captionId: 'overview.chart-wal',
        title: 'WAL generation',
        unitHint: 'per second',
        unit: 'bytes',
        wide: true,
        source: 'metrics',
        metrics: [
          instance('pg_wal_wal_bytes', 'WAL bytes'),
          instance('pg_wal_wal_records', 'WAL records', { unit: 'count' }),
          instance('pg_wal_wal_fpi', 'Full-page images', { unit: 'count' }),
        ],
      },
      {
        id: 'checkpoints',
        captionId: 'overview.chart-checkpoints',
        title: 'Checkpoints',
        unitHint: 'per step',
        unit: 'count',
        source: 'metrics',
        perStep: true,
        metrics: [
          instance('pg_checkpoints_timed_total', 'Timed'),
          instance('pg_checkpoints_requested_total', 'Requested'),
        ],
      },
      {
        id: 'checkpoint-time',
        captionId: 'overview.chart-checkpoint-time',
        title: 'Checkpoint write and sync time',
        unitHint: 'per step',
        unit: 'ms',
        source: 'metrics',
        perStep: true,
        metrics: [
          instance('pg_checkpoint_write_time_ms_total', 'Write'),
          instance('pg_checkpoint_sync_time_ms_total', 'Sync'),
        ],
      },
    ],
  },
  {
    id: 'queries',
    title: 'Queries',
    description:
      'Totals over the statements tracked by pg_stat_statements (the agent keeps the top statements of each scrape), for every database of the instance.',
    cards: [
      {
        id: 'calls',
        captionId: 'overview.chart-calls',
        title: 'Calls per second',
        unitHint: 'tracked statements',
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [{ series: 'calls', label: 'Calls per second' }],
      },
      {
        id: 'exec-time',
        captionId: 'overview.chart-exec-time',
        title: 'Execution time per second',
        unitHint: 'ms per second, tracked statements',
        unit: 'ms',
        source: 'workload',
        workloadSource: 'statements',
        series: [{ series: 'exec_time', label: 'Execution time' }],
      },
      {
        id: 'rows',
        captionId: 'overview.chart-rows',
        title: 'Rows per second',
        unitHint: 'tracked statements',
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [{ series: 'rows', label: 'Rows per second' }],
      },
      {
        id: 'statement-reads',
        captionId: 'overview.chart-statement-reads',
        title: 'Statement block reads',
        unitHint: 'blocks per second, tracked statements',
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [{ series: 'blks_read', label: 'Shared blocks read' }],
      },
      {
        id: 'statement-temp',
        captionId: 'overview.chart-statement-temp',
        title: 'Statement temp blocks',
        unitHint: 'blocks per second, tracked statements',
        unit: 'count',
        wide: true,
        source: 'workload',
        workloadSource: 'statements',
        series: [{ series: 'temp_blks', label: 'Temp blocks written' }],
      },
    ],
  },
  {
    id: 'tables-indexes',
    title: 'Tables & indexes',
    description:
      'Totals over the tables and indexes tracked by the agent, for every database of the instance. Tables and indexes are sampled every 5 minutes.',
    cards: [
      {
        id: 'scans',
        captionId: 'overview.chart-scans',
        title: 'Sequential vs index scans',
        unitHint: 'per second, tracked tables',
        unit: 'count',
        source: 'workload',
        workloadSource: 'tables',
        series: [
          { series: 'seq_scans', label: 'Sequential scans' },
          { series: 'idx_scans', label: 'Index scans' },
        ],
      },
      {
        id: 'tuple-churn',
        captionId: 'overview.chart-tuple-churn',
        title: 'Tuple churn',
        unitHint: 'rows per second, tracked tables',
        unit: 'count',
        source: 'workload',
        workloadSource: 'tables',
        series: [
          { series: 'tup_ins', label: 'Inserted' },
          { series: 'tup_upd', label: 'Updated' },
          { series: 'tup_hot_upd', label: 'HOT updated' },
          { series: 'tup_del', label: 'Deleted' },
        ],
      },
      {
        id: 'dead-tuples',
        captionId: 'overview.chart-dead-tuples',
        title: 'Dead tuples',
        unitHint: 'tracked tables',
        unit: 'count',
        annotateResets: false,
        source: 'workload',
        workloadSource: 'tables',
        series: [{ series: 'dead_tuples', label: 'Dead tuples' }],
      },
      {
        id: 'index-scans',
        captionId: 'overview.chart-index-scans',
        title: 'Index scans',
        unitHint: 'per second, tracked indexes',
        unit: 'count',
        source: 'workload',
        workloadSource: 'indexes',
        series: [{ series: 'idx_scans', label: 'Index scans' }],
      },
      {
        id: 'index-size',
        captionId: 'overview.chart-index-size',
        title: 'Index size',
        unitHint: 'tracked indexes',
        unit: 'bytes',
        wide: true,
        annotateResets: false,
        source: 'workload',
        workloadSource: 'indexes',
        series: [{ series: 'index_bytes', label: 'Index size' }],
      },
    ],
  },
]

/** Charts of the Overview share their tooltip pointer and zoom. */
export const OVERVIEW_CHART_GROUP = 'overview-charts'

/** The anchor nav: the five chart groups, then the Host section below the Overview. */
export const OVERVIEW_NAV_LINKS: readonly ChartNavLink[] = [
  ...OVERVIEW_GROUPS.map(({ id, title }) => ({ id, label: title })),
  // Host is its own tab of the instance page (15.2): the link switches tabs.
  { id: 'host', label: 'Host', section: 'host' },
]
