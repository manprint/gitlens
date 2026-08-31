import { useEffect, useMemo, useRef } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAsh, useInstanceSettings } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Disabled, ErrorState } from '@/components/state'
import { useTimeRange } from '@/hooks/useTimeRange'
import { totalSamples } from '@/lib/ash'
import type { AshBucket, AshGroupBy } from '@/lib/ash'

import { AshBreakdown } from './AshBreakdown'
import { AshTopQueries } from './AshTopQueries'

type DrillGroup = Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>

const DEFAULT_GROUP: DrillGroup = 'wait_event_type'

function AshFootnote() {
  return (
    <p className="text-text-secondary text-xs" data-testid="ash-sampling-footnote">
      ASH uses 1 s statistical sampling; sub-second queries are under-represented. At most 100 wait
      keys are retained per 10 s window; the remainder is folded into “other” and the total is
      conserved.
    </p>
  )
}

function isComputeQueryIdOff(settings: readonly { name: string; value: string | null }[]) {
  const setting = settings.find(({ name }) => name === 'compute_query_id')
  return ['off', 'false', '0'].includes(setting?.value?.trim().toLocaleLowerCase('en-US') ?? '')
}

function readGroup(value: string | null): DrillGroup {
  return value === 'wait_event' || value === 'queryid' ? value : DEFAULT_GROUP
}

function groupValue(bucket: AshBucket, groupBy: DrillGroup): string {
  const value =
    groupBy === 'wait_event_type'
      ? bucket.wait_event_type
      : groupBy === 'wait_event'
        ? bucket.wait_event
        : bucket.queryid
  if (value === undefined || value === null || value === '') {
    return groupBy === 'queryid' ? 'other' : 'CPU'
  }
  return String(value)
}

function filteredBuckets(
  buckets: readonly AshBucket[],
  groupBy: DrillGroup,
  waitEventType: string | null,
  waitEvent: string | null,
): AshBucket[] {
  if (groupBy === 'wait_event' && waitEventType) {
    return buckets.filter((bucket) => groupValue(bucket, 'wait_event_type') === waitEventType)
  }
  if (groupBy === 'queryid' && waitEvent) {
    return buckets.filter((bucket) => groupValue(bucket, 'wait_event') === waitEvent)
  }
  return [...buckets]
}

interface DrillBreadcrumbProps {
  groupBy: DrillGroup
  onNavigate: (groupBy: DrillGroup) => void
  waitEvent: string | null
  waitEventType: string | null
}

function DrillBreadcrumb({ groupBy, onNavigate, waitEvent, waitEventType }: DrillBreadcrumbProps) {
  return (
    <nav aria-label="ASH drill path">
      <ol className="text-text-secondary flex flex-wrap items-center gap-2 text-sm">
        <li>
          {groupBy === DEFAULT_GROUP ? (
            <span aria-current="page">Wait event types</span>
          ) : (
            <button className="underline" onClick={() => onNavigate(DEFAULT_GROUP)} type="button">
              Wait event types
            </button>
          )}
        </li>
        {groupBy !== DEFAULT_GROUP ? (
          <>
            <li aria-hidden="true">/</li>
            <li>
              {groupBy === 'wait_event' ? (
                <span aria-current="page">Wait events: {waitEventType ?? 'all'}</span>
              ) : (
                <button
                  className="underline"
                  onClick={() => onNavigate('wait_event')}
                  type="button"
                >
                  Wait events: {waitEvent ?? 'all'}
                </button>
              )}
            </li>
          </>
        ) : null}
        {groupBy === 'queryid' ? (
          <>
            <li aria-hidden="true">/</li>
            <li aria-current="page">Queries</li>
          </>
        ) : null}
      </ol>
    </nav>
  )
}

export interface AshPageProps {
  /** Optional override keeps the page easy to exercise without the lazy route. */
  instanceId?: string
}

