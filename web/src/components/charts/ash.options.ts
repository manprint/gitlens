import type { EChartsOption } from 'echarts'

import type { AshPoint, AshSeries } from '@/lib/ash'
import { formatTooltipTime } from '@/lib/chartTime'

import { sliderLabelFormatter, spanOf, timeAxisOption } from './timeAxis'

export const ASH_WAIT_TYPES = [
  'Lock',
  'IO',
  'CPU',
  'LWLock',
  'Client',
  'IPC',
  'Timeout',
  'BufferPin',
  // PostgreSQL 19 replaced BufferPin (and LWLock BufferContent) with the
  // Buffer type; both get the same colour across an upgrade.
  'Buffer',
  'Extension',
  'Activity',
  'other',
] as const

export type AshPaletteName = (typeof ASH_WAIT_TYPES)[number]

/**
 * One wait-event token per type keeps a type the same colour between polls,
 * and keeps stacked neighbours apart. The tokens are defined in index.css.
 */
export const ASH_PALETTE: Record<AshPaletteName, string> = {
  Activity: 'var(--wait-activity)',
  Buffer: 'var(--wait-buffer)',
  BufferPin: 'var(--wait-buffer)',
  CPU: 'var(--wait-cpu)',
  Client: 'var(--wait-client)',
  Extension: 'var(--wait-extension)',
  IO: 'var(--wait-io)',
  IPC: 'var(--wait-ipc)',
  LWLock: 'var(--wait-lwlock)',
  Lock: 'var(--wait-lock)',
  Timeout: 'var(--wait-timeout)',
  other: 'var(--wait-other)',
}

export const OTHER_FOLDED_LABEL = 'other (folded)'

export interface AshChartPoint extends AshPoint {
  samples?: number | null
  ticks?: number | null
}

export interface AshChartSeries {
  name: string
  points: readonly AshChartPoint[]
}

export interface AshOptionOptions {
  /** The selected time range; the time axis spans it rather than the data alone. */
  window?: { from: Date; to: Date } | undefined
  /** Host CPU count; absent or non-positive values omit the reference line. */
  cpuCount?: number | null
  /** Number of source groups folded into the `other` series. */
  foldedCount?: number
}

interface TooltipParameter {
  dataIndex?: unknown
  seriesName?: unknown
  value?: unknown
}

function asTooltipParameters(value: unknown): readonly TooltipParameter[] {
  if (Array.isArray(value)) return value as TooltipParameter[]
  return [value as TooltipParameter]
}

function escapeHtml(value: string): string {
  return value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
}

export function displayName(name: string): string {
  return name === 'other' ? OTHER_FOLDED_LABEL : name
}

/** The colour token of a wait event type; unknown types take the `other` colour. */
export function paletteFor(name: string): string {
  return ASH_PALETTE[name as AshPaletteName] ?? ASH_PALETTE.other
}

/** Distinct tokens for wait events, which share their type's colour otherwise. */
export const ASH_EVENT_COLORS: readonly string[] = [
  'var(--wait-io)',
  'var(--wait-lock)',
  'var(--wait-cpu)',
  'var(--wait-lwlock)',
  'var(--wait-timeout)',
  'var(--wait-ipc)',
  'var(--wait-client)',
  'var(--wait-extension)',
  'var(--wait-buffer)',
  'var(--wait-activity)',
]

function formatMetric(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) : 'Unknown'
}

function formatCount(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : 'Unknown'
}

function pointValue(value: unknown): string {
  if (!Array.isArray(value)) return 'Unknown'
  const timestamp = (value as readonly unknown[])[0]
  return typeof timestamp === 'string' || typeof timestamp === 'number'
    ? String(timestamp)
    : 'Unknown'
}

function tupleValue(value: unknown, index: number): unknown {
  return Array.isArray(value) ? (value as readonly unknown[])[index] : undefined
}

