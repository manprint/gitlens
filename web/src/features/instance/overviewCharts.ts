import type { MetricUnit } from '@/components/charts/metric.options'
import type { CaptionId } from '@/lib/captions'
import { instance as englishInstance } from '@/i18n/messages/en/instance'
import { metricKind, SAMPLE_SECONDS, stepFor, type MetricKind } from '@/lib/chartStep'
import type { ChartNavLink } from './ChartNav'
import { WORKLOAD_GAUGES, WORKLOAD_SAMPLE_SECONDS, type WorkloadSource } from '@/lib/workloadSeries'

/** A key of the instance catalog. */
export type OverviewKey = keyof typeof englishInstance

/** English text of a key: the specs' `label`/`title` fields keep it for the unit tests and e2e. */
function enText(key: OverviewKey): string {
  return englishInstance[key]
}

/**
 * Where a `pg_*` metric is stored: per database (pg_stat_database counters,
 * requested with the selected database) or instance-wide (stored without one).
 */
export type MetricScope = 'database' | 'instance'

export interface MetricLine {
  metric: string
  /** Legend and data-table name of the line. */
  label: string
  labelKey: OverviewKey
  scope: MetricScope
  /** Unit of this line when it differs from the card's; it then uses a right axis. */
  unit?: MetricUnit
}

export interface WorkloadLine {
  series: string
  label: string
  labelKey: OverviewKey
  unit?: MetricUnit
}

