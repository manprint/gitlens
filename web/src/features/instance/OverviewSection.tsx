import { REFRESH } from '@/api/policy'
import { useTimeRange } from '@/hooks/useTimeRange'
import { chooseStep, stepMilliseconds } from '@/lib/timerange'
import {
  cacheHitRatioSeries,
  cacheHitRatioValue,
  integrateRate,
  latestValue,
  scaleSeries,
  type MetricSeriesPoint,
} from '@/lib/metrics'
import { formatBytes, formatCount, formatRate } from '@/lib/format'
import { useQueryMetrics } from '@/api/queries'
import { Section } from '@/components/layout/Section'
import { MetricTile } from '@/components/layout/MetricTile'
import { Unknown } from '@/components/state/Unknown'

import { buildMetricOption } from '@/components/charts/metric.options'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'

interface OverviewSectionProps {
  instanceId: string
  database: string
}

function metricParams(
  metric: string,
  instanceId: string,
  database: string | undefined,
  from: Date,
  to: Date,
  step: string,
) {
  return {
    ...(database === undefined ? {} : { database }),
    from: from.toISOString(),
    instance_id: instanceId,
    metric,
    step,
    to: to.toISOString(),
  }
}

/**
 * Connections, checkpoints and WAL come from instance-wide checks and are
 * stored without a database. They used to be requested with the selected
 * database like the pg_stat_database counters, which filtered every one of
 * their rows out: those tiles and charts were always empty.
 */
function useOverviewMetricQueries(
  instanceId: string,
  database: string,
  from: Date,
  to: Date,
  step: string,
) {
  const perDatabase = (metric: string) => metricParams(metric, instanceId, database, from, to, step)
  const instanceWide = (metric: string) =>
    metricParams(metric, instanceId, undefined, from, to, step)

  return {
    blksHit: useQueryMetrics(perDatabase('pg_blks_hit_total')),
    blksRead: useQueryMetrics(perDatabase('pg_blks_read_total')),
    checkpointsRequested: useQueryMetrics(instanceWide('pg_checkpoints_requested_total')),
    checkpointsTimed: useQueryMetrics(instanceWide('pg_checkpoints_timed_total')),
    connectionsLimit: useQueryMetrics(instanceWide('pg_connections_limit')),
    connectionsUsed: useQueryMetrics(instanceWide('pg_connections_used')),
    deadlocks: useQueryMetrics(perDatabase('pg_deadlocks_total')),
    rollbacks: useQueryMetrics(perDatabase('pg_xact_rollback_total')),
    tempBytes: useQueryMetrics(perDatabase('pg_temp_bytes_total')),
    transactions: useQueryMetrics(perDatabase('pg_xact_commit_total')),
    walRate: useQueryMetrics(instanceWide('pg_wal_lsn_bytes')),
  }
}

function seriesFrom(query: ReturnType<typeof useQueryMetrics>): MetricSeriesPoint[] {
  return (query.data?.series ?? []).map((point) => ({ ts: point.ts, value: point.value }))
}

function sumSeries(
  first: readonly MetricSeriesPoint[],
  second: readonly MetricSeriesPoint[],
): MetricSeriesPoint[] {
  return first.map((point, index) => {
    const other = second[index]
    return {
      ts: point.ts,
      value:
        other && point.value !== null && other.value !== null ? point.value + other.value : null,
    }
  })
}

function formattedCount(value: number | null): string | null {
  return value === null ? null : formatCount(value)
}

function formattedBytes(value: number | null): string | null {
  return value === null ? null : formatBytes(value)
}

function formattedRate(value: number | null): string | null {
  return value === null ? null : formatRate(value)
}

interface ChartDefinition {
  ariaLabel: string
  dataTableLabel: string
  name: string
  series: readonly TimeSeriesTableSeries[]
  yAxisName?: string
  annotateResets?: boolean
}