function tooltipFor(
  params: unknown,
  metadata: ReadonlyMap<string, AshChartPoint>,
  foldedCount: number,
): string {
  const entries = asTooltipParameters(params)
  const first = entries[0]
  const timestamp = pointValue(first?.value)
  const rows = entries.map((entry) => {
    const name = typeof entry.seriesName === 'string' ? entry.seriesName : 'Unknown'
    const value = tupleValue(entry.value, 1)
    const metadataPoint = metadata.get(`${name}\u0000${timestamp}`)
    const samples = metadataPoint?.samples
    const ticks = metadataPoint?.ticks
    return (
      `<div><strong>${escapeHtml(name)}</strong>: ${formatMetric(value)} avg active backends ` +
      `(samples: ${formatCount(samples)}, ticks: ${formatCount(ticks)})</div>`
    )
  })
  const folded =
    foldedCount > 0 ? `<div>${OTHER_FOLDED_LABEL}: ${foldedCount} groups folded</div>` : ''
  // The lookup keeps the raw timestamp; the reader sees the UTC tooltip form.
  const parsed = Date.parse(timestamp)
  const time = Number.isFinite(parsed) ? formatTooltipTime(parsed) : timestamp
  return `<div>${escapeHtml(time)}</div>${rows.join('')}${folded}`
}

function validCpuCount(value: number | null | undefined): value is number {
  return value !== null && value !== undefined && Number.isFinite(value) && value > 0
}

export function orderAshSeries<S extends AshChartSeries | AshSeries>(series: readonly S[]): S[] {
  return [...series].sort((left, right) => {
    if (left.name === 'CPU' && right.name !== 'CPU') return -1
    if (left.name !== 'CPU' && right.name === 'CPU') return 1
    if (left.name === 'other' && right.name !== 'other') return 1
    if (left.name !== 'other' && right.name === 'other') return -1
    return left.name.localeCompare(right.name)
  })
}

function tooltipMetadata(
  series: readonly (AshChartSeries | AshSeries)[],
): Map<string, AshChartPoint> {
  const metadata = new Map<string, AshChartPoint>()
  for (const item of series) {
    for (const point of item.points) {
      metadata.set(`${displayName(item.name)}\u0000${point.ts}`, point)
    }
  }
  return metadata
}

/** Span of the time axis: the selected window when given, else the data. */
function axisSpan(window: AshOptionOptions['window'], dataSpanMs: number): number {
  return window ? window.to.getTime() - window.from.getTime() : dataSpanMs
}

/** The shared time axis, pinned to the selected window when one is given. */
function ashTimeAxis(spanMs: number, window: AshOptionOptions['window']) {
  const axis = timeAxisOption(spanMs)
  return window ? { ...axis, min: window.from.getTime(), max: window.to.getTime() } : axis
}

function safeFolded(foldedCount: number): number {
  return Number.isFinite(foldedCount) ? Math.max(0, Math.floor(foldedCount)) : 0
}

function ashLineSeries(
  item: AshChartSeries | AshSeries,
  stack: string,
  color: string = paletteFor(item.name),
) {
  return {
    type: 'line' as const,
    name: displayName(item.name),
    stack,
    data: item.points.map((point) => [point.ts, point.value] as [string, number | null]),
    connectNulls: false,
    showSymbol: false,
    smooth: false,
    areaStyle: { opacity: 0.55 },
    itemStyle: { color },
    lineStyle: { color, width: 1 },
  }
}

function cpuCountLine(cpuCount: number) {
  return {
    silent: true,
    symbol: 'none',
    data: [
      {
        yAxis: cpuCount,
        name: `CPU count (${cpuCount})`,
        label: { formatter: `CPU count (${cpuCount})` },
        lineStyle: { color: 'var(--muted-foreground)', type: 'dashed' as const, width: 1 },
      },
    ],
  }
}

