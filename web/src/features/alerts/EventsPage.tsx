import { History } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlerts, useCachedClusters, useEvents, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { InfoHint } from '@/components/layout/InfoHint'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { usePreferences } from '@/hooks/usePreferences'
import { useT, type Translate } from '@/i18n/react'
import { formatCount } from '@/lib/format'
import { summariseAlerts, type Alert } from '@/lib/alerts'
import { sortEventsNewestFirst, type ClusterEvent } from '@/lib/events'
import { nameLookup } from '@/lib/names'
import { parseRange, queryWindowOf } from '@/lib/timerange'
import { NativeSelect } from '@/components/ui/native-select'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  DEFAULT_EVENTS_SECTION,
  EVENTS_SECTIONS,
  readEventsSection,
  withSection,
} from './alertingSections'
import { severityText, stateText } from './AlertRow'
import { eventColumns } from './eventColumns'

const DEFAULT_EVENT_LIMIT = 100
const MAX_EVENT_LIMIT = 1_000
const EMPTY_ALERTS: Alert[] = []
const EMPTY_EVENTS: ClusterEvent[] = []

function readLimit(value: string | null): { requested: number; effective: number } {
  const parsed = value === null ? DEFAULT_EVENT_LIMIT : Number.parseInt(value, 10)
  if (!Number.isInteger(parsed) || parsed < 1) {
    return { requested: DEFAULT_EVENT_LIMIT, effective: DEFAULT_EVENT_LIMIT }
  }
  return { requested: parsed, effective: Math.min(parsed, MAX_EVENT_LIMIT) }
}

function updateSearchParams(
  searchParams: URLSearchParams,
  setSearchParams: (next: URLSearchParams) => void,
  key: string,
  value: string,
) {
  const next = new URLSearchParams(searchParams)
  if (value) next.set(key, value)
  else next.delete(key)
  // A narrower list starts on its first page (D23).
  next.delete('events.page')
  setSearchParams(next)
}

function latestEvaluation(alerts: readonly Alert[]): string | null {
  let latest: { timestamp: string; milliseconds: number } | null = null
  for (const alert of alerts) {
    const milliseconds = Date.parse(alert.last_eval_at)
    if (!Number.isFinite(milliseconds)) continue
    if (latest === null || milliseconds > latest.milliseconds) {
      latest = { milliseconds, timestamp: alert.last_eval_at }
    }
  }
  return latest?.timestamp ?? null
}

function suppressionLabel(alert: Alert, suppressionUnknown: boolean, t: Translate): string {
  if (suppressionUnknown) return t('shell.topology.unknown')
  return alert.suppressed ? t('alerts.status.suppressed') : t('alerts.filter.notSuppressed')
}

function alertStatusColumns(suppressionUnknown: boolean, t: Translate): SimpleColumn<Alert>[] {
  return [
    {
      id: 'alert',
      header: t('alerts.column.alert'),
      rowHeader: true,
      className: 'font-mono text-xs',
      sortValue: (alert) => alert.alert_key,
      truncate: (alert) => alert.alert_key,
      cell: (alert) => alert.alert_key,
    },
    {
      id: 'severity',
      header: t('alerts.column.severity'),
      width: '8rem',
      sortValue: (alert) => alert.severity,
      cell: (alert) => severityText(alert.severity, t),
    },
    {
      id: 'state',
      header: t('alerts.column.state'),
      width: '8rem',
      sortValue: (alert) => alert.state,
      cell: (alert) => stateText(alert.state, t),
    },
    {
      id: 'suppression',
      header: t('alerts.filter.suppression'),
      help: 'alerts.suppression',
      width: '10rem',
      cell: (alert) => suppressionLabel(alert, suppressionUnknown, t),
    },
  ]
}

// Suppressed alerts count under Suppressed, as on the alerts page, so the two
// pages never report different firing totals for the same alerts.
function FiringStatus({ alerts }: { alerts: readonly Alert[] }) {
  const t = useT()
  const { firing } = summariseAlerts(alerts)
  const firingCount = firing.critical + firing.warning + firing.info
  const evaluation = latestEvaluation(alerts)

  return firingCount === 0 ? (
    <p className="text-sm" role="status">
      {t('alerts.status.noneFiring', {
        evaluation: evaluation ?? t('alerts.status.noEvaluation'),
      })}
    </p>
  ) : (
    <p className="text-destructive text-sm font-semibold" role="status">
      {t('alerts.status.firingCount', { count: firingCount })}
    </p>
  )
}