export function OverviewSection({ database, instanceId }: OverviewSectionProps) {
  const { range } = useTimeRange(REFRESH.activity.interval)
  const step = chooseStep(range.from, range.to)
  const stepSeconds = stepMilliseconds(step) / 1_000
  const queries = useOverviewMetricQueries(instanceId, database, range.from, range.to, step)

  // Counters arrive as per-second rates: pg_xact_commit_total is commits per
  // second, pg_wal_lsn_bytes is WAL bytes per second, and so on.
  const connectionsUsed = seriesFrom(queries.connectionsUsed)
  const connectionsLimit = seriesFrom(queries.connectionsLimit)
  // Every transaction either commits or rolls back, so the tile counts both.
  const transactionRate = sumSeries(seriesFrom(queries.transactions), seriesFrom(queries.rollbacks))
  const blksHit = seriesFrom(queries.blksHit)
  const blksRead = seriesFrom(queries.blksRead)
  const checkpointRate = sumSeries(
    seriesFrom(queries.checkpointsTimed),
    seriesFrom(queries.checkpointsRequested),
  )
  const deadlockRate = seriesFrom(queries.deadlocks)
  const tempByteRate = seriesFrom(queries.tempBytes)
  const walRate = seriesFrom(queries.walRate)

  const checkpointsPerHour = scaleSeries(checkpointRate, 3_600)
  const cacheHitRatio = cacheHitRatioSeries(blksHit, blksRead)
  const latestConnectionsLimit = latestValue(connectionsLimit)
  const latestCacheHitRatio = cacheHitRatioValue(latestValue(blksHit), latestValue(blksRead))
  const checkpointsInRange = integrateRate(checkpointRate, stepSeconds)
  const deadlocksInRange = integrateRate(deadlockRate, stepSeconds)

  const charts: readonly ChartDefinition[] = [
    {
      ariaLabel: 'Connections in use and configured maximum',
      dataTableLabel: 'Connections in use and configured maximum data',
      name: 'Connections',
      series: [
        { name: 'Connections in use', points: connectionsUsed },
        { name: 'Configured maximum', points: connectionsLimit },
      ],
      yAxisName: 'connections',
    },
    {
      ariaLabel: 'Transactions per second',
      dataTableLabel: 'Transactions per second data',
      name: 'Transactions per second',
      series: [{ name: 'Transactions per second', points: transactionRate }],
      yAxisName: 'per second',
    },
    {
      annotateResets: false,
      ariaLabel: 'Cache hit ratio',
      dataTableLabel: 'Cache hit ratio data',
      name: 'Cache hit ratio',
      series: [{ name: 'Cache hit ratio', points: cacheHitRatio }],
      yAxisName: 'ratio',
    },
    {
      ariaLabel: 'WAL generation rate',
      dataTableLabel: 'WAL generation rate data',
      name: 'WAL generation',
      series: [{ name: 'WAL bytes per second', points: walRate }],
      yAxisName: 'bytes per second',
    },
    {
      ariaLabel: 'Checkpoint frequency',
      dataTableLabel: 'Checkpoint frequency data',
      name: 'Checkpoint frequency',
      series: [{ name: 'Checkpoints per hour', points: checkpointsPerHour }],
      yAxisName: 'per hour',
    },
    {
      ariaLabel: 'Temporary bytes',
      dataTableLabel: 'Temporary bytes data',
      name: 'Temporary bytes',
      series: [{ name: 'Temporary bytes per second', points: tempByteRate }],
      yAxisName: 'bytes per second',
    },
    {
      ariaLabel: 'Deadlocks',
      dataTableLabel: 'Deadlocks data',
      name: 'Deadlocks',
      series: [{ name: 'Deadlocks per second', points: deadlockRate }],
      yAxisName: 'per second',
    },
  ]

  return (
    <Section
      title="Instance overview"
      description={`Transactions, cache hit ratio, temporary bytes and deadlocks are for database ${database}; connections, WAL and checkpoints are instance-wide. Selected range step ${step}. Null buckets remain gaps; reset markers identify counter resets.`}
    >
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetricTile
          label="Connections in use"
          unit={
            <>
              of{' '}
              {latestConnectionsLimit === null ? <Unknown /> : formatCount(latestConnectionsLimit)}
            </>
          }
          value={formattedCount(latestValue(connectionsUsed))}
        />
        <MetricTile
          label="Transactions per second"
          value={formattedRate(latestValue(transactionRate))}
        />
        <MetricTile label="Cache hit ratio" value={latestCacheHitRatio} />
        <MetricTile
          label="WAL generated in range"
          value={formattedBytes(integrateRate(walRate, stepSeconds))}
        />
        <MetricTile
          label="Checkpoints in range"
          value={checkpointsInRange === null ? null : formatCount(Math.round(checkpointsInRange))}
        />
        <MetricTile
          label="Temporary bytes in range"
          value={formattedBytes(integrateRate(tempByteRate, stepSeconds))}
        />
        <MetricTile
          label="Deadlocks in range"
          value={deadlocksInRange === null ? null : formatCount(Math.round(deadlocksInRange))}
        />
      </div>

      <div className="grid gap-8 xl:grid-cols-2">
        {charts.map((chart) => (
          <article key={chart.name} className="bg-card rounded-lg border p-4 shadow-sm">
            <h3 className="mb-3 font-medium">{chart.name}</h3>
            <TimeSeriesChart
              ariaLabel={chart.ariaLabel}
              dataTableLabel={chart.dataTableLabel}
              option={buildMetricOption(chart.series, {
                ...(chart.annotateResets === undefined
                  ? {}
                  : { annotateResets: chart.annotateResets }),
                ...(chart.yAxisName === undefined ? {} : { yAxisName: chart.yAxisName }),
              })}
              series={chart.series}
            />
          </article>
        ))}
      </div>
    </Section>
  )
}
