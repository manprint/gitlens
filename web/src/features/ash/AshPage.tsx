import { Activity } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { useAsh, useInstanceSettings } from '@/api/queries'
import { Caption } from '@/components/layout/Caption'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { InfoHint } from '@/components/layout/InfoHint'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Degraded, Disabled, ErrorState } from '@/components/state'
import { displayName } from '@/components/charts/ash.options'
import { useTimeRange } from '@/hooks/useTimeRange'
import { totalSamples } from '@/lib/ash'
import type { AshGroupBy } from '@/lib/ash'
import { useT } from '@/i18n/react'
import { queryWindowOf, rangeLabelKey } from '@/lib/timerange'

import { AshBreakdown } from './AshBreakdown'
import { AshTopQueries } from './AshTopQueries'
import { Callout } from '@/components/ui/callout'
import { cn } from '@/lib/utils'

type DrillGroup = Extract<AshGroupBy, 'wait_event_type' | 'wait_event' | 'queryid'>

const DEFAULT_GROUP: DrillGroup = 'wait_event_type'

// Rows for 24 groups and "other" in each of the server's 180 buckets. At the
// old 1,000 the server kept only the 4 largest groups of a long range.
const ASH_ROW_LIMIT = 5_000

function AshFootnote() {
  const t = useT()
  return (
    <p className="text-muted-foreground text-xs" data-testid="ash-sampling-footnote">
      {t('ash.footnote')}
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

type ChartView = 'separate' | 'stacked'

// Separate is the default and is never written to the URL.
function readView(value: string | null): ChartView {
  return value === 'stacked' ? 'stacked' : 'separate'
}

interface ChartLayoutToggleProps {
  view: ChartView
  onChange: (view: ChartView) => void
}

const LAYOUTS: readonly {
  value: ChartView
  label: 'ash.layout.separate' | 'ash.layout.stacked'
}[] = [
  { value: 'separate', label: 'ash.layout.separate' },
  { value: 'stacked', label: 'ash.layout.stacked' },
]

function ChartLayoutToggle({ onChange, view }: ChartLayoutToggleProps) {
  const t = useT()
  return (
    <div className="inline-flex items-center gap-1" data-caption-id="ash.chart-layout">
      <div
        aria-label={t('ash.layout.label')}
        className="bg-muted/40 inline-flex items-center gap-1 rounded-lg border p-1 text-sm"
        role="radiogroup"
      >
        {LAYOUTS.map(({ label, value }) => (
          <label
            className={cn(
              'focus-within:ring-ring cursor-pointer rounded-md px-3 py-1 focus-within:ring-2',
              view === value ? 'bg-background font-medium shadow-sm' : 'text-muted-foreground',
            )}
            key={value}
          >
            <input
              checked={view === value}
              className="sr-only"
              name="ash-chart-layout"
              onChange={() => onChange(value)}
              type="radio"
              value={value}
            />
            {t(label)}
          </label>
        ))}
      </div>
      <InfoHint id="ash.chart-layout" label={t('ash.layout.label')} />
    </div>
  )
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
  const t = useT()
  return (
    <nav aria-label={t('ash.drill.label')}>
      <ol className="text-muted-foreground flex flex-wrap items-center gap-2 text-sm">
        <li>
          {groupBy === DEFAULT_GROUP ? (
            <span aria-current="page">{t('ash.drill.types')}</span>
          ) : (
            <button className="underline" onClick={() => onNavigate(DEFAULT_GROUP)} type="button">
              {t('ash.drill.types')}
            </button>
          )}
        </li>
        {groupBy !== DEFAULT_GROUP ? (
          <>
            <li aria-hidden="true">/</li>
            <li>
              {groupBy === 'wait_event' ? (
                <span aria-current="page">
                  {t('ash.drill.events', { value: waitEventType ?? t('ash.drill.all') })}
                </span>
              ) : (
                <button
                  className="underline"
                  onClick={() => onNavigate('wait_event')}
                  type="button"
                >
                  {t('ash.drill.events', { value: waitEvent ?? t('ash.drill.all') })}
                </button>
              )}
            </li>
          </>
        ) : null}
        {groupBy === 'queryid' ? (
          <>
            <li aria-hidden="true">/</li>
            <li aria-current="page">{t('ash.group.queryid')}</li>
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
  const t = useT()
  const { range } = useTimeRange()
  const timeWindow = queryWindowOf(range)
  const instanceId = instanceIdOverride ?? routeInstanceId ?? ''
  const groupBy = readGroup(searchParams.get('group'))
  const waitEventType = searchParams.get('wait_event_type')
  const waitEvent = searchParams.get('wait_event')
  const database = searchParams.get('db') ?? undefined
  const view = readView(searchParams.get('view'))
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

  function changeView(nextView: ChartView) {
    const next = new URLSearchParams(searchParams)
    if (nextView === 'separate') next.delete('view')
    else next.set('view', nextView)
    setSearchParams(next)
  }

  function selectSeries(name: string) {
    if (name === displayName('other') || name.toLocaleLowerCase('en-US') === 'other') return
    const next = new URLSearchParams(searchParams)
    if (groupBy === DEFAULT_GROUP) {
      next.set('group', 'wait_event')
      next.set('wait_event_type', name)
      next.delete('wait_event')
      next.delete('queryid')
    } else if (groupBy === 'wait_event') {
      next.set('group', 'queryid')
      next.set('wait_event', name)
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
        <section aria-busy="true" aria-label={t('ash.page.loadingLabel')} role="status">
          {t('ash.page.loading')}
        </section>
        <AshFootnote />
      </div>
    )
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="wait-events" label={t('ash.page.helpLabel')} />}
        icon={Activity}
        eyebrow={t('ash.page.eyebrow')}
        freshness={<FreshnessBadge dataUpdatedAt={ashQuery.dataUpdatedAt} policy="ash" />}
        {...(groupBy === 'queryid'
          ? {}
          : { actions: <ChartLayoutToggle onChange={changeView} view={view} /> })}
        title={t('ash.page.title')}
        // The breadcrumb names the instance; its id stays out of the title area.
        subtitle={<span>{t(rangeLabelKey(range))}</span>}
      />
      <div className="flex w-full min-w-0 items-center gap-1">
        <Caption className="text-xs" id="ash.page" oneLine />
        <InfoHint className="shrink-0" id="ash.page" label={t('ash.page.title')} />
      </div>
      {groupBy === DEFAULT_GROUP ? null : (
        <DrillBreadcrumb
          groupBy={groupBy}
          onNavigate={navigateTo}
          waitEvent={waitEvent}
          waitEventType={waitEventType}
        />
      )}
      {ashQuery.data.enabled === false ? (
        <>
          <Disabled feature={t('ash.disabled.feature')} configKey={t('ash.disabled.configKey')} />
          <p className="text-muted-foreground text-sm">{t('ash.disabled.note')}</p>
        </>
      ) : (
        <>
          {ashQuery.data.warning ? (
            <Callout role="status" variant="warning">
              <strong>{t('ash.warning.title')}</strong>{' '}
              {t('ash.warning.body', { warning: ashQuery.data.warning, samples: sampleCount })}
            </Callout>
          ) : null}
          {queryIdsUnavailable ? (
            <div className="space-y-2">
              <Degraded
                reason={t(
                  queryIdSetting === 'off' ? 'ash.queryId.reasonOff' : 'ash.queryId.reasonAuto',
                )}
                requires="compute_query_id=on"
              />
              <p className="text-muted-foreground text-sm">
                {queryIdSetting === 'off' ? (
                  <>
                    {t('ash.queryId.readinessBefore')}
                    <Link
                      className="underline"
                      to={`/instances/${encodeURIComponent(instanceId)}?section=readiness`}
                    >
                      {t('ash.queryId.readinessLink')}
                    </Link>
                    {t('ash.queryId.readinessAfter')}
                  </>
                ) : null}
                {t('ash.queryId.seeBefore')}
                <a className="underline" href="/README.md#agent-configuration">
                  {t('ash.queryId.readmeLink')}
                </a>
                {t('ash.queryId.seeAfter')}
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
              view={view}
            />
          )}
          {groupBy === 'queryid' ? (
            <p className="text-muted-foreground text-sm" data-testid="ash-queryid-note">
              {t('ash.queryId.note')}
            </p>
          ) : null}
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