export function EventsPage() {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const location = useLocation()
  const navigate = useNavigate()
  // The header's range picker and this page read the same default (11.2).
  const { defaultTimeRange } = usePreferences()
  const range = parseRange(searchParams, new Date(), defaultTimeRange)
  const redirectedForUnauthorized = useRef(false)
  const clusterId = searchParams.get('cluster_id') ?? ''
  const selectedType = searchParams.get('type') ?? ''
  const eventLimit = readLimit(searchParams.get('limit'))

  const eventsParams = {
    limit: eventLimit.effective,
    ...(clusterId ? { cluster_id: clusterId } : {}),
    ...(selectedType ? { type: selectedType } : {}),
  }
  const eventsQuery = useEvents(eventsParams, queryWindowOf(range))
  const alertsQuery = useAlerts({ state: 'all' })
  const silencesQuery = useSilences({ all: true })

  const events = eventsQuery.data ?? EMPTY_EVENTS
  const alerts = alertsQuery.data ?? EMPTY_ALERTS
  const eventLimitReached =
    eventLimit.requested > MAX_EVENT_LIMIT ||
    (eventLimit.effective === MAX_EVENT_LIMIT && events.length >= MAX_EVENT_LIMIT)
  const suppressionUnknown = Boolean(silencesQuery.error)
  const sortedEvents = useMemo(() => sortEventsNewestFirst(events), [events])
  const typeOptions = useMemo(
    () =>
      [
        ...new Set([...events.map((event) => event.type), ...(selectedType ? [selectedType] : [])]),
      ].sort((left, right) => left.localeCompare(right)),
    [events, selectedType],
  )
  // Cluster and instance names come from the list the shell polls (D14, D15).
  const cachedClusters = useCachedClusters()
  const eventTableColumns = useMemo(
    () => eventColumns(nameLookup(cachedClusters), t),
    [cachedClusters, t],
  )
  const statusColumns = useMemo(
    () => alertStatusColumns(suppressionUnknown, t),
    [suppressionUnknown, t],
  )
  const section = readEventsSection(searchParams.get('section'))

  function showSection(next: string) {
    setSearchParams((current) => withSection(current, next, DEFAULT_EVENTS_SECTION), {
      replace: true,
    })
  }

  const unauthorized =
    eventsQuery.error?.kind === 'unauthorized' ||
    alertsQuery.error?.kind === 'unauthorized' ||
    silencesQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  if (eventsQuery.error && !eventsQuery.data) {
    return (
      <ErrorState
        endpoint={t('alerts.endpoint.events')}
        failure={eventsQuery.error}
        onRetry={() => void eventsQuery.refetch()}
      />
    )
  }
  if (alertsQuery.error && !alertsQuery.data) {
    return (
      <ErrorState
        endpoint={t('alerts.endpoint.alerts')}
        failure={alertsQuery.error}
        onRetry={() => void alertsQuery.refetch()}
      />
    )
  }
  if (eventsQuery.isPending || alertsQuery.isPending || silencesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label={t('alerts.events.loadingLabel')} role="status">
        {t('alerts.events.loading')}
      </section>
    )
  }

  const filterLabel = 'flex items-center gap-2 text-sm'
  // From 1600 px the filters sit in the list header at the toolbar height (D26).
  const filterControl = 'min-[1600px]:h-8'

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="events" label={t('alerts.events.helpLabel')} />}
        icon={History}
        eyebrow={t('alerts.page.eyebrow')}
        title={t('alerts.events.title')}
        subtitle={t('alerts.events.subtitle')}
        freshness={<FreshnessBadge dataUpdatedAt={eventsQuery.dataUpdatedAt} policy="activity" />}
      />

      {eventsQuery.error ? (
        <ErrorState
          endpoint={t('alerts.endpoint.events')}
          failure={eventsQuery.error}
          onRetry={() => void eventsQuery.refetch()}
        />
      ) : null}
      {alertsQuery.error ? (
        <ErrorState
          endpoint={t('alerts.endpoint.alerts')}
          failure={alertsQuery.error}
          onRetry={() => void alertsQuery.refetch()}
        />
      ) : null}
      {silencesQuery.error ? (
        <div className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="alert">
          {t('alerts.events.suppressionUnknown')}
          <button
            className="ml-2 underline"
            onClick={() => void silencesQuery.refetch()}
            type="button"
          >
            {t('alerts.callout.retry')}
          </button>
        </div>
      ) : null}

      {eventLimitReached ? (
        <p className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="note">
          {t('alerts.events.capped', { limit: formatCount(1000) })}
        </p>
      ) : null}

      <AlertListTruncated truncated={alertsQuery.truncated} />
      {/* One view per tab (15.3): the event list, or the alerts with their suppression. */}
      <Tabs onValueChange={showSection} value={section}>
        <div
          className="flex flex-col gap-4 min-[1600px]:flex-row min-[1600px]:items-center min-[1600px]:justify-between"
          data-testid="events-tab-row"
        >
          <TabsList aria-label={t('alerts.events.sectionsLabel')}>
            {EVENTS_SECTIONS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {t(entry.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
          {/* Alert health is evaluated independently from the event history. */}
          <FiringStatus alerts={alerts} />
        </div>
        <TabsContent value="events">
          <Section
            captionId="events.timeline"
            title={t('alerts.events.timelineTitle')}
            actions={
              <>
                <span className="flex items-center gap-1">
                  <label className={filterLabel} htmlFor="events-cluster">
                    <span className="font-medium">{t('alerts.column.cluster')}</span>
                    <Input
                      className={`w-44 ${filterControl}`}
                      id="events-cluster"
                      onChange={(event) =>
                        updateSearchParams(
                          searchParams,
                          setSearchParams,
                          'cluster_id',
                          event.target.value,
                        )
                      }
                      placeholder={t('alerts.events.allClusters')}
                      value={clusterId}
                    />
                  </label>
                  <InfoHint id="events.filter-cluster" label={t('alerts.filter.clusterHint')} />
                </span>
                <span className="flex items-center gap-1">
                  <label className={filterLabel} htmlFor="events-type">
                    <span className="font-medium">{t('alerts.events.typeLabel')}</span>
                    <NativeSelect
                      className={filterControl}
                      id="events-type"
                      onChange={(event) =>
                        updateSearchParams(
                          searchParams,
                          setSearchParams,
                          'type',
                          event.target.value,
                        )
                      }
                      value={selectedType}
                    >
                      <option value="">{t('alerts.events.allTypes')}</option>
                      {typeOptions.map((type) => (
                        <option key={type} value={type}>
                          {type}
                        </option>
                      ))}
                    </NativeSelect>
                  </label>
                  <InfoHint id="events.filter-type" label={t('alerts.events.typeHint')} />
                </span>
                <span className="flex items-center gap-1">
                  <label className={filterLabel} htmlFor="events-limit">
                    <span className="font-medium">{t('alerts.events.limitLabel')}</span>
                    <NativeSelect
                      className={filterControl}
                      id="events-limit"
                      onChange={(event) =>
                        updateSearchParams(
                          searchParams,
                          setSearchParams,
                          'limit',
                          event.target.value,
                        )
                      }
                      value={String(eventLimit.effective)}
                    >
                      <option value="100">
                        {t('alerts.events.limitOption', { count: formatCount(100) })}
                      </option>
                      <option value="500">
                        {t('alerts.events.limitOption', { count: formatCount(500) })}
                      </option>
                      <option value="1000">
                        {t('alerts.events.limitOption', { count: formatCount(1000) })}
                      </option>
                    </NativeSelect>
                  </label>
                  <InfoHint id="events.filter-limit" label={t('alerts.events.limitHint')} />
                </span>
                <HelpLink anchor="events#timeline" label={t('alerts.events.timelineTitle')} />
              </>
            }
          >
            <SimpleTable
              ariaLabel={t('alerts.events.tableLabel')}
              captionId="events.table"
              columns={eventTableColumns}
              emptyState={
                <EmptyState
                  description={t('alerts.events.emptyDescription')}
                  title={t('alerts.events.emptyTitle')}
                />
              }
              rowKey={(event) => String(event.event_id)}
              rows={sortedEvents}
              scope="events"
              tableId="events"
            />
          </Section>
        </TabsContent>
        <TabsContent value="alerts">
          <Section
            actions={
              <HelpLink anchor="events#alert-status" label={t('alerts.section.alertStatus')} />
            }
            captionId="events.alert-status"
            title={t('alerts.section.alertStatus')}
          >
            <SimpleTable
              ariaLabel={t('alerts.events.alertStatusLabel')}
              captionId="events.alert-status-table"
              columns={statusColumns}
              emptyState={
                <p className="text-muted-foreground text-sm">{t('alerts.events.noAlerts')}</p>
              }
              rowKey={(alert) => alert.alert_key}
              rows={alerts}
              scope="alerts"
              tableId="alert-status"
            />
          </Section>
        </TabsContent>
      </Tabs>
    </div>
  )
}
