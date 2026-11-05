import { useState } from 'react'

import { ChartCard } from '@/components/charts/ChartCard'
import { TimeSeriesChart } from '@/components/charts/TimeSeriesChart'
import {
  ASH_EVENT_COLORS,
  buildAshGroupOption,
  displayName,
  orderAshSeries,
  paletteFor,
  type AshChartSeries,
} from '@/components/charts/ash.options'
import {
  formatAverage,
  seriesAverage,
  waitDescription,
  type AshGroupedSeries,
  type AshWaitGroupId,
} from '@/lib/ashGroups'

import type { CaptionId } from '@/lib/captions'

import { AshLegend } from './AshLegend'

const GROUP_CAPTIONS: Record<AshWaitGroupId, CaptionId> = {
  client: 'ash.chart-client',
  cpu: 'ash.chart-cpu',
  io: 'ash.chart-io',
  locks: 'ash.chart-locks',
  other: 'ash.chart-other',
}

/** Charts of the grid share one tooltip pointer and zoom through this group. */
export const ASH_CHART_GROUP = 'ash-wait-groups'

export interface AshChartGridProps {
  groups: readonly AshGroupedSeries<AshChartSeries>[]
  /** `wait_event` series are events: colours by position, descriptions by their type. */
  groupBy: 'wait_event_type' | 'wait_event'
  /** Type of each wait event, when the series are events. */
  eventTypes?: ReadonlyMap<string, string>
  cpuCount?: number | null
  foldedCount: number
  range: { from: Date; to: Date }
  stepSeconds: number
  onSeriesSelect?: (name: string) => void
}

function seriesDescription(
  name: string,
  groupBy: AshChartGridProps['groupBy'],
  eventTypes: ReadonlyMap<string, string>,
) {
  if (name === 'other') return waitDescription('other')
  if (groupBy === 'wait_event_type') return waitDescription(name)
  const type = eventTypes.get(name)
  return type === undefined ? waitDescription('other') : `${type}: ${waitDescription(type)}`
}

const percent = (share: number) =>
  share > 0 && share < 0.005 ? '<1%' : `${Math.round(share * 100)}%`

/**
 * One chart per wait group, each on its own y axis so a small group is not
 * squashed by a large one. The charts share the page's time axis and, through
 * `echarts.connect`, their tooltip pointer and zoom.
 */
export function AshChartGrid({
  cpuCount,
  eventTypes = new Map(),
  foldedCount,
  groupBy,
  groups,
  onSeriesSelect,
  range,
  stepSeconds,
}: AshChartGridProps) {
  const [isolated, setIsolated] = useState<ReadonlyMap<AshWaitGroupId, string>>(new Map())
  // Every card spans the selected range, so the shared zoom lines them up.
  const spanMs = range.to.getTime() - range.from.getTime()
  const interactionProps = onSeriesSelect === undefined ? {} : { onSeriesSelect }

  return (
    <div className="grid gap-4 xl:grid-cols-2" data-testid="ash-chart-grid">
      {groups.map(({ group, series, share, total }) => {
        const ordered = orderAshSeries(series)
        const colors = new Map(
          ordered.map((entry, index) => [
            entry.name,
            groupBy === 'wait_event_type'
              ? paletteFor(entry.name)
              : (ASH_EVENT_COLORS[index % ASH_EVENT_COLORS.length] ?? paletteFor('other')),
          ]),
        )
        const colorOf = (name: string) => colors.get(name) ?? paletteFor('other')
        const alone = isolated.get(group.id) ?? null
        const visible = alone === null ? ordered : ordered.filter(({ name }) => name === alone)
        const option = buildAshGroupOption(visible, {
          colorOf,
          cpuCount: cpuCount ?? null,
          foldedCount: ordered.some(({ name }) => name === 'other') ? foldedCount : 0,
          showCpuCount: group.id === 'cpu',
          spanMs,
          stack: group.id,
          window: range,
        })

        return (
          <ChartCard
            captionId={GROUP_CAPTIONS[group.id]}
            key={group.id}
            range={range}
            state="ready"
            stepSeconds={stepSeconds}
            subtitle={`avg ${formatAverage(total)} active backends · ${percent(share)} of all waits`}
            title={group.label}
          >
            <TimeSeriesChart
              ariaLabel={`${group.label} average active backends`}
              dataTableLabel={`${group.label} average active backends data`}
              group={ASH_CHART_GROUP}
              {...interactionProps}
              option={option}
              series={visible}
              valueLabel="Average active backends"
            />
            <AshLegend
              entries={ordered.map((entry) => ({
                average: seriesAverage(entry),
                color: colorOf(entry.name),
                description: seriesDescription(entry.name, groupBy, eventTypes),
                name: displayName(entry.name),
              }))}
              isolated={alone === null ? null : displayName(alone)}
              label={`${group.label} legend`}
              onToggle={(name) => {
                const raw = ordered.find((entry) => displayName(entry.name) === name)?.name
                if (raw === undefined) return
                setIsolated((current) => {
                  const next = new Map(current)
                  if (next.get(group.id) === raw) next.delete(group.id)
                  else next.set(group.id, raw)
                  return next
                })
              }}
            />
          </ChartCard>
        )
      })}
    </div>
  )
}
