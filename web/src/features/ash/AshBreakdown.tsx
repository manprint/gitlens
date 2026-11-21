import { ChartCard } from '@/components/charts/ChartCard'
import { Section } from '@/components/layout/Section'
import { useTimeRange } from '@/hooks/useTimeRange'
import { EmptyState } from '@/components/state'
import { AshChart, type AshChartProps } from './AshChart'
import { AshChartGrid } from './AshChartGrid'
import {
  ASH_WAIT_TYPES,
  type AshChartPoint,
  type AshChartSeries,
} from '@/components/charts/ash.options'
import { foldOther, orderGroups, toStackedSeries, type AshBucket, type AshGroupBy } from '@/lib/ash'
import { useT } from '@/i18n/react'
import { groupSeries } from '@/lib/ashGroups'
import type { CaptionId } from '@/lib/captions'

const MAX_VISIBLE_SERIES = ASH_WAIT_TYPES.length
const OTHER = 'other'

const GROUP_LABELS: Record<
  Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>,
  'ash.group.queryid' | 'ash.group.waitEvent' | 'ash.group.waitEventType'
> = {
  queryid: 'ash.group.queryid',
  wait_event: 'ash.group.waitEvent',
  wait_event_type: 'ash.group.waitEventType',
}

const EMPTY_LABELS: Record<
  Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>,
  'ash.empty.queryid' | 'ash.empty.waitEvent' | 'ash.empty.waitEventType'
> = {
  queryid: 'ash.empty.queryid',
  wait_event: 'ash.empty.waitEvent',
  wait_event_type: 'ash.empty.waitEventType',
}

const STACK_CAPTIONS: Record<
  Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>,
  CaptionId
> = {
  queryid: 'ash.stack-queries',
  wait_event: 'ash.stack-events',
  wait_event_type: 'ash.stack-types',
}

interface AshBreakdownProps {
  buckets: readonly AshBucket[]
  filterLabel?: string | null
  groupBy: Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>
  onSeriesSelect?: AshChartProps['onSeriesSelect']
  /** The server's bucket width, so a missing bucket is drawn as a gap. */
  resolutionSeconds?: number
  /** `separate`: one chart per wait group; `stacked`: one chart. Queries always stack. */
  view?: 'separate' | 'stacked'
}

/** Each wait event's type, as the server names it on every bucket. */
function eventTypesOf(buckets: readonly AshBucket[]): Map<string, string> {
  const types = new Map<string, string>()
  for (const bucket of buckets) {
    const type = bucket.wait_event_type
    if (type === undefined || type === '') continue
    types.set(groupValue(bucket, 'wait_event'), type)
  }
  return types
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
  resolutionSeconds: number | undefined,
): { folded: number; series: AshChartSeries[] } {
  const source = toStackedSeries(buckets, groupBy, resolutionSeconds)
  const result = foldOther(source, MAX_VISIBLE_SERIES)
  const ordered = orderGroups(source)
  const overflow = new Set(
    ordered.length > MAX_VISIBLE_SERIES
      ? ordered.slice(MAX_VISIBLE_SERIES - 1).map(({ name }) => name)
      : [],
  )
  const metadata = new Map<string, { samples: number; ticks: number }>()

  // Several source groups share one "other" point: their samples add up
  // (the ticks are the instance's, the same for every group of a bucket).
  // Each used to overwrite the last, so "other" reported one group's samples.
  for (const bucket of buckets) {
    const sourceName = groupValue(bucket, groupBy)
    const displayName = overflow.has(sourceName) ? OTHER : sourceName
    const key = metadataKey(displayName, bucket.ts)
    metadata.set(key, {
      samples: (metadata.get(key)?.samples ?? 0) + bucket.samples,
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

export function AshBreakdown({
  buckets,
  filterLabel,
  groupBy,
  onSeriesSelect,
  resolutionSeconds,
  view = 'stacked',
}: AshBreakdownProps) {
  const t = useT()
  const { range } = useTimeRange()
  const { folded, series } = chartSeries(buckets, groupBy, resolutionSeconds)
  const title = t(GROUP_LABELS[groupBy])
  const eventTypes = eventTypesOf(buckets)
  const interactionProps = onSeriesSelect === undefined ? {} : { onSeriesSelect }
  const filterNote = filterLabel ? (
    <p className="text-muted-foreground mb-3 text-sm">
      {t('ash.breakdown.filtered', { title, filter: filterLabel })}
    </p>
  ) : null
  const body = (
    <>
      {filterNote}
      {series.length > 0 && view === 'separate' && groupBy !== 'queryid' ? (
        <AshChartGrid
          eventTypes={eventTypes}
          foldedCount={folded}
          groupBy={groupBy}
          groups={groupSeries(series, groupBy, eventTypes)}
          {...interactionProps}
          range={range}
          stepSeconds={resolutionSeconds ?? 0}
        />
      ) : series.length > 0 ? (
        <ChartCard
          captionId={STACK_CAPTIONS[groupBy]}
          range={range}
          state="ready"
          stepSeconds={resolutionSeconds ?? 0}
          title={t('ash.breakdown.averageTitle', { title })}
        >
          <AshChart
            ariaLabel={t('ash.breakdown.averageTitle', { title })}
            dataTableLabel={t('ash.breakdown.averageTable', { title })}
            foldedCount={folded}
            {...interactionProps}
            series={series}
            window={range}
          />
        </ChartCard>
      ) : (
        <EmptyState title={t(EMPTY_LABELS[groupBy])} description={t('ash.breakdown.emptyBody')} />
      )}
    </>
  )

  if (groupBy === 'wait_event_type') {
    return (
      <Section captionId="ash.breakdown-types" title={title}>
        {body}
      </Section>
    )
  }
  if (groupBy === 'wait_event') {
    return (
      <Section captionId="ash.breakdown-events" title={title}>
        {body}
      </Section>
    )
  }
  return (
    <Section captionId="ash.breakdown-queries" title={title}>
      {body}
    </Section>
  )
}
