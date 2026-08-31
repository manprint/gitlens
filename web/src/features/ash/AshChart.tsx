import { TimeSeriesChart } from '@/components/charts/TimeSeriesChart'
import {
  buildAshOption,
  type AshChartSeries,
  type AshOptionOptions,
} from '@/components/charts/ash.options'

export interface AshChartProps {
  series: readonly AshChartSeries[]
  cpuCount?: AshOptionOptions['cpuCount']
  foldedCount?: AshOptionOptions['foldedCount']
  ariaLabel?: string
  dataTableLabel?: string
}

/** Accessible ASH chart wrapper; brush events are delegated to TimeSeriesChart. */
export function AshChart({
  ariaLabel = 'Average active sessions by wait event',
  cpuCount,
  dataTableLabel,
  foldedCount = 0,
  series,
}: AshChartProps) {
  const option =
    cpuCount === undefined
      ? buildAshOption(series, { foldedCount })
      : buildAshOption(series, { cpuCount, foldedCount })
  const tableProps = dataTableLabel === undefined ? {} : { dataTableLabel }

  return (
    <TimeSeriesChart
      ariaLabel={ariaLabel}
      {...tableProps}
      option={option}
      series={series}
      valueLabel="Average active sessions"
    />
  )
}