interface CardBase {
  id: string
  /** The card's caption, one entry of the instance caption set. */
  captionId: CaptionId
  title: string
  titleKey: OverviewKey
  /** Unit of the values, shown after the title, e.g. `per second`. */
  unitHint?: string
  unitHintKey?: OverviewKey
  unit: MetricUnit
  /** Span both columns of the group grid. */
  wide?: boolean
  /** The range line counts gaps and counter resets; not on a derived ratio or a gauge. */
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
  titleKey: OverviewKey
  /** The group card's caption (its sentence under the title). */
  captionId: CaptionId
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

const db = (
  metric: string,
  labelKey: OverviewKey,
  extra: Partial<MetricLine> = {},
): MetricLine => ({
  metric,
  label: enText(labelKey),
  labelKey,
  scope: 'database',
  ...extra,
})
const instance = (
  metric: string,
  labelKey: OverviewKey,
  extra: Partial<MetricLine> = {},
): MetricLine => ({
  metric,
  label: enText(labelKey),
  labelKey,
  scope: 'instance',
  ...extra,
})

/** The Overview chart groups, in page order (D4). */
export const OVERVIEW_GROUPS: readonly ChartGroupSpec[] = [
  {
    id: 'workload',
    titleKey: 'instance.overviewCharts.group.workload.title',
    title: enText('instance.overviewCharts.group.workload.title'),
    captionId: 'overview.group-workload',
    cards: [
      {
        id: 'transactions',
        captionId: 'overview.chart-transactions',
        titleKey: 'instance.overviewCharts.card.transactions.title',
        title: enText('instance.overviewCharts.card.transactions.title'),
        unit: 'count',
        wide: true,
        source: 'metrics',
        metrics: [
          db('pg_xact_commit_total', 'instance.overviewCharts.line.commits'),
          db('pg_xact_rollback_total', 'instance.overviewCharts.line.rollbacks'),
        ],
      },
      {
        id: 'connections',
        captionId: 'overview.chart-connections',
        titleKey: 'instance.overviewCharts.card.connections.title',
        title: enText('instance.overviewCharts.card.connections.title'),
        unit: 'count',
        annotateResets: false,
        source: 'metrics',
        metrics: [
          instance('pg_connections_used', 'instance.overviewCharts.line.connectionsInUse'),
          instance('pg_connections_limit', 'instance.overviewCharts.line.configuredMaximum'),
        ],
      },
      {
        id: 'deadlocks-conflicts',
        captionId: 'overview.chart-deadlocks-conflicts',
        titleKey: 'instance.overviewCharts.card.deadlocksConflicts.title',
        title: enText('instance.overviewCharts.card.deadlocksConflicts.title'),
        unitHintKey: 'instance.overviewCharts.hint.perSecond',
        unitHint: enText('instance.overviewCharts.hint.perSecond'),
        unit: 'count',
        source: 'metrics',
        metrics: [
          db('pg_deadlocks_total', 'instance.overviewCharts.line.deadlocks'),
          db('pg_conflicts_total', 'instance.overviewCharts.line.recoveryConflicts'),
        ],
      },
    ],
  },
  {
    id: 'memory-io',
    titleKey: 'instance.overviewCharts.group.memoryIo.title',
    title: enText('instance.overviewCharts.group.memoryIo.title'),
    captionId: 'overview.group-memory-io',
    cards: [
      {
        id: 'cache-hit-ratio',
        captionId: 'overview.chart-cache-hit-ratio',
        titleKey: 'instance.overviewCharts.card.cacheHitRatio.title',
        title: enText('instance.overviewCharts.card.cacheHitRatio.title'),
        unit: 'ratio',
        annotateResets: false,
        source: 'metrics',
        derive: 'cache-hit-ratio',
        metrics: [
          db('pg_blks_hit_total', 'instance.overviewCharts.line.blocksHit'),
          db('pg_blks_read_total', 'instance.overviewCharts.line.blocksRead'),
        ],
      },
      {
        id: 'blocks',
        captionId: 'overview.chart-blocks',
        titleKey: 'instance.overviewCharts.card.blocks.title',
        title: enText('instance.overviewCharts.card.blocks.title'),
        unitHintKey: 'instance.overviewCharts.hint.blocksPerSecond',
        unitHint: enText('instance.overviewCharts.hint.blocksPerSecond'),
        unit: 'count',
        source: 'metrics',
        metrics: [
          db('pg_blks_read_total', 'instance.overviewCharts.line.readFromDiskOrOsCache'),
          db('pg_blks_hit_total', 'instance.overviewCharts.line.hitInSharedBuffers'),
        ],
      },
      {
        id: 'temp',
        captionId: 'overview.chart-temp',
        titleKey: 'instance.overviewCharts.card.temp.title',
        title: enText('instance.overviewCharts.card.temp.title'),
        unitHintKey: 'instance.overviewCharts.hint.perSecond',
        unitHint: enText('instance.overviewCharts.hint.perSecond'),
        unit: 'count',
        wide: true,
        source: 'metrics',
        metrics: [
          db('pg_temp_files_total', 'instance.overviewCharts.line.temporaryFiles'),
          db('pg_temp_bytes_total', 'instance.overviewCharts.line.temporaryBytes', {
            unit: 'bytes',
          }),
        ],
      },
    ],
  },
  {
    id: 'wal-checkpoints',
    titleKey: 'instance.overviewCharts.group.walCheckpoints.title',
    title: enText('instance.overviewCharts.group.walCheckpoints.title'),
    captionId: 'overview.group-wal-checkpoints',
    cards: [
      {
        id: 'wal',
        captionId: 'overview.chart-wal',
        titleKey: 'instance.overviewCharts.card.wal.title',
        title: enText('instance.overviewCharts.card.wal.title'),
        unitHintKey: 'instance.overviewCharts.hint.perSecond',
        unitHint: enText('instance.overviewCharts.hint.perSecond'),
        unit: 'bytes',
        wide: true,
        source: 'metrics',
        metrics: [
          instance('pg_wal_wal_bytes', 'instance.overviewCharts.line.walBytes'),
          instance('pg_wal_wal_records', 'instance.overviewCharts.line.walRecords', {
            unit: 'count',
          }),
          instance('pg_wal_wal_fpi', 'instance.overviewCharts.line.fullPageImages', {
            unit: 'count',
          }),
        ],
      },
      {
        id: 'checkpoints',
        captionId: 'overview.chart-checkpoints',
        titleKey: 'instance.overviewCharts.card.checkpoints.title',
        title: enText('instance.overviewCharts.card.checkpoints.title'),
        unitHintKey: 'instance.overviewCharts.hint.perStep',
        unitHint: enText('instance.overviewCharts.hint.perStep'),
        unit: 'count',
        source: 'metrics',
        perStep: true,
        metrics: [
          instance('pg_checkpoints_timed_total', 'instance.overviewCharts.line.timed'),
          instance('pg_checkpoints_requested_total', 'instance.overviewCharts.line.requested'),
        ],
      },
      {
        id: 'checkpoint-time',
        captionId: 'overview.chart-checkpoint-time',
        titleKey: 'instance.overviewCharts.card.checkpointTime.title',
        title: enText('instance.overviewCharts.card.checkpointTime.title'),
        unitHintKey: 'instance.overviewCharts.hint.perStep',
        unitHint: enText('instance.overviewCharts.hint.perStep'),
        unit: 'ms',
        source: 'metrics',
        perStep: true,
        metrics: [
          instance('pg_checkpoint_write_time_ms_total', 'instance.overviewCharts.line.write'),
          instance('pg_checkpoint_sync_time_ms_total', 'instance.overviewCharts.line.sync'),
        ],
      },
    ],
  },
  {
    id: 'queries',
    titleKey: 'instance.overviewCharts.group.queries.title',
    title: enText('instance.overviewCharts.group.queries.title'),
    captionId: 'overview.group-queries',
    cards: [
      {
        id: 'calls',
        captionId: 'overview.chart-calls',
        titleKey: 'instance.overviewCharts.card.calls.title',
        title: enText('instance.overviewCharts.card.calls.title'),
        unitHintKey: 'instance.overviewCharts.hint.trackedStatements',
        unitHint: enText('instance.overviewCharts.hint.trackedStatements'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [
          {
            series: 'calls',
            labelKey: 'instance.overviewCharts.line.callsPerSecond',
            label: enText('instance.overviewCharts.line.callsPerSecond'),
          },
        ],
      },
      {
        id: 'exec-time',
        captionId: 'overview.chart-exec-time',
        titleKey: 'instance.overviewCharts.card.execTime.title',
        title: enText('instance.overviewCharts.card.execTime.title'),
        unitHintKey: 'instance.overviewCharts.hint.msPerSecondTrackedStatements',
        unitHint: enText('instance.overviewCharts.hint.msPerSecondTrackedStatements'),
        unit: 'ms',
        source: 'workload',
        workloadSource: 'statements',
        series: [
          {
            series: 'exec_time',
            labelKey: 'instance.overviewCharts.line.executionTime',
            label: enText('instance.overviewCharts.line.executionTime'),
          },
        ],
      },
      {
        id: 'rows',
        captionId: 'overview.chart-rows',
        titleKey: 'instance.overviewCharts.card.rows.title',
        title: enText('instance.overviewCharts.card.rows.title'),
        unitHintKey: 'instance.overviewCharts.hint.trackedStatements',
        unitHint: enText('instance.overviewCharts.hint.trackedStatements'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [
          {
            series: 'rows',
            labelKey: 'instance.overviewCharts.line.rowsPerSecond',
            label: enText('instance.overviewCharts.line.rowsPerSecond'),
          },
        ],
      },
      {
        id: 'statement-reads',
        captionId: 'overview.chart-statement-reads',
        titleKey: 'instance.overviewCharts.card.statementReads.title',
        title: enText('instance.overviewCharts.card.statementReads.title'),
        unitHintKey: 'instance.overviewCharts.hint.blocksPerSecondTrackedStatements',
        unitHint: enText('instance.overviewCharts.hint.blocksPerSecondTrackedStatements'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'statements',
        series: [
          {
            series: 'blks_read',
            labelKey: 'instance.overviewCharts.line.sharedBlocksRead',
            label: enText('instance.overviewCharts.line.sharedBlocksRead'),
          },
        ],
      },
      {
        id: 'statement-temp',
        captionId: 'overview.chart-statement-temp',
        titleKey: 'instance.overviewCharts.card.statementTemp.title',
        title: enText('instance.overviewCharts.card.statementTemp.title'),
        unitHintKey: 'instance.overviewCharts.hint.blocksPerSecondTrackedStatements',
        unitHint: enText('instance.overviewCharts.hint.blocksPerSecondTrackedStatements'),
        unit: 'count',
        wide: true,
        source: 'workload',
        workloadSource: 'statements',
        series: [
          {
            series: 'temp_blks',
            labelKey: 'instance.overviewCharts.line.tempBlocksWritten',
            label: enText('instance.overviewCharts.line.tempBlocksWritten'),
          },
        ],
      },
    ],
  },
  {
    id: 'tables-indexes',
    titleKey: 'instance.overviewCharts.group.tablesIndexes.title',
    title: enText('instance.overviewCharts.group.tablesIndexes.title'),
    captionId: 'overview.group-tables-indexes',
    cards: [
      {
        id: 'scans',
        captionId: 'overview.chart-scans',
        titleKey: 'instance.overviewCharts.card.scans.title',
        title: enText('instance.overviewCharts.card.scans.title'),
        unitHintKey: 'instance.overviewCharts.hint.perSecondTrackedTables',
        unitHint: enText('instance.overviewCharts.hint.perSecondTrackedTables'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'tables',
        series: [
          {
            series: 'seq_scans',
            labelKey: 'instance.overviewCharts.line.sequentialScans',
            label: enText('instance.overviewCharts.line.sequentialScans'),
          },
          {
            series: 'idx_scans',
            labelKey: 'instance.overviewCharts.line.indexScans',
            label: enText('instance.overviewCharts.line.indexScans'),
          },
        ],
      },
      {
        id: 'tuple-churn',
        captionId: 'overview.chart-tuple-churn',
        titleKey: 'instance.overviewCharts.card.tupleChurn.title',
        title: enText('instance.overviewCharts.card.tupleChurn.title'),
        unitHintKey: 'instance.overviewCharts.hint.rowsPerSecondTrackedTables',
        unitHint: enText('instance.overviewCharts.hint.rowsPerSecondTrackedTables'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'tables',
        series: [
          {
            series: 'tup_ins',
            labelKey: 'instance.overviewCharts.line.inserted',
            label: enText('instance.overviewCharts.line.inserted'),
          },
          {
            series: 'tup_upd',
            labelKey: 'instance.overviewCharts.line.updated',
            label: enText('instance.overviewCharts.line.updated'),
          },
          {
            series: 'tup_hot_upd',
            labelKey: 'instance.overviewCharts.line.hotUpdated',
            label: enText('instance.overviewCharts.line.hotUpdated'),
          },
          {
            series: 'tup_del',
            labelKey: 'instance.overviewCharts.line.deleted',
            label: enText('instance.overviewCharts.line.deleted'),
          },
        ],
      },
      {
        id: 'dead-tuples',
        captionId: 'overview.chart-dead-tuples',
        titleKey: 'instance.overviewCharts.card.deadTuples.title',
        title: enText('instance.overviewCharts.card.deadTuples.title'),
        unitHintKey: 'instance.overviewCharts.hint.trackedTables',
        unitHint: enText('instance.overviewCharts.hint.trackedTables'),
        unit: 'count',
        annotateResets: false,
        source: 'workload',
        workloadSource: 'tables',
        series: [
          {
            series: 'dead_tuples',
            labelKey: 'instance.overviewCharts.line.deadTuples',
            label: enText('instance.overviewCharts.line.deadTuples'),
          },
        ],
      },
      {
        id: 'index-scans',
        captionId: 'overview.chart-index-scans',
        titleKey: 'instance.overviewCharts.card.indexScans.title',
        title: enText('instance.overviewCharts.card.indexScans.title'),
        unitHintKey: 'instance.overviewCharts.hint.perSecondTrackedIndexes',
        unitHint: enText('instance.overviewCharts.hint.perSecondTrackedIndexes'),
        unit: 'count',
        source: 'workload',
        workloadSource: 'indexes',
        series: [
          {
            series: 'idx_scans',
            labelKey: 'instance.overviewCharts.line.indexScans',
            label: enText('instance.overviewCharts.line.indexScans'),
          },
        ],
      },
      {
        id: 'index-size',
        captionId: 'overview.chart-index-size',
        titleKey: 'instance.overviewCharts.card.indexSize.title',
        title: enText('instance.overviewCharts.card.indexSize.title'),
        unitHintKey: 'instance.overviewCharts.hint.trackedIndexes',
        unitHint: enText('instance.overviewCharts.hint.trackedIndexes'),
        unit: 'bytes',
        wide: true,
        annotateResets: false,
        source: 'workload',
        workloadSource: 'indexes',
        series: [
          {
            series: 'index_bytes',
            labelKey: 'instance.overviewCharts.line.indexSize',
            label: enText('instance.overviewCharts.line.indexSize'),
          },
        ],
      },
    ],
  },
]

/** The anchor nav: the five chart groups, then the Host section below the Overview. */
export const OVERVIEW_NAV_LINKS: readonly ChartNavLink[] = [
  ...OVERVIEW_GROUPS.map(({ id, titleKey, title }) => ({ id, labelKey: titleKey, label: title })),
  // Host is its own tab of the instance page (15.2): the link switches tabs.
  {
    id: 'host',
    labelKey: 'instance.section.host',
    label: enText('instance.section.host'),
    section: 'host',
  },
]
