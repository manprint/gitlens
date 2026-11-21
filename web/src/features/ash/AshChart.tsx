import { TimeSeriesChart } from '@/components/charts/TimeSeriesChart'
import {
  buildAshOption,
  type AshChartSeries,
  type AshOptionOptions,
} from '@/components/charts/ash.options'
import { useT } from '@/i18n/react'

export interface AshChartProps {
  series: readonly AshChartSeries[]
  foldedCount?: AshOptionOptions['foldedCount']
  ariaLabel?: string
  dataTableLabel?: string
  onSeriesSelect?: (name: string) => void
  /** The selected time range; the axis spans it. */
  window?: AshOptionOptions['window']
}

/** Accessible ASH chart wrapper; brush events are delegated to TimeSeriesChart. */
export function AshChart({
  ariaLabel,
  dataTableLabel,
  foldedCount = 0,
  onSeriesSelect,
  series,
  window,
}: AshChartProps) {
  const t = useT()
  const option = buildAshOption(series, { foldedCount, window })
  const tableProps = dataTableLabel === undefined ? {} : { dataTableLabel }
  const interactionProps = onSeriesSelect === undefined ? {} : { onSeriesSelect }

  return (
    <TimeSeriesChart
      ariaLabel={ariaLabel ?? t('ash.chart.ariaLabel')}
      {...tableProps}
      {...interactionProps}
      option={option}
      series={series}
      valueLabel={t('ash.chartGrid.valueLabel')}
    />
  )
}