/** Build a deterministic, gap-preserving stacked area option for ASH groups. */
export function buildAshOption(
  series: readonly AshChartSeries[] | readonly AshSeries[],
  { cpuCount, foldedCount = 0, window }: AshOptionOptions = {},
): EChartsOption {
  const ordered = orderAshSeries<AshChartSeries | AshSeries>(series)
  const metadata = tooltipMetadata(ordered)
  const spanMs = axisSpan(
    window,
    spanOf(ordered.flatMap((item) => item.points.map((point) => point.ts))),
  )
  const safeFoldedCount = safeFolded(foldedCount)
  const metricSeries = ordered.map((item, index) => {
    const optionSeries = ashLineSeries(item, 'ash')
    if (index === 0 && validCpuCount(cpuCount)) {
      return { ...optionSeries, markLine: cpuCountLine(cpuCount) }
    }
    return optionSeries
  })

  return {
    animation: false,
    color: ordered.map((item) => paletteFor(item.name)),
    grid: { bottom: 72, containLabel: true, left: 58, right: 24, top: 48 },
    legend: { data: ordered.map((item) => displayName(item.name)), top: 8, type: 'scroll' },
    tooltip: {
      trigger: 'axis',
      formatter: (params: unknown) => tooltipFor(params, metadata, safeFoldedCount),
    },
    xAxis: ashTimeAxis(spanMs, window),
    yAxis: { type: 'value', min: 0, name: 'avg active backends' },
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

/** Indexes of values with no value on either side: a line through them draws nothing. */
export function isolatedPoints(points: readonly AshPoint[]): Set<number> {
  const isolated = new Set<number>()
  points.forEach((point, index) => {
    if (point.value === null) return
    const before = points[index - 1]?.value ?? null
    const after = points[index + 1]?.value ?? null
    if (before === null && after === null) isolated.add(index)
  })
  return isolated
}

const ISOLATED_SYMBOL_SIZE = 5

export interface AshGroupOptionOptions extends AshOptionOptions {
  /** The stack (and wait group) id; each group's chart stacks only its own series. */
  stack: string
  /** Only the CPU card draws the CPU-count line. */
  showCpuCount?: boolean
  /** Span of the whole page's data, so every card labels its time axis alike. */
  spanMs?: number
  /** Colour of a series; by default the token of its wait event type. */
  colorOf?: (name: string) => string
}

/**
 * One wait group's stacked chart: its own y axis from zero, the shared short
 * UTC time axis, and no ECharts legend (the page draws an HTML legend).
 */
export function buildAshGroupOption(
  series: readonly AshChartSeries[] | readonly AshSeries[],
  {
    colorOf = paletteFor,
    cpuCount,
    foldedCount = 0,
    showCpuCount = false,
    spanMs,
    stack,
    window,
  }: AshGroupOptionOptions,
): EChartsOption {
  const ordered = orderAshSeries<AshChartSeries | AshSeries>(series)
  const metadata = tooltipMetadata(ordered)
  const span = axisSpan(
    window,
    spanMs ?? spanOf(ordered.flatMap((item) => item.points.map((point) => point.ts))),
  )
  const safeFoldedCount = safeFolded(foldedCount)
  const metricSeries = ordered.map((item, index) => {
    const isolated = isolatedPoints(item.points)
    // A sample between two unsampled buckets gets a dot, or it would not show.
    const optionSeries = {
      ...ashLineSeries(item, stack, colorOf(item.name)),
      showSymbol: isolated.size > 0,
      symbol: 'circle',
      symbolSize: (_value: unknown, params: { dataIndex?: number }) =>
        params.dataIndex !== undefined && isolated.has(params.dataIndex) ? ISOLATED_SYMBOL_SIZE : 0,
    }
    if (index === 0 && showCpuCount && validCpuCount(cpuCount)) {
      return { ...optionSeries, markLine: cpuCountLine(cpuCount) }
    }
    return optionSeries
  })

  return {
    animation: false,
    color: ordered.map((item) => colorOf(item.name)),
    grid: { bottom: 72, containLabel: true, left: 58, right: 24, top: 32 },
    legend: { show: false },
    tooltip: {
      trigger: 'axis',
      confine: true,
      formatter: (params: unknown) => tooltipFor(params, metadata, safeFoldedCount),
    },
    xAxis: ashTimeAxis(span, window),
    yAxis: { type: 'value', min: 0, name: 'avg active backends' },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, filterMode: 'none', throttle: 50 },
      {
        type: 'slider',
        xAxisIndex: 0,
        filterMode: 'none',
        bottom: 8,
        labelFormatter: sliderLabelFormatter(span),
      },
    ],
    series: metricSeries,
  }
}
