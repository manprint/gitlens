import type { EChartsOption } from 'echarts'

import type { AlignedReplicationSeries } from '@/lib/replication'

export const LAG_METRICS = ['write_lag_sec', 'flush_lag_sec', 'replay_lag_sec'] as const
export type LagMetric = (typeof LAG_METRICS)[number]

/** Design-system palette tokens kept in the pure option builder. */
export const LAG_PALETTE: Record<LagMetric, string> = {
  flush_lag_sec: 'var(--severity-warning)',
  replay_lag_sec: 'var(--severity-critical)',
  write_lag_sec: 'var(--severity-info)',
}

function points(series: AlignedReplicationSeries, metric: LagMetric): [string, number | null][] {
  return series.timestamps.map((timestamp, index) => {
    const value = series[metric][index]
    return [timestamp, value === undefined ? null : value]
  })
}

function formatUtcTimestamp(value: number): string {
  return new Date(value).toISOString()
}

/** Build a deterministic, gap-preserving ECharts option for one replication edge. */
export function buildLagOption(series: AlignedReplicationSeries): EChartsOption {
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
    tooltip: { trigger: 'axis' },
    xAxis: {
      type: 'time',
      axisLabel: {
        formatter: formatUtcTimestamp,
      },
    },
    yAxis: { type: 'value', min: 0, name: 'seconds' },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, filterMode: 'none', throttle: 50 },
      { type: 'slider', xAxisIndex: 0, filterMode: 'none', bottom: 8 },
    ],
    series: metricSeries,
  }
}
