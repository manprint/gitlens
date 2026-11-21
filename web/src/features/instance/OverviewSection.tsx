import { useTimeRange } from '@/hooks/useTimeRange'
import { stepParam } from '@/lib/chartStep'
import { queryWindowOf, type QueryWindow } from '@/lib/timerange'
import {
  cacheHitRatioValue,
  integrateRate,
  latestValue,
  type MetricSeriesPoint,
} from '@/lib/metrics'
import { formatBytes, formatCount, formatRate } from '@/lib/format'
import { useQueryMetrics } from '@/api/queries'
import { useT } from '@/i18n/react'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { MetricTile } from '@/components/layout/MetricTile'
import { Unknown } from '@/components/state/Unknown'

import { OverviewCharts } from './OverviewCharts'
import { overviewStepSeconds } from './overviewCharts'

interface OverviewSectionProps {
  instanceId: string
  database: string
}

function metricParams(
  metric: string,
  instanceId: string,
  database: string | undefined,
  step: string,
) {
  return {
    ...(database === undefined ? {} : { database }),
    instance_id: instanceId,
    metric,
    step,
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
  timeWindow: QueryWindow,
  stepOf: (metric: string) => number,
) {
  const perDatabase = (metric: string) =>
    metricParams(metric, instanceId, database, stepParam(stepOf(metric)))
  const instanceWide = (metric: string) =>
    metricParams(metric, instanceId, undefined, stepParam(stepOf(metric)))

  return {
    blksHit: useQueryMetrics(perDatabase('pg_blks_hit_total'), timeWindow),
    blksRead: useQueryMetrics(perDatabase('pg_blks_read_total'), timeWindow),
    checkpointsRequested: useQueryMetrics(
      instanceWide('pg_checkpoints_requested_total'),
      timeWindow,
    ),
    checkpointsTimed: useQueryMetrics(instanceWide('pg_checkpoints_timed_total'), timeWindow),
    connectionsLimit: useQueryMetrics(instanceWide('pg_connections_limit'), timeWindow),
    connectionsUsed: useQueryMetrics(instanceWide('pg_connections_used'), timeWindow),
    deadlocks: useQueryMetrics(perDatabase('pg_deadlocks_total'), timeWindow),
    rollbacks: useQueryMetrics(perDatabase('pg_xact_rollback_total'), timeWindow),
    tempBytes: useQueryMetrics(perDatabase('pg_temp_bytes_total'), timeWindow),
    tempFiles: useQueryMetrics(perDatabase('pg_temp_files_total'), timeWindow),
    transactions: useQueryMetrics(perDatabase('pg_xact_commit_total'), timeWindow),
    walRate: useQueryMetrics(instanceWide('pg_wal_lsn_bytes'), timeWindow),
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

/** The figure of a rate tile; its `per second` unit follows it, so no `/s` here. */
function formattedRate(value: number | null): string | null {
  return value === null ? null : formatRate(value).replace(/\/s$/, '')
}

export function OverviewSection({ database, instanceId }: OverviewSectionProps) {
  const { range } = useTimeRange()
  const t = useT()
  const timeWindow = queryWindowOf(range)
  const stepOf = (metric: string) => overviewStepSeconds(metric, range.from, range.to)
  const queries = useOverviewMetricQueries(instanceId, database, timeWindow, stepOf)
  const databaseRateStep = stepOf('pg_xact_commit_total')
  const checkpointStep = stepOf('pg_checkpoints_timed_total')
  const walStep = stepOf('pg_wal_lsn_bytes')

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
  const tempFileRate = seriesFrom(queries.tempFiles)
  const walRate = seriesFrom(queries.walRate)

  const latestConnectionsLimit = latestValue(connectionsLimit)
  const latestCacheHitRatio = cacheHitRatioValue(latestValue(blksHit), latestValue(blksRead))
  const checkpointsInRange = integrateRate(checkpointRate, checkpointStep)
  const deadlocksInRange = integrateRate(deadlockRate, databaseRateStep)
  const tempFilesInRange = integrateRate(tempFileRate, databaseRateStep)

  return (
    <>
      <Section
        actions={
          <HelpLink anchor="instance-overview#summary-tiles" label={t('instance.overview.tiles')} />
        }
        id="instance-overview"
        title={t('instance.overview.section')}
        captionId="overview.section"
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <MetricTile
            label={t('instance.overview.tile.connectionsUsed')}
            unit="connections"
            captionId="activity.connections-used"
            detail={
              <>
                {t('instance.overview.limitBefore')}{' '}
                {latestConnectionsLimit === null ? (
                  <Unknown />
                ) : (
                  formatCount(latestConnectionsLimit)
                )}{' '}
                {t('instance.overview.limitAfter')}
              </>
            }
            value={formattedCount(latestValue(connectionsUsed))}
          />
          <MetricTile
            label={t('instance.overview.tile.transactions')}
            unit="per second"
            captionId="overview.tx-rate"
            value={formattedRate(latestValue(transactionRate))}
          />
          <MetricTile
            label={t('instance.overview.tile.cacheHitRatio')}
            captionId="overview.cache-hit-ratio"
            value={latestCacheHitRatio}
          />
          <MetricTile
            label={t('instance.overview.tile.walInRange')}
            captionId="overview.wal-in-range"
            value={formattedBytes(integrateRate(walRate, walStep))}
          />
          <MetricTile
            label={t('instance.overview.tile.checkpointsInRange')}
            unit="checkpoints"
            captionId="overview.checkpoints-in-range"
            value={checkpointsInRange === null ? null : formatCount(Math.round(checkpointsInRange))}
          />
          <MetricTile
            label={t('instance.overview.tile.tempBytesInRange')}
            captionId="overview.temp-bytes-in-range"
            value={formattedBytes(integrateRate(tempByteRate, databaseRateStep))}
          />
          <MetricTile
            label={t('instance.overview.tile.tempFilesInRange')}
            unit="files"
            captionId="overview.temp-files-in-range"
            value={tempFilesInRange === null ? null : formatCount(Math.round(tempFilesInRange))}
          />
          <MetricTile
            label={t('instance.overview.tile.deadlocksInRange')}
            unit="deadlocks"
            captionId="overview.deadlocks-in-range"
            value={deadlocksInRange === null ? null : formatCount(Math.round(deadlocksInRange))}
          />
        </div>
      </Section>
      <OverviewCharts database={database} instanceId={instanceId} />
    </>
  )
}
