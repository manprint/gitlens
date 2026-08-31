import type { EChartsOption } from 'echarts'

import type { AshPoint, AshSeries } from '@/lib/ash'

export const ASH_WAIT_TYPES = [
  'Lock',
  'IO',
  'CPU',
  'LWLock',
  'Client',
  'IPC',
  'Timeout',
  'BufferPin',
  'Extension',
  'Activity',
  'other',
] as const

export type AshPaletteName = (typeof ASH_WAIT_TYPES)[number]

/** Fixed semantic tokens keep a wait type visually stable between polls. */
export const ASH_PALETTE: Record<AshPaletteName, string> = {
  Activity: 'var(--text-muted)',
  BufferPin: 'var(--status-degraded)',
  CPU: 'var(--status-ok)',
  Client: 'var(--status-stale)',
  Extension: 'var(--text-secondary)',
  IO: 'var(--severity-warning)',
  IPC: 'var(--status-unknown)',
  LWLock: 'var(--severity-info)',
  Lock: 'var(--status-critical)',
  Timeout: 'var(--severity-critical)',
  other: 'var(--status-muted)',
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

function displayName(name: string): string {
  return name === 'other' ? OTHER_FOLDED_LABEL : name
}

function paletteFor(name: string): string {
  return ASH_PALETTE[name as AshPaletteName] ?? ASH_PALETTE.other
}

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
      `<div><strong>${escapeHtml(name)}</strong>: ${formatMetric(value)} avg_active_sessions ` +
      `(samples: ${formatCount(samples)}, ticks: ${formatCount(ticks)})</div>`
    )
  })
  const folded =
    foldedCount > 0 ? `<div>${OTHER_FOLDED_LABEL}: ${foldedCount} groups folded</div>` : ''
  return `<div>${escapeHtml(timestamp)}</div>${rows.join('')}${folded}`
}

function formatUtcTimestamp(value: number): string {
  return new Date(value).toISOString()
}

function validCpuCount(value: number | null | undefined): value is number {
  return value !== null && value !== undefined && Number.isFinite(value) && value > 0
}

/** Build a deterministic, gap-preserving stacked area option for ASH groups. */
export function buildAshOption(
  series: readonly AshChartSeries[] | readonly AshSeries[],
  { cpuCount, foldedCount = 0 }: AshOptionOptions = {},
): EChartsOption {
  const ordered = [...series].sort((left, right) => {
    if (left.name === 'CPU' && right.name !== 'CPU') return -1
    if (left.name !== 'CPU' && right.name === 'CPU') return 1
    if (left.name === 'other' && right.name !== 'other') return 1
    if (left.name !== 'other' && right.name === 'other') return -1
    return left.name.localeCompare(right.name)
  })
  const metadata = new Map<string, AshChartPoint>()
  for (const item of ordered) {
    for (const point of item.points) {
      metadata.set(`${displayName(item.name)}\u0000${point.ts}`, point)
    }
  }
  const safeFoldedCount = Number.isFinite(foldedCount) ? Math.max(0, Math.floor(foldedCount)) : 0
  const metricSeries = ordered.map((item, index) => {
    const name = displayName(item.name)
    const optionSeries = {
      type: 'line' as const,
      name,
      stack: 'ash',
      data: item.points.map((point) => [point.ts, point.value] as [string, number | null]),
      connectNulls: false,
      showSymbol: false,
      smooth: false,
      areaStyle: { opacity: 0.55 },
      itemStyle: { color: paletteFor(item.name) },
      lineStyle: { color: paletteFor(item.name), width: 1 },
    }

    if (index === 0 && validCpuCount(cpuCount)) {
      return {
        ...optionSeries,
        markLine: {
          silent: true,
          symbol: 'none',
          data: [
            {
              yAxis: cpuCount,
              name: `CPU count (${cpuCount})`,
              label: { formatter: `CPU count (${cpuCount})` },
              lineStyle: { color: 'var(--text-secondary)', type: 'dashed' as const, width: 1 },
            },
          ],
        },
      }
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
    xAxis: {
      type: 'time',
      axisLabel: { formatter: formatUtcTimestamp },
    },
    yAxis: { type: 'value', min: 0, name: 'avg active sessions' },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, filterMode: 'none', throttle: 50 },
      { type: 'slider', xAxisIndex: 0, filterMode: 'none', bottom: 8 },
    ],
    series: metricSeries,
  }
}
