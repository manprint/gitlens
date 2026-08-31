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
  onSeriesSelect?: (name: string) => void
}

/** Accessible ASH chart wrapper; brush events are delegated to TimeSeriesChart. */
export function AshChart({
  ariaLabel = 'Average active sessions by wait event',
  cpuCount,
  dataTableLabel,
  foldedCount = 0,
  onSeriesSelect,
  series,
}: AshChartProps) {
  const option =
    cpuCount === undefined
      ? buildAshOption(series, { foldedCount })
      : buildAshOption(series, { cpuCount, foldedCount })
  const tableProps = dataTableLabel === undefined ? {} : { dataTableLabel }
  const interactionProps = onSeriesSelect === undefined ? {} : { onSeriesSelect }

  return (
    <TimeSeriesChart
      ariaLabel={ariaLabel}
      {...tableProps}
      {...interactionProps}
      option={option}
      series={series}
      valueLabel="Average active sessions"
    />
  )
}
