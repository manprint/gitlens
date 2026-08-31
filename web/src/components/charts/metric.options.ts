import type { EChartsOption } from 'echarts'

import { findResets } from '@/lib/metrics'

import type { TimeSeriesTableSeries } from './TimeSeriesChart'

export interface MetricOptionOptions {
  annotateResets?: boolean
  yAxisName?: string
}

function formatUtcTimestamp(value: number): string {
  return new Date(value).toISOString()
}

/** Build a null-preserving option for an instance metric series or series group. */
export function buildMetricOption(
  series: readonly TimeSeriesTableSeries[],
  { annotateResets = true, yAxisName }: MetricOptionOptions = {},
): EChartsOption {
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

    const resetPoints = findResets(metric.points).map((index) => {
      const point = metric.points[index]
      return {
        coord: [point?.ts ?? '', 0],
        name: 'Counter reset',
        value: 'reset',
      }
    })

    return resetPoints.length > 0
      ? { ...optionSeries, markPoint: { data: resetPoints } }
      : optionSeries
  })

  return {
    animation: false,
    grid: { bottom: 72, containLabel: true, left: 48, right: 24, top: 48 },
    legend: { data: series.map(({ name }) => name), top: 8, type: 'scroll' },
    tooltip: { trigger: 'axis' },
    xAxis: {
      type: 'time',
      axisLabel: { formatter: formatUtcTimestamp },
    },
    yAxis: yAxisName ? { type: 'value', name: yAxisName } : { type: 'value' },
    dataZoom: [
      { type: 'inside', xAxisIndex: 0, filterMode: 'none', throttle: 50 },
      { type: 'slider', xAxisIndex: 0, filterMode: 'none', bottom: 8 },
    ],
    series: metricSeries,
  }
}
