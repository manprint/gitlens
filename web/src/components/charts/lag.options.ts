import type { EChartsOption } from 'echarts'

import type { AlignedReplicationSeries } from '@/lib/replication'

import { tooltipHtml } from './metric.options'
import { sliderLabelFormatter, spanOf, timeAxisOption } from './timeAxis'

interface LagTooltipParam {
  seriesName?: string
  marker?: unknown
  value?: unknown
}

export const LAG_METRICS = ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec'] as const
export type LagMetric = (typeof LAG_METRICS)[number]

/** Design-system palette tokens kept in the pure option builder. */
export const LAG_PALETTE: Record<LagMetric, string> = {
  flush_lag_sec: 'var(--warning)',
  replay_lag_sec: 'var(--destructive)',
  write_lag_sec: 'var(--info)',
}

function points(series: AlignedReplicationSeries, metric: LagMetric): [string, number | null][] {
  return series.timestamps.map((timestamp, index) => {
    const value = series[metric][index]
    return [timestamp, value === undefined ? null : value]
  })
}

/** Tooltip: UTC time, then each lag in seconds. */
const lagTooltip = (params: unknown) => {
  const list = (Array.isArray(params) ? params : [params]) as LagTooltipParam[]
  const first = list[0]?.value
  const ts = Array.isArray(first) ? (first as unknown[])[0] : undefined
  return tooltipHtml(
    typeof ts === 'string' ? Date.parse(ts) : NaN,
    list.map((param) => {
      const value = Array.isArray(param.value) ? (param.value as unknown[])[1] : undefined
      return {
        seriesName: param.seriesName ?? '',
        marker: typeof param.marker === 'string' ? param.marker : '',
        value: typeof value === 'number' ? value : null,
      }
    }),
    'seconds',
  )
}

/** Build a deterministic, gap-preserving ECharts option for one replication edge. */
export function buildLagOption(series: AlignedReplicationSeries): EChartsOption {
  const spanMs = spanOf(series.timestamps)
  const metricSeries = LAG_METRICS.map((metric) => ({
    type: 'line' as const,
    name: metric,
    data: points(series, metric),
    connectNulls: false,
    showSymbol: false,
    smooth: false,
    itemStyle: { color: LAG_PALETTE[metric] },
    lineStyle: { color: LAG_PALETTE[metric], width: 2 },
  }))

  return {
    animation: false,
    color: LAG_METRICS.map((metric) => LAG_PALETTE[metric]),
    grid: { bottom: 72, containLabel: true, left: 48, right: 24, top: 48 },
    legend: { data: [...LAG_METRICS], top: 8, type: 'scroll' },
    tooltip: {
      trigger: 'axis',
      confine: true,
      formatter: lagTooltip,
    },
    xAxis: timeAxisOption(spanMs),
    yAxis: { type: 'value', min: 0, name: 'seconds' },
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
