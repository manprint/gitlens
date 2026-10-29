import type { EChartsOption } from 'echarts'
import type { ReactNode } from 'react'

import { useQueryMetrics } from '@/api/queries'
import { ChartCard, type ChartCardState } from '@/components/charts/ChartCard'
import { ChartGroup } from '@/components/charts/ChartGroup'
import {
  buildMetricOption,
  tooltipHtml,
  tooltipTime,
  tooltipValue,
  type AxisTooltipParam,
  type MetricUnit,
} from '@/components/charts/metric.options'
import { TimeSeriesChart, type TimeSeriesTableSeries } from '@/components/charts/TimeSeriesChart'
import { useTimeRange } from '@/hooks/useTimeRange'
import { SAMPLE_SECONDS, stepFor, stepParam } from '@/lib/chartStep'
import { formatTooltipTime } from '@/lib/chartTime'
import { niceCeil, subtractSeries, toPercent } from '@/lib/hostSeries'
import type { MetricSeriesPoint } from '@/lib/metrics'
import { queryWindowOf, type QueryWindow } from '@/lib/timerange'

/** Charts of the Host section share their tooltip pointer and zoom. */
export const HOST_CHART_GROUP = 'host-charts'

export const FILESYSTEM_HINT =
  'Filesystem metrics need the PostgreSQL data directory mounted read-only at the same path in the agent container.'

export const NO_SWAP_TEXT =
  'No swap is configured: the host reports a swap total of 0 bytes, so there is nothing to chart.'

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

type AxisParam = AxisTooltipParam & { seriesIndex?: number }

/** Tooltip of a chart whose series have different units, one unit per series index. */
function mixedUnitTooltip(units: readonly MetricUnit[]) {
  return (params: unknown) => {
    const list = (Array.isArray(params) ? params : [params]) as AxisParam[]
    const ms = tooltipTime(list[0])
    const time = Number.isFinite(ms) ? `<div>${formatTooltipTime(ms)}</div>` : ''
    const rows = list.map((param) =>
      tooltipHtml(
        Number.NaN,
        [
          {
            seriesName: param.seriesName ?? '',
            marker: typeof param.marker === 'string' ? param.marker : '',
            value: tooltipValue(param.value),
          },
        ],
        units[param.seriesIndex ?? 0],
      ),
    )
    return time + rows.join('')
  }
}

function percentAxis(option: EChartsOption): EChartsOption {
  return { ...option, yAxis: { ...(option.yAxis as object), min: 0, max: 100 } }
}

function cpuOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  return percentAxis(buildMetricOption(series, { annotateResets: false, unit: 'percent' }))
}

/**
 * Load average lines with a horizontal reference at the CPU count. The line
 * sits on its own empty series, outside the legend, so hiding a load series
 * keeps it.
 */
function loadOption(series: readonly TimeSeriesTableSeries[], cpuCount: number | null) {
  const option = buildMetricOption(series, { annotateResets: false })
  if (cpuCount === null) return option
  const lines = option.series as Record<string, unknown>[]
  return {
    ...option,
    // A reference line does not stretch the axis, so the top leaves room for it.
    yAxis: {
      ...(option.yAxis as object),
      max: ({ max }: { max: number }) => niceCeil(Math.max(max, cpuCount) * 1.1),
    },
    series: [
      ...lines,
      {
        type: 'line',
        name: 'CPU count',
        data: [],
        markLine: {
          silent: true,
          symbol: 'none',
          label: { formatter: `CPU count ${cpuCount}`, position: 'insideEndTop' },
          lineStyle: { type: 'dashed', width: 1 },
          data: [{ name: 'CPU count', yAxis: cpuCount }],
        },
      },
    ],
  } satisfies EChartsOption
}

function bytesOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  return buildMetricOption(series, { annotateResets: false, unit: 'bytes' })
}

