import { useTimeRange } from '@/hooks/useTimeRange'
import { chooseStep } from '@/lib/timerange'
import {
  cacheHitRatioSeries,
  cacheHitRatioValue,
  counterRateSeries,
  latestValue,
  rangeDelta,
  type MetricSeriesPoint,
} from '@/lib/metrics'
import { formatBytes, formatCount } from '@/lib/format'
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
  database: string,
  from: Date,
  to: Date,
  step: string,
) {
  return {
    database,
    from: from.toISOString(),
    instance_id: instanceId,
    metric,
    step,
    to: to.toISOString(),
  }
}

function useOverviewMetricQueries(
  instanceId: string,
  database: string,
  from: Date,
  to: Date,
  step: string,
) {
  const params = (metric: string) => metricParams(metric, instanceId, database, from, to, step)

  return {
    blksHit: useQueryMetrics(params('pg_blks_hit_total')),
    blksRead: useQueryMetrics(params('pg_blks_read_total')),
    checkpointsRequested: useQueryMetrics(params('pg_checkpoints_requested_total')),
    checkpointsTimed: useQueryMetrics(params('pg_checkpoints_timed_total')),
    connectionsLimit: useQueryMetrics(params('pg_connections_limit')),
    connectionsUsed: useQueryMetrics(params('pg_connections_used')),
    deadlocks: useQueryMetrics(params('pg_deadlocks_total')),
    tempBytes: useQueryMetrics(params('pg_temp_bytes_total')),
    transactions: useQueryMetrics(params('pg_xact_commit_total')),
    walPosition: useQueryMetrics(params('pg_wal_lsn_bytes')),
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

interface ChartDefinition {
  ariaLabel: string
  dataTableLabel: string
  name: string
  series: readonly TimeSeriesTableSeries[]
  yAxisName?: string
  annotateResets?: boolean
}

export function OverviewSection({ database, instanceId }: OverviewSectionProps) {
  const { range } = useTimeRange()
  const step = chooseStep(range.from, range.to)
  const queries = useOverviewMetricQueries(instanceId, database, range.from, range.to, step)

  const connectionsUsed = seriesFrom(queries.connectionsUsed)
  const connectionsLimit = seriesFrom(queries.connectionsLimit)
  const transactions = seriesFrom(queries.transactions)
  const blksHit = seriesFrom(queries.blksHit)
  const blksRead = seriesFrom(queries.blksRead)
  const checkpoints = sumSeries(
    seriesFrom(queries.checkpointsTimed),
    seriesFrom(queries.checkpointsRequested),
  )
  const deadlocks = seriesFrom(queries.deadlocks)
  const tempBytes = seriesFrom(queries.tempBytes)
  const walPosition = seriesFrom(queries.walPosition)

  const transactionRate = counterRateSeries(transactions)
  const checkpointRate = counterRateSeries(checkpoints)
  const cacheHitRatio = cacheHitRatioSeries(blksHit, blksRead)
  const latestConnectionsLimit = latestValue(connectionsLimit)
  const latestCacheHitRatio = cacheHitRatioValue(latestValue(blksHit), latestValue(blksRead))

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
      ariaLabel: 'WAL generated in selected range',
      dataTableLabel: 'WAL position data',
      name: 'WAL position',
      series: [{ name: 'WAL position', points: walPosition }],
      yAxisName: 'bytes',
    },
    {
      ariaLabel: 'Checkpoint frequency',
      dataTableLabel: 'Checkpoint frequency data',
      name: 'Checkpoint frequency',
      series: [{ name: 'Checkpoints per second', points: checkpointRate }],
      yAxisName: 'per second',
    },
    {
      ariaLabel: 'Temporary bytes',
      dataTableLabel: 'Temporary bytes data',
      name: 'Temporary bytes',
      series: [{ name: 'Temporary bytes', points: tempBytes }],
      yAxisName: 'bytes',
    },
    {
      ariaLabel: 'Deadlocks',
      dataTableLabel: 'Deadlocks data',
      name: 'Deadlocks',
      series: [{ name: 'Deadlocks', points: deadlocks }],
      yAxisName: 'count',
    },
  ]

  return (
    <Section
      title="Instance overview"
      description={`Database ${database} · selected range step ${step}. Null buckets remain gaps; reset markers identify counter resets.`}
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
          unit="per second"
          value={latestValue(transactionRate)}
        />
        <MetricTile label="Cache hit ratio" value={latestCacheHitRatio} />
        <MetricTile
          label="WAL generated in range"
          unit="bytes"
          value={formattedBytes(rangeDelta(walPosition))}
        />
        <MetricTile
          label="Checkpoint frequency"
          unit="per second"
          value={latestValue(checkpointRate)}
        />
        <MetricTile
          label="Temporary bytes"
          unit="bytes"
          value={formattedBytes(rangeDelta(tempBytes))}
        />
        <MetricTile label="Deadlocks" value={formattedCount(rangeDelta(deadlocks))} />
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
