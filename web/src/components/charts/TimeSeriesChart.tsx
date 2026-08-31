import type { EChartsOption } from 'echarts'
import EChartsReact from 'echarts-for-react'
import { useTimeRange } from '@/hooks/useTimeRange'

export interface TimeSeriesPoint {
  ts: string
  value: number | null
}

export interface TimeSeriesTableSeries {
  name: string
  points: readonly TimeSeriesPoint[]
}

export interface TimeSeriesChartProps {
  option: EChartsOption
  ariaLabel: string
  series: readonly TimeSeriesTableSeries[]
  dataTableLabel?: string
  onSeriesSelect?: (name: string) => void
  valueLabel?: string
}

interface DataZoomWindow {
  startValue?: unknown
  endValue?: unknown
}

function isDataZoomWindow(value: unknown): value is DataZoomWindow {
  return typeof value === 'object' && value !== null
}

function dateFromValue(value: unknown): Date | null {
  if (typeof value === 'number' && Number.isFinite(value)) {
    const date = new Date(value)
    return Number.isFinite(date.getTime()) ? date : null
  }
  if (typeof value === 'string') {
    const date = new Date(value)
    return Number.isFinite(date.getTime()) ? date : null
  }
  return null
}

function dataZoomWindow(event: unknown): DataZoomWindow | null {
  if (typeof event !== 'object' || event === null) return null
  const record = event as Record<string, unknown>
  const batch = record.batch
  if (Array.isArray(batch) && batch.length > 0) {
    const first: unknown = batch[0] as unknown
    if (isDataZoomWindow(first)) return first
  }
  return isDataZoomWindow(record) ? record : null
}

function rangeFromDataZoom(event: unknown): { from: Date; to: Date } | null {
  const window = dataZoomWindow(event)
  if (!window) return null
  const from = dateFromValue(window.startValue)
  const to = dateFromValue(window.endValue)
  if (!from || !to || from.getTime() > to.getTime()) return null
  return { from, to }
}

/** Thin, accessible ECharts wrapper with a URL-backed brush selection. */
export function TimeSeriesChart({
  ariaLabel,
  dataTableLabel = `${ariaLabel} data`,
  onSeriesSelect,
  option,
  series,
  valueLabel = 'Value (seconds)',
}: TimeSeriesChartProps) {
  const { setRange } = useTimeRange()

  function handleDataZoom(event: unknown) {
    const range = rangeFromDataZoom(event)
    if (!range) return
    setRange({ from: range.from, kind: 'absolute', label: 'Custom range', to: range.to })
  }

  function handleSeriesClick(event: unknown) {
    if (typeof event !== 'object' || event === null) return
    const name = (event as Record<string, unknown>).seriesName
    if (typeof name === 'string' && name.length > 0) onSeriesSelect?.(name)
  }

  return (
    <div role="img" aria-label={ariaLabel}>
      <EChartsReact
        aria-hidden="true"
        onEvents={{
          datazoom: handleDataZoom,
          ...(onSeriesSelect === undefined ? {} : { click: handleSeriesClick }),
        }}
        option={option}
        style={{ height: 300, width: '100%' }}
      />
      <table className="sr-only" aria-label={dataTableLabel}>
        <caption>{dataTableLabel}</caption>
        <thead>
          <tr>
            <th scope="col">Metric</th>
            <th scope="col">Timestamp (UTC)</th>
            <th scope="col">{valueLabel}</th>
          </tr>
        </thead>
        <tbody>
          {series.flatMap((metric) =>
            metric.points.map((point) => (
              <tr key={`${metric.name}-${point.ts}`}>
                <th scope="row">{metric.name}</th>
                <td>{point.ts}</td>
                <td>{point.value ?? 'Unknown'}</td>
              </tr>
            )),
          )}
        </tbody>
      </table>
    </div>
  )
}
