import { Activity } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAsh, useInstanceSettings } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Disabled, ErrorState } from '@/components/state'
import { useTimeRange } from '@/hooks/useTimeRange'
import { totalSamples } from '@/lib/ash'
import type { AshGroupBy } from '@/lib/ash'
import { queryWindowOf } from '@/lib/timerange'

import { AshBreakdown } from './AshBreakdown'
import { AshTopQueries } from './AshTopQueries'
import { Callout } from '@/components/ui/callout'

type DrillGroup = Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>

const DEFAULT_GROUP: DrillGroup = 'wait_event_type'

// Rows for 24 groups and "other" in each of the server's 180 buckets. At the
// old 1,000 the server kept only the 4 largest groups of a long range.
const ASH_ROW_LIMIT = 5_000

function AshFootnote() {
  return (
    <p className="text-muted-foreground text-xs" data-testid="ash-sampling-footnote">
      ASH uses 1 s statistical sampling; sub-second queries are under-represented. At most 100 wait
      keys are retained per 10 s window; the remainder is folded into “other” and the total is
      conserved. The chart plots the largest groups of the range and folds the rest into “other”
      too.
    </p>
  )
}

// The settings endpoint omits a setting at its default, and the default is auto.
// Auto computes query IDs only when a module such as pg_stat_statements is
// loaded, so a missing setting is not evidence that query IDs are on.
function computeQueryIdSetting(settings: readonly { name: string; value: string | null }[]) {
  const setting = settings.find(({ name }) => name === 'compute_query_id')
  return setting?.value?.trim().toLocaleLowerCase('en-US') ?? 'auto'
}

function readGroup(value: string | null): DrillGroup {
  return value === 'wait_event' || value === 'queryid' ? value : DEFAULT_GROUP
}

// The drill-down narrows on the server. Grouped rows carry only their group
// column, so the old client-side filter on the parent column matched no row
// and every drill-down came back empty.
function drillFilters(
  groupBy: DrillGroup,
  waitEventType: string | null,
  waitEvent: string | null,
): { wait_event_type?: string; wait_event?: string } {
  return {
    ...(groupBy !== DEFAULT_GROUP && waitEventType ? { wait_event_type: waitEventType } : {}),
    ...(groupBy === 'queryid' && waitEvent ? { wait_event: waitEvent } : {}),
  }
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
      <ol className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
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
  const timeWindow = queryWindowOf(range)
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const groupBy = readGroup(searchParams.get('group'))
  const waitEventType = searchParams.get('wait_event_type')
  const waitEvent = searchParams.get('wait_event')
  const database = searchParams.get('db') ?? undefined
  const filters = drillFilters(groupBy, waitEventType, waitEvent)
  const params = {
    group_by: groupBy,
    instance_id: instanceId,
    limit: ASH_ROW_LIMIT,
    ...(database === undefined ? {} : { database }),
    ...filters,
  }
  const ashQuery = useAsh(params, timeWindow)
  const settingsQuery = useInstanceSettings(instanceId)
  const unauthorized =
    ashQuery.error?.kind === 'unauthorized' || settingsQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  const topQueryProps = {
    ...(database === undefined ? {} : { database }),
    ...(filters.wait_event_type === undefined ? {} : { waitEventType: filters.wait_event_type }),
    ...(filters.wait_event === undefined ? {} : { waitEvent: filters.wait_event }),
  }
  const sampleCount = totalSamples(ashQuery.data?.buckets ?? [])
  // Key the note on the sampled data as well as the setting. Auto, the default,
  // leaves the setting out of the settings endpoint. With the setting on, a
  // range of idle sessions can legitimately carry no query IDs, so the chart
  // stays. An empty range has no samples to judge.
  const queryIdSetting = computeQueryIdSetting(settingsQuery.data?.settings ?? [])
  const queryIdsUnavailable =
    groupBy === 'queryid' &&
    settingsQuery.data !== undefined &&
    !['on', 'regress'].includes(queryIdSetting) &&
    (ashQuery.data?.buckets.length ?? 0) > 0 &&
    ashQuery.data?.buckets.every(
      (bucket) => bucket.queryid === null || bucket.queryid === undefined,
    )

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
        icon={Activity}
        eyebrow="Wait events"
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
          <p className="text-muted-foreground text-sm">
            Keeping ASH disabled avoids its sampling cost but removes wait-analysis visibility.
          </p>
        </>
      ) : (
        <>
          {ashQuery.data.warning ? (
            <Callout role="status" variant="warning">
              <strong>ASH sampling warning:</strong> {ashQuery.data.warning} {sampleCount} samples
              were collected in this range; conclusions are unreliable.
            </Callout>
          ) : null}
          {queryIdsUnavailable ? (
            <div className="space-y-2">
              <Degraded
                reason={
                  queryIdSetting === 'off'
                    ? 'No non-null query_id was observed in this ASH range, and the instance setting compute_query_id is off.'
                    : 'No non-null query_id was observed in this ASH range. Under compute_query_id = auto, query IDs are computed only when a module such as pg_stat_statements is loaded.'
                }
                requires="compute_query_id=on"
              />
              <p className="text-muted-foreground text-sm">
                See the{' '}
                <a className="underline" href="/README.md#agent-configuration">
                  agent configuration in the README
                </a>{' '}
                to enable query attribution.
              </p>
            </div>
          ) : (
            <AshBreakdown
              buckets={ashQuery.data.buckets}
              filterLabel={
                groupBy === 'wait_event'
                  ? (waitEventType ?? null)
                  : groupBy === 'queryid'
                    ? (waitEvent ?? null)
                    : null
              }
              groupBy={groupBy}
              onSeriesSelect={selectSeries}
              resolutionSeconds={ashQuery.data.resolution_seconds}
            />
          )}
          {groupBy === 'queryid' && !queryIdsUnavailable ? (
            <AshTopQueries {...topQueryProps} timeWindow={timeWindow} instanceId={instanceId} />
          ) : null}
        </>
      )}
      <AshFootnote />
    </div>
  )
}

export default AshPage
