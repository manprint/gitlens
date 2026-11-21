import type { EChartsOption } from 'echarts'

import { formatTooltipTime } from '@/lib/chartTime'
import { formatBytes, formatDuration, formatMilliseconds, formatPercent } from '@/lib/format'
import { activeIntl } from '@/i18n/store'
import { localizedNumber } from '@/lib/format/shared'

import { sliderLabelFormatter, spanOf, timeAxisOption } from './timeAxis'
import type { TimeSeriesTableSeries } from './TimeSeriesChart'

export type MetricUnit = 'count' | 'bytes' | 'percent' | 'ratio' | 'seconds' | 'ms'

export interface MetricOptionOptions {
  yAxisName?: string
  unit?: MetricUnit
  /** Query step of the series; informational, the ChartCard states it. */
  stepSeconds?: number
}

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
        ? localizedNumber(value, activeIntl(), { maximumSignificantDigits: 2 })
        : localizedNumber(value, activeIntl(), { maximumFractionDigits: 2 })
  }
}

/** An axis tick: like the tooltip value, but a whole percentage drops its `,0`. */
export function formatAxisValue(value: number, unit: MetricUnit): string {
  if ((unit === 'percent' || unit === 'ratio') && Number.isFinite(value)) {
    return localizedNumber(unit === 'percent' ? value / 100 : value, activeIntl(), {
      style: 'percent',
      maximumFractionDigits: 1,
    })
  }
  return formatMetricValue(value, unit)
}

/** Byte steps within one binary unit, so ticks read 256 KiB, 512 KiB, 1 MiB. */
const BYTE_STEPS = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024]
/** Duration steps in milliseconds a reader counts in: 1 s, 15 s, 1 min, 5 min, 1 h… */
const DURATION_STEPS_MS = [
  1, 2, 5, 10, 20, 50, 100, 200, 500, 1_000, 2_000, 5_000, 10_000, 15_000, 30_000, 60_000, 120_000,
  300_000, 600_000, 900_000, 1_800_000, 3_600_000, 7_200_000, 10_800_000, 21_600_000, 43_200_000,
  86_400_000,
]
const AXIS_TICKS = 5

function niceStep(raw: number, unit: MetricUnit): number | undefined {
  if (unit === 'bytes') {
    if (raw < 1) return undefined
    const power = 1024 ** Math.floor(Math.log(raw) / Math.log(1024))
    return (BYTE_STEPS.find((step) => step * power >= raw) ?? 1024) * power
  }
  if (unit === 'ms' || unit === 'seconds') {
    const scale = unit === 'seconds' ? 1_000 : 1
    const rawMs = raw * scale
    const step =
      DURATION_STEPS_MS.find((candidate) => candidate >= rawMs) ??
      Math.ceil(rawMs / 86_400_000) * 86_400_000
    return step / scale
  }
  return undefined
}

/**
 * A fixed tick step and top for byte and duration axes: ECharts picks decimal
 * steps (488.3 KiB, 1.7 min), these read in the unit's own steps. Other
 * units, an empty chart or a negative value keep the automatic scale.
 */
export function axisScale(
  unit: MetricUnit | undefined,
  values: Iterable<number | null>,
): { interval: number; max: number } | Record<string, never> {
  if (unit === undefined) return {}
  let max = 0
  for (const value of values) {
    if (value === null || !Number.isFinite(value)) continue
    if (value < 0) return {}
    if (value > max) max = value
  }
  if (max <= 0) return {}
  const interval = niceStep(max / AXIS_TICKS, unit)
  return interval === undefined ? {} : { interval, max: Math.ceil(max / interval) * interval }
}

interface LineData {
  data?: readonly (readonly [unknown, number | null])[]
}

/** The values of the lines whose index passes `keep`. */
function lineValues(lines: readonly LineData[], keep: (index: number) => boolean): number[] {
  return lines.flatMap((line, index) =>
    keep(index) ? (line.data ?? []).flatMap(([, value]) => (value === null ? [] : [value])) : [],
  )
}

/** A value axis in `unit`: tick labels in the unit, and its fixed step when it has one. */
function valueAxis(base: Record<string, unknown>, unit: MetricUnit, values: number[]) {
  const rest = Object.fromEntries(
    Object.entries(base).filter(([key]) => key !== 'interval' && key !== 'max'),
  )
  return {
    ...rest,
    type: 'value' as const,
    axisLabel: { formatter: (value: number) => formatAxisValue(value, unit) },
    ...axisScale(unit, values),
    // Whole counts (checkpoints per step) never get a 1.2 tick.
    ...(unit === 'count' && values.length > 0 && values.every(Number.isInteger)
      ? { minInterval: 1 }
      : {}),
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
  const lines = option.series as (Record<string, unknown> & LineData)[]
  return {
    ...option,
    tooltip: { ...(option.tooltip as object), formatter: mixedUnitTooltip(units) },
    yAxis: [
      valueAxis(
        option.yAxis as Record<string, unknown>,
        primary,
        lineValues(lines, (index) => units[index] === primary),
      ),
      valueAxis(
        { splitLine: { show: false } },
        secondary,
        lineValues(lines, (index) => units[index] !== primary),
      ),
    ],
    series: lines.map((line, index) =>
      units[index] === primary ? line : { ...line, yAxisIndex: 1 },
    ),
  }
}

/**
 * Width a legend may use in the narrowest chart card (a half-width card at
 * 1024 px, a full-width one at 390 px), and the width of one legend entry:
 * its icon and gap plus about 7 px per character at 12 px.
 */
const LEGEND_WIDTH_PX = 280
const LEGEND_ICON_PX = 35
const LEGEND_CHAR_PX = 7
const LEGEND_ROW_PX = 22

/**
 * Rows a wrapping legend takes in the narrowest card. The legend wraps
 * instead of scrolling, so no series hides behind paging; the plot starts
 * below the last row.
 */
export function legendRows(names: readonly string[]): number {
  let rows = 1
  let used = 0
  for (const name of names) {
    const width = LEGEND_ICON_PX + name.length * LEGEND_CHAR_PX
    if (used > 0 && used + width > LEGEND_WIDTH_PX) {
      rows += 1
      used = 0
    }
    used += width
  }
  return rows
}

/** Build a null-preserving option for an instance metric series or series group. */
export function buildMetricOption(
  series: readonly TimeSeriesTableSeries[],
  { yAxisName, unit }: MetricOptionOptions = {},
): EChartsOption {
  const spanMs = spanOf(series.flatMap((metric) => metric.points.map((point) => point.ts)))

  const metricSeries = series.map((metric) => ({
    type: 'line' as const,
    name: metric.name,
    data: metric.points.map((point) => [point.ts, point.value] as [string, number | null]),
    connectNulls: false,
    showSymbol: false,
    smooth: false,
  }))

  return {
    animation: false,
    grid: {
      bottom: 72,
      containLabel: true,
      left: 56,
      right: 24,
      top: 40 + (legendRows(series.map(({ name }) => name)) - 1) * LEGEND_ROW_PX,
    },
    legend: { data: series.map(({ name }) => name), top: 8, type: 'plain' },
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
    yAxis: unit
      ? valueAxis(
          yAxisName ? { name: yAxisName } : {},
          unit,
          lineValues(metricSeries, () => true),
        )
      : { type: 'value', ...(yAxisName ? { name: yAxisName } : {}) },
    dataZoom: [
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
