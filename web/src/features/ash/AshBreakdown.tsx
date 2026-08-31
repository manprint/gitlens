import { Section } from '@/components/layout/Section'
import { EmptyState } from '@/components/state'
import { AshChart, type AshChartProps } from './AshChart'
import {
  ASH_WAIT_TYPES,
  type AshChartPoint,
  type AshChartSeries,
} from '@/components/charts/ash.options'
import { foldOther, orderGroups, toStackedSeries, type AshBucket, type AshGroupBy } from '@/lib/ash'

const MAX_VISIBLE_SERIES = ASH_WAIT_TYPES.length
const OTHER = 'other'

const GROUP_LABELS: Record<
  Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>,
  string
> = {
  queryid: 'Queries',
  wait_event: 'Wait events',
  wait_event_type: 'Wait event types',
}

interface AshBreakdownProps {
  buckets: readonly AshBucket[]
  filterLabel?: string | null
  groupBy: Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>
  onSeriesSelect?: AshChartProps['onSeriesSelect']
}

function groupValue(bucket: AshBucket, groupBy: AshGroupBy): string {
  const value =
    groupBy === 'wait_event_type'
      ? bucket.wait_event_type
      : groupBy === 'wait_event'
        ? bucket.wait_event
        : groupBy === 'queryid'
          ? bucket.queryid
          : groupBy === 'state'
            ? bucket.state
            : bucket.datname

  if (value === undefined || value === null || value === '') {
    return groupBy === 'wait_event_type' || groupBy === 'wait_event' ? 'CPU' : OTHER
  }
  return String(value)
}

function metadataKey(name: string, timestamp: string): string {
  return `${name}\u0000${timestamp}`
}

function chartSeries(
  buckets: readonly AshBucket[],
  groupBy: AshBreakdownProps['groupBy'],
): { folded: number; series: AshChartSeries[] } {
  const source = toStackedSeries(buckets, groupBy)
  const result = foldOther(source, MAX_VISIBLE_SERIES)
  const ordered = orderGroups(source)
  const overflow = new Set(
    ordered.length > MAX_VISIBLE_SERIES
      ? ordered.slice(MAX_VISIBLE_SERIES - 1).map(({ name }) => name)
      : [],
  )
  const metadata = new Map<string, { samples: number; ticks: number }>()

  for (const bucket of buckets) {
    const sourceName = groupValue(bucket, groupBy)
    const displayName = overflow.has(sourceName) ? OTHER : sourceName
    metadata.set(metadataKey(displayName, bucket.ts), {
      samples: bucket.samples,
      ticks: bucket.ticks,
    })
  }

  return {
    folded: result.folded,
    series: result.series.map((entry) => ({
      name: entry.name,
      points: entry.points.map((point): AshChartPoint => {
        const details = metadata.get(metadataKey(entry.name, point.ts))
        return details ? { ...point, samples: details.samples, ticks: details.ticks } : point
      }),
    })),
  }
}

export function AshBreakdown({ buckets, filterLabel, groupBy, onSeriesSelect }: AshBreakdownProps) {
  const { folded, series } = chartSeries(buckets, groupBy)
  const title = GROUP_LABELS[groupBy]
  const interactionProps = onSeriesSelect === undefined ? {} : { onSeriesSelect }
  const description = filterLabel
    ? `${title} filtered by ${filterLabel}. Select a plotted series to drill deeper.`
    : `Select a plotted series to drill from ${title.toLocaleLowerCase('en-US')} to the next level.`

  return (
    <Section title={title} description={description}>
      {series.length > 0 ? (
        <AshChart
          ariaLabel={`${title} average active sessions`}
          dataTableLabel={`${title} average active sessions data`}
          foldedCount={folded}
          {...interactionProps}
          series={series}
        />
      ) : (
        <EmptyState
          title={`No ${title.toLocaleLowerCase('en-US')} observed`}
          description="The selected time range contains no sampled activity for this breakdown."
        />
      )}
    </Section>
  )
}