export function AshPage({ instanceId: instanceIdOverride }: AshPageProps = {}) {
  const location = useLocation()
  const navigate = useNavigate()
  const { instanceId: routeInstanceId } = useParams<{ instanceId: string }>()
  const redirectedForUnauthorized = useRef(false)
  const [searchParams, setSearchParams] = useSearchParams()
  const { range } = useTimeRange()
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const groupBy = readGroup(searchParams.get('group'))
  const waitEventType = searchParams.get('wait_event_type')
  const waitEvent = searchParams.get('wait_event')
  const database = searchParams.get('db') ?? undefined
  const params = {
    from: range.from.toISOString(),
    group_by: groupBy,
    instance_id: instanceId,
    limit: 1000,
    to: range.to.toISOString(),
    ...(database === undefined ? {} : { database }),
  }
  const ashQuery = useAsh(params)
  const settingsQuery = useInstanceSettings(instanceId)
  const unauthorized =
    ashQuery.error?.kind === 'unauthorized' || settingsQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  const buckets = useMemo(
    () => filteredBuckets(ashQuery.data?.buckets ?? [], groupBy, waitEventType, waitEvent),
    [ashQuery.data?.buckets, groupBy, waitEvent, waitEventType],
  )
  const topQueryProps = database === undefined ? {} : { database }
  const sampleCount = totalSamples(ashQuery.data?.buckets ?? [])
  const queryIdsUnavailable =
    groupBy === 'queryid' &&
    ashQuery.data?.buckets.every(
      (bucket) => bucket.queryid === null || bucket.queryid === undefined,
    ) &&
    isComputeQueryIdOff(settingsQuery.data?.settings ?? [])

  function navigateTo(nextGroup: DrillGroup) {
    const next = new URLSearchParams(searchParams)
    next.set('group', nextGroup)
    if (nextGroup === DEFAULT_GROUP) {
      next.delete('wait_event_type')
      next.delete('wait_event')
      next.delete('queryid')
    } else if (nextGroup === 'wait_event') {
      next.delete('wait_event')
      next.delete('queryid')
    }
    setSearchParams(next)
  }

  function selectSeries(name: string) {
    const cleanName = name.replace(/ \(folded\)$/u, '')
    if (cleanName.toLocaleLowerCase('en-US') === 'other') return
    const next = new URLSearchParams(searchParams)
    if (groupBy === DEFAULT_GROUP) {
      next.set('group', 'wait_event')
      next.set('wait_event_type', cleanName)
      next.delete('wait_event')
      next.delete('queryid')
    } else if (groupBy === 'wait_event') {
      next.set('group', 'queryid')
      next.set('wait_event', cleanName)
      next.delete('queryid')
    }
    setSearchParams(next)
  }

  if (ashQuery.error && !ashQuery.data) {
    return (
      <div className="space-y-4">
        <ErrorState
          endpoint="ASH"
          failure={ashQuery.error}
          onRetry={() => void ashQuery.refetch()}
        />
        <AshFootnote />
      </div>
    )
  }

  if (ashQuery.isPending || ashQuery.data === undefined) {
    return (
      <div className="space-y-4">
        <section aria-busy="true" aria-label="Loading ASH and wait analysis" role="status">
          Loading ASH and wait analysis…
        </section>
        <AshFootnote />
      </div>
    )
  }

  return (
    <div className="space-y-8">
      <PageHeader
        freshness={<FreshnessBadge dataUpdatedAt={ashQuery.dataUpdatedAt} policy="ash" />}
        title="ASH and wait analysis"
        subtitle={
          <span>
            Instance <code>{instanceId}</code> · {range.label}
          </span>
        }
      />
      <DrillBreadcrumb
        groupBy={groupBy}
        onNavigate={navigateTo}
        waitEvent={waitEvent}
        waitEventType={waitEventType}
      />
      {ashQuery.data.enabled === false ? (
        <>
          <Disabled feature="ASH sampling" configKey="checks.ash in the agent configuration file" />
          <p className="text-text-secondary text-sm">
            Keeping ASH disabled avoids its sampling cost but removes wait-analysis visibility.
          </p>
        </>
      ) : (
        <>
          {ashQuery.data.warning ? (
            <div className="border-warning/40 bg-warning/10 text-sm" role="status">
              <strong>ASH sampling warning:</strong> {ashQuery.data.warning} {sampleCount} samples
              were collected in this range; conclusions are unreliable.
            </div>
          ) : null}
          {queryIdsUnavailable ? (
            <div className="space-y-2">
              <Degraded
                reason="No non-null query_id was observed in this ASH range, and the instance setting compute_query_id is off."
                requires="compute_query_id=on"
              />
              <p className="text-text-secondary text-sm">
                See the{' '}
                <a className="underline" href="/README.md#agent-configuration">
                  agent configuration in the README
                </a>{' '}
                to enable query attribution.
              </p>
            </div>
          ) : (
            <AshBreakdown
              buckets={buckets}
              filterLabel={
                groupBy === 'wait_event'
                  ? (waitEventType ?? null)
                  : groupBy === 'queryid'
                    ? (waitEvent ?? null)
                    : null
              }
              groupBy={groupBy}
              onSeriesSelect={selectSeries}
            />
          )}
          {groupBy === 'queryid' && !queryIdsUnavailable ? (
            <AshTopQueries
              {...topQueryProps}
              from={range.from}
              instanceId={instanceId}
              to={range.to}
            />
          ) : null}
        </>
      )}
      <AshFootnote />
    </div>
  )
}

export default AshPage
