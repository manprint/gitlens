import type { EChartsOption } from 'echarts'
import type { ReactNode } from 'react'

import { useT } from '@/i18n/react'
import { useQueryMetrics } from '@/api/queries'
import { ChartCard, type ChartCardState } from '@/components/charts/ChartCard'
import { ChartGroup } from '@/components/charts/ChartGroup'
import { buildMetricOption, mixedUnitTooltip } from '@/components/charts/metric.options'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { useTimeRange } from '@/hooks/useTimeRange'
import type { CaptionId } from '@/lib/captions'
import { SAMPLE_SECONDS, stepFor, stepParam } from '@/lib/chartStep'
import { subtractSeries, toPercent } from '@/lib/hostSeries'
import type { MetricSeriesPoint } from '@/lib/metrics'
import { queryWindowOf, type QueryWindow } from '@/lib/timerange'

interface HostChartsProps {
  instanceId: string
  /** The latest host reading has filesystem totals (the data directory is measured). */
  diskMeasured: boolean
  /** The latest host reading reports no swap at all. */
  swapAbsent: boolean
}

type MetricQuery = ReturnType<typeof useQueryMetrics>

function useHostMetric(metric: string, instanceId: string, step: number, window: QueryWindow) {
  return useQueryMetrics({ instance_id: instanceId, metric, step: stepParam(step) }, window)
}

function seriesFrom(query: MetricQuery): MetricSeriesPoint[] {
  return (query.data?.series ?? []).map((point) => ({ ts: point.ts, value: point.value }))
}

/** Loading until every query answered, error if one failed, empty without a single value. */
function cardState(queries: readonly MetricQuery[], series: readonly TimeSeriesTableSeries[]) {
  const failed = queries.find((query) => query.error && !query.data)
  if (failed) return { state: 'error' as ChartCardState, failed }
  if (queries.some((query) => query.isPending)) return { state: 'loading' as ChartCardState }
  const measured = series.some((item) => item.points.some((point) => point.value !== null))
  return { state: (measured ? 'ready' : 'empty') as ChartCardState }
}

function percentAxis(option: EChartsOption): EChartsOption {
  return { ...option, yAxis: { ...(option.yAxis as object), min: 0, max: 100 } }
}

function cpuOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  return percentAxis(buildMetricOption(series, { unit: 'percent' }))
}

function bytesOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  return buildMetricOption(series, { unit: 'bytes' })
}

/** Used bytes on the left axis, free share on a 0–100 % right axis. */
function filesystemOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  const option = buildMetricOption(series, { unit: 'bytes' })
  const lines = option.series as Record<string, unknown>[]
  return {
    ...option,
    tooltip: { ...(option.tooltip as object), formatter: mixedUnitTooltip(['bytes', 'percent']) },
    yAxis: [
      option.yAxis as object,
      {
        type: 'value',
        min: 0,
        max: 100,
        splitLine: { show: false },
        axisLabel: { formatter: '{value}%' },
      },
    ],
    series: lines.map((line, index) => (index === 1 ? { ...line, yAxisIndex: 1 } : line)),
  }
}

interface HostCardProps {
  title: string
  captionId: CaptionId
  unitHint?: string
  queries: readonly MetricQuery[]
  series: readonly TimeSeriesTableSeries[]
  option: EChartsOption
  stepSeconds: number
  range: { from: Date; to: Date }
  /** Shown instead of the chart when the reading says there is nothing to chart. */
  replacement?: ReactNode
}

function HostCard({
  title,
  captionId,
  unitHint,
  queries,
  series,
  option,
  stepSeconds,
  range,
  replacement,
}: HostCardProps) {
  const t = useT()
  const { state, failed } = cardState(queries, series)
  const common = {
    range,
    stepSeconds,
    title,
    ...(unitHint === undefined ? {} : { unitHint }),
  }
  if (replacement !== undefined) {
    return (
      <ChartCard {...common} captionId={captionId} state="ready">
        {replacement}
      </ChartCard>
    )
  }
  return (
    <ChartCard
      {...common}
      captionId={captionId}
      state={state}
      {...(failed?.error ? { error: failed.error, onRetry: () => void failed.refetch() } : {})}
    >
      <TimeSeriesChart
        ariaLabel={title}
        dataTableLabel={t('instance.host.chart.dataTable', { title })}
        option={option}
        series={series}
      />
    </ChartCard>
  )
}

function Explanation({ children }: { children: ReactNode }) {
  return (
    <p className="text-muted-foreground bg-muted/40 rounded-lg p-4 text-sm" role="note">
      {children}
    </p>
  )
}

