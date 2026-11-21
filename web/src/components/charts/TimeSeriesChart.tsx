import { useMemo } from 'react'
import type { EChartsOption } from 'echarts'
import EChartsReact from 'echarts-for-react/esm/core'

import { useT } from '@/i18n/react'

import { buildChartTheme, resolveTokens, useTokenReader } from './chartTheme'
import { echarts } from './echarts'

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
  /** Chart height in pixels (default 300). */
  height?: number
}

/** Thin, accessible ECharts wrapper; each chart's tooltip and zoom are its own. */
export function TimeSeriesChart({
  ariaLabel,
  dataTableLabel: dataTableLabelProp,
  height = 300,
  onSeriesSelect,
  option,
  series,
  valueLabel: valueLabelProp,
}: TimeSeriesChartProps) {
  const t = useT()
  const dataTableLabel = dataTableLabelProp ?? t('shell.chart.dataTable', { chart: ariaLabel })
  const valueLabel = valueLabelProp ?? t('shell.chart.valueSeconds')
  const read = useTokenReader()
  // A theme change gives a new reader, and a new theme makes the chart library
  // rebuild the chart with the other palette.
  const themed = useMemo(
    () => ({ option: resolveTokens(option, read), theme: buildChartTheme(read) }),
    [option, read],
  )

  function handleSeriesClick(event: unknown) {
    if (typeof event !== 'object' || event === null) return
    const name = (event as Record<string, unknown>).seriesName
    if (typeof name === 'string' && name.length > 0) onSeriesSelect?.(name)
  }

  return (
    <div>
      {/* role="img" makes its children presentational, so the data table sits
          beside the chart rather than inside it, where screen readers reach it. */}
      <div aria-label={ariaLabel} role="img">
        <EChartsReact
          echarts={echarts}
          // Each chart stands alone: no shared tooltip, and its zoom stays local.
          onEvents={onSeriesSelect === undefined ? {} : { click: handleSeriesClick }}
          option={themed.option}
          style={{ height, width: '100%' }}
          theme={themed.theme}
        />
      </div>
      {/* Tables ignore a 1px width, so the wrapper hides the data table instead. */}
      <div className="sr-only">
        <table aria-label={dataTableLabel} data-chart-data="">
          <caption>{dataTableLabel}</caption>
          <thead>
            <tr>
              <th scope="col">{t('shell.chart.metric')}</th>
              <th scope="col">{t('shell.chart.timestamp')}</th>
              <th scope="col">{valueLabel}</th>
            </tr>
          </thead>
          <tbody>
            {series.flatMap((metric) =>
              metric.points.map((point) => (
                <tr key={`${metric.name}-${point.ts}`}>
                  <th scope="row">{metric.name}</th>
                  <td>{point.ts}</td>
                  <td>{point.value ?? t('shell.topology.unknown')}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