/** Used bytes on the left axis, free share on a 0–100 % right axis. */
function filesystemOption(series: readonly TimeSeriesTableSeries[]): EChartsOption {
  const option = buildMetricOption(series, { annotateResets: false, unit: 'bytes' })
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
  unitHint,
  queries,
  series,
  option,
  stepSeconds,
  range,
  replacement,
}: HostCardProps) {
  const { state, failed } = cardState(queries, series)
  const common = {
    range,
    stepSeconds,
    title,
    ...(unitHint === undefined ? {} : { unitHint }),
  }
  if (replacement !== undefined) {
    return (
      <ChartCard {...common} state="ready">
        {replacement}
      </ChartCard>
    )
  }
  return (
    <ChartCard
      {...common}
      state={state}
      {...(failed?.error ? { error: failed.error, onRetry: () => void failed.refetch() } : {})}
    >
      <TimeSeriesChart
        ariaLabel={title}
        dataTableLabel={`${title} data`}
        group={HOST_CHART_GROUP}
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
  const { range } = useTimeRange()
  const window = queryWindowOf(range)
  const step = stepFor('gauge', range.from, range.to, SAMPLE_SECONDS.host)
  // Each query resolves its own "now", so buckets of two series can be offset.
  const tolerance = (step * 1_000) / 2
  const cpuUsed = useHostMetric('host_cpu_used_ratio', instanceId, step, window)
  const cpuCount = useHostMetric('host_cpu_count', instanceId, step, window)
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

  const cpuSeries = [{ name: 'CPU used', points: toPercent(seriesFrom(cpuUsed)) }]
  const loadSeries = [
    { name: 'Load 1m', points: seriesFrom(load1) },
    { name: 'Load 5m', points: seriesFrom(load5) },
    { name: 'Load 15m', points: seriesFrom(load15) },
  ]
  const memoryTotal = seriesFrom(memTotal)
  const memorySeries = [
    { name: 'Used', points: subtractSeries(memoryTotal, seriesFrom(memAvailable), tolerance) },
    { name: 'Total', points: memoryTotal },
  ]
  const swapSeries = [
    { name: 'Used', points: seriesFrom(swapUsed) },
    { name: 'Total', points: seriesFrom(swapTotal) },
  ]
  const filesystemSeries = [
    {
      name: 'Used',
      points: subtractSeries(seriesFrom(diskTotal), seriesFrom(diskFree), tolerance),
    },
    { name: 'Free (%)', points: toPercent(seriesFrom(diskFreeRatio)) },
  ]
  const latestCpuCount = latestMeasured(seriesFrom(cpuCount))

  const common = { range, stepSeconds: step }

  return (
    <ChartGroup
      description="Readings over the selected range. Charts share their pointer and zoom; null buckets remain gaps."
      id="host"
      title="Host charts"
    >
      <HostCard
        {...common}
        option={cpuOption(cpuSeries)}
        queries={[cpuUsed]}
        series={cpuSeries}
        title="CPU used (%)"
      />
      <HostCard
        {...common}
        option={loadOption(loadSeries, latestCpuCount)}
        queries={[load1, load5, load15]}
        series={loadSeries}
        title="Load average"
      />
      <HostCard
        {...common}
        option={bytesOption(memorySeries)}
        queries={[memTotal, memAvailable]}
        series={memorySeries}
        title="Memory"
        unitHint="bytes"
      />
      <HostCard
        {...common}
        option={bytesOption(swapSeries)}
        queries={[swapUsed, swapTotal]}
        series={swapSeries}
        title="Swap"
        unitHint="bytes"
        {...(swapAbsent ? { replacement: <Explanation>{NO_SWAP_TEXT}</Explanation> } : {})}
      />
      <HostCard
        {...common}
        option={filesystemOption(filesystemSeries)}
        queries={[diskTotal, diskFree, diskFreeRatio]}
        series={filesystemSeries}
        title="Filesystem"
        unitHint="used bytes, free %"
        {...(diskMeasured ? {} : { replacement: <Explanation>{FILESYSTEM_HINT}</Explanation> })}
      />
    </ChartGroup>
  )
}

function latestMeasured(series: readonly MetricSeriesPoint[]): number | null {
  for (let index = series.length - 1; index >= 0; index -= 1) {
    const value = series[index]?.value
    if (value !== null && value !== undefined) return value
  }
  return null
}