/** Time-series charts of the host readings, one card per resource. */
export function HostCharts({ diskMeasured, instanceId, swapAbsent }: HostChartsProps) {
  const t = useT()
  const { range } = useTimeRange()
  const window = queryWindowOf(range)
  const step = stepFor('gauge', range.from, range.to, SAMPLE_SECONDS.host)
  // Each query resolves its own "now", so buckets of two series can be offset.
  const tolerance = (step * 1_000) / 2
  const cpuUsed = useHostMetric('host_cpu_used_ratio', instanceId, step, window)
  const load1 = useHostMetric('host_load1', instanceId, step, window)
  const load5 = useHostMetric('host_load5', instanceId, step, window)
  const load15 = useHostMetric('host_load15', instanceId, step, window)
  const memTotal = useHostMetric('host_mem_total_bytes', instanceId, step, window)
  const memAvailable = useHostMetric('host_mem_available_bytes', instanceId, step, window)
  const swapUsed = useHostMetric('host_swap_used_bytes', instanceId, step, window)
  const swapTotal = useHostMetric('host_swap_total_bytes', instanceId, step, window)
  const diskTotal = useHostMetric('host_disk_total_bytes', instanceId, step, window)
  const diskFree = useHostMetric('host_disk_free_bytes', instanceId, step, window)
  const diskFreeRatio = useHostMetric('host_disk_free_ratio', instanceId, step, window)

  const cpuSeries = [
    { name: t('instance.host.chart.series.cpuUsed'), points: toPercent(seriesFrom(cpuUsed)) },
  ]
  const loadSeries = [
    { name: t('instance.host.chart.series.load1'), points: seriesFrom(load1) },
    { name: t('instance.host.chart.series.load5'), points: seriesFrom(load5) },
    { name: t('instance.host.chart.series.load15'), points: seriesFrom(load15) },
  ]
  const memoryTotal = seriesFrom(memTotal)
  const memorySeries = [
    {
      name: t('instance.host.chart.series.used'),
      points: subtractSeries(memoryTotal, seriesFrom(memAvailable), tolerance),
    },
    { name: t('instance.host.chart.series.total'), points: memoryTotal },
  ]
  const swapSeries = [
    { name: t('instance.host.chart.series.used'), points: seriesFrom(swapUsed) },
    { name: t('instance.host.chart.series.total'), points: seriesFrom(swapTotal) },
  ]
  const filesystemSeries = [
    {
      name: t('instance.host.chart.series.used'),
      points: subtractSeries(seriesFrom(diskTotal), seriesFrom(diskFree), tolerance),
    },
    { name: t('instance.host.chart.series.free'), points: toPercent(seriesFrom(diskFreeRatio)) },
  ]

  const common = { range, stepSeconds: step }

  return (
    <ChartGroup captionId="host.charts" id="host" title={t('instance.host.chart.group')}>
      <HostCard
        {...common}
        option={cpuOption(cpuSeries)}
        queries={[cpuUsed]}
        series={cpuSeries}
        captionId="host.chart-cpu"
        title={t('instance.host.chart.cpuUsedTitle')}
      />
      <HostCard
        {...common}
        option={buildMetricOption(loadSeries)}
        queries={[load1, load5, load15]}
        series={loadSeries}
        captionId="host.chart-load"
        title={t('instance.host.chart.loadTitle')}
      />
      <HostCard
        {...common}
        option={bytesOption(memorySeries)}
        queries={[memTotal, memAvailable]}
        series={memorySeries}
        captionId="host.chart-memory"
        title={t('instance.host.chart.memoryTitle')}
        unitHint={t('instance.host.chart.unitBytes')}
      />
      <HostCard
        {...common}
        option={bytesOption(swapSeries)}
        queries={[swapUsed, swapTotal]}
        series={swapSeries}
        captionId="host.chart-swap"
        title={t('instance.host.chart.swapTitle')}
        unitHint={t('instance.host.chart.unitBytes')}
        {...(swapAbsent
          ? { replacement: <Explanation>{t('instance.host.chart.noSwap')}</Explanation> }
          : {})}
      />
      <HostCard
        {...common}
        option={filesystemOption(filesystemSeries)}
        queries={[diskTotal, diskFree, diskFreeRatio]}
        series={filesystemSeries}
        captionId="host.chart-filesystem"
        title={t('instance.host.chart.filesystemTitle')}
        unitHint={t('instance.host.chart.usedFreeHint')}
        {...(diskMeasured
          ? {}
          : { replacement: <Explanation>{t('instance.host.chart.filesystemHint')}</Explanation> })}
      />
    </ChartGroup>
  )
}
