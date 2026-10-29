import type { EChartsOption } from 'echarts'

import { formatTooltipTime } from '@/lib/chartTime'
import { formatBytes, formatDuration, formatMilliseconds, formatPercent } from '@/lib/format'
import { localizedNumber } from '@/lib/format/shared'
import { findResets } from '@/lib/metrics'

import { sliderLabelFormatter, spanOf, timeAxisOption } from './timeAxis'
import type { TimeSeriesTableSeries } from './TimeSeriesChart'

export type MetricUnit = 'count' | 'bytes' | 'percent' | 'ratio' | 'seconds' | 'ms'

export interface MetricOptionOptions {
  annotateResets?: boolean
  yAxisName?: string
  unit?: MetricUnit
  /** Query step of the series; informational, the ChartCard states it. */
  stepSeconds?: number
}

/** Above this many resets in one series no line is drawn; the card states the count instead. */
export const MAX_RESET_LINES = 12

const MISSING = '—'

/** A value in the chart's unit; `—` for a missing bucket. */
export function formatMetricValue(value: number | null | undefined, unit: MetricUnit = 'count') {
  if (value === null || value === undefined || !Number.isFinite(value)) return MISSING
  switch (unit) {
    case 'bytes':
      return formatBytes(value)
    case 'percent':
      return formatPercent(value, 100) ?? MISSING
    case 'ratio':
      return formatPercent(value, 1) ?? MISSING
    case 'seconds':
      return formatDuration(value)
    case 'ms':
      return formatMilliseconds(value)
    case 'count':
      // Small rates (0.004 rows/s) keep two significant digits, so axis ticks
      // stay distinct instead of all rounding to 0 or 0.01.
      return Math.abs(value) > 0 && Math.abs(value) < 1
        ? localizedNumber(value, 'en-US', { maximumSignificantDigits: 2 })
        : localizedNumber(value, 'en-US', { maximumFractionDigits: 2 })
  }
}

export interface TooltipRow {
  seriesName: string
  marker?: string
  value: number | null
}

const escapeHtml = (text: string) => text.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`)

/** Tooltip body: the UTC time, then one row per series in the chart's unit. */
export function tooltipHtml(ms: number, rows: readonly TooltipRow[], unit?: MetricUnit): string {
  const lines = rows.map(
    (row) =>
      `<div>${row.marker ?? ''}${escapeHtml(row.seriesName)}: <strong>${escapeHtml(
        formatMetricValue(row.value, unit),
      )}</strong></div>`,
  )
  // An unparseable time drops the time row instead of throwing in the tooltip.
  const time = Number.isFinite(ms) ? [`<div>${formatTooltipTime(ms)}</div>`] : []
  return [...time, ...lines].join('')
}

export interface AxisTooltipParam {
  seriesName?: string
  marker?: unknown
  value?: unknown
  axisValue?: unknown
}

/** The value of an axis tooltip parameter, `[ts, value]` or a bare value. */
export function tooltipValue(value: unknown): number | null {
  const raw = Array.isArray(value) ? (value as unknown[])[1] : value
  return typeof raw === 'number' && Number.isFinite(raw) ? raw : null
}

/** The time of an axis tooltip parameter in milliseconds; NaN when unknown. */
export function tooltipTime(param: AxisTooltipParam | undefined): number {
  const axis = param?.axisValue
  if (typeof axis === 'number') return axis
  if (typeof axis === 'string') return Date.parse(axis)
  const first = Array.isArray(param?.value) ? (param.value as unknown[])[0] : undefined
  return typeof first === 'string' ? Date.parse(first) : typeof first === 'number' ? first : NaN
}

type AxisParam = AxisTooltipParam & { seriesIndex?: number }

/** Tooltip of a chart whose series have different units, one unit per series index. */
export function mixedUnitTooltip(units: readonly MetricUnit[]) {
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

/**
 * Lines whose unit differs from the first line's move to a right axis in that
 * unit (one secondary unit per chart); the tooltip shows each line in its unit.
 */
export function withSecondaryAxis(
  option: EChartsOption,
  units: readonly MetricUnit[],
): EChartsOption {
  const primary = units[0]
  const secondary = units.find((unit) => unit !== primary)
  if (primary === undefined || secondary === undefined) return option
  const lines = option.series as Record<string, unknown>[]
  return {
    ...option,
    tooltip: { ...(option.tooltip as object), formatter: mixedUnitTooltip(units) },
    yAxis: [
      option.yAxis as object,
      {
        type: 'value',
        splitLine: { show: false },
        axisLabel: { formatter: (value: number) => formatMetricValue(value, secondary) },
      },
    ],
    series: lines.map((line, index) =>
      units[index] === primary ? line : { ...line, yAxisIndex: 1 },
    ),
  }
}

/** Build a null-preserving option for an instance metric series or series group. */
export function buildMetricOption(
  series: readonly TimeSeriesTableSeries[],
  { annotateResets = true, yAxisName, unit }: MetricOptionOptions = {},
): EChartsOption {
  const spanMs = spanOf(series.flatMap((metric) => metric.points.map((point) => point.ts)))

  const metricSeries = series.map((metric) => {
    const optionSeries = {
      type: 'line' as const,
      name: metric.name,
      data: metric.points.map((point) => [point.ts, point.value] as [string, number | null]),
      connectNulls: false,
      showSymbol: false,
      smooth: false,
    }

    if (!annotateResets) return optionSeries

    const resets = findResets(metric.points)
    if (resets.length === 0 || resets.length > MAX_RESET_LINES) return optionSeries

    return {
      ...optionSeries,
      markLine: {
        silent: true,
        symbol: 'none',
        label: { show: false },
        lineStyle: { type: 'dashed' as const, width: 1, opacity: 0.5 },
        data: resets.map((index) => ({ xAxis: metric.points[index]?.ts ?? '' })),
      },
    }
  })

  return {
    animation: false,
    grid: { bottom: 72, containLabel: true, left: 56, right: 24, top: 40 },
    legend: { data: series.map(({ name }) => name), top: 8, type: 'scroll' },
    tooltip: {
      trigger: 'axis',
      confine: true,
      axisPointer: { type: 'line' },
      formatter: (params: unknown) => {
        const list = (Array.isArray(params) ? params : [params]) as AxisTooltipParam[]
        return tooltipHtml(
          tooltipTime(list[0]),
          list.map((param) => ({
            seriesName: param.seriesName ?? '',
            marker: typeof param.marker === 'string' ? param.marker : '',
            value: tooltipValue(param.value),
          })),
          unit,
        )
      },
    },
    xAxis: timeAxisOption(spanMs),
    yAxis: {
      type: 'value',
      ...(yAxisName ? { name: yAxisName } : {}),
      ...(unit
        ? { axisLabel: { formatter: (value: number) => formatMetricValue(value, unit) } }
        : {}),
    },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, filterMode: 'none', throttle: 50 },
      {
        type: 'slider',
        xAxisIndex: 0,
        filterMode: 'none',
        bottom: 8,
        labelFormatter: sliderLabelFormatter(spanMs),
      },
    ],
    series: metricSeries,
  }
}
