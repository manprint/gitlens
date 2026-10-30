import { History } from 'lucide-react'
import { useEffect, useMemo, useRef } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlerts, useCachedClusters, useEvents, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { usePreferences } from '@/hooks/usePreferences'
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

function suppressionLabel(alert: Alert, suppressionUnknown: boolean): string {
  if (suppressionUnknown) return 'Unknown'
  return alert.suppressed ? 'Suppressed' : 'Not suppressed'
}

function alertStatusColumns(suppressionUnknown: boolean): SimpleColumn<Alert>[] {
  return [
    {
      id: 'alert',
      header: 'Alert',
      rowHeader: true,
      className: 'font-mono text-xs',
      sortValue: (alert) => alert.alert_key,
      truncate: (alert) => alert.alert_key,
      cell: (alert) => alert.alert_key,
    },
    {
      id: 'severity',
      header: 'Severity',
      width: '8rem',
      sortValue: (alert) => alert.severity,
      cell: (alert) => alert.severity,
    },
    {
      id: 'state',
      header: 'State',
      width: '8rem',
      sortValue: (alert) => alert.state,
      cell: (alert) => alert.state,
    },
    {
      id: 'suppression',
      header: 'Suppression',
      width: '10rem',
      cell: (alert) => suppressionLabel(alert, suppressionUnknown),
    },
  ]
}

// Suppressed alerts count under Suppressed, as on the alerts page, so the two
// pages never report different firing totals for the same alerts.
function FiringStatus({ alerts }: { alerts: readonly Alert[] }) {
  const { firing } = summariseAlerts(alerts)
  const firingCount = firing.critical + firing.warning + firing.info
  const evaluation = latestEvaluation(alerts)

  return firingCount === 0 ? (
    <p className="text-sm" role="status">
      No alerts firing. Last evaluated: {evaluation ?? 'no alert evaluation has been recorded'}.
    </p>
  ) : (
    <p className="text-destructive text-sm font-semibold" role="status">
      Firing alerts: {firingCount}
    </p>
  )
}

export function EventsPage() {
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
    () => eventColumns(nameLookup(cachedClusters)),
    [cachedClusters],
  )
  const statusColumns = useMemo(() => alertStatusColumns(suppressionUnknown), [suppressionUnknown])
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
        endpoint="events"
        failure={eventsQuery.error}
        onRetry={() => void eventsQuery.refetch()}
      />
    )
  }
  if (alertsQuery.error && !alertsQuery.data) {
    return (
      <ErrorState
        endpoint="alerts"
        failure={alertsQuery.error}
        onRetry={() => void alertsQuery.refetch()}
      />
    )
  }
  if (eventsQuery.isPending || alertsQuery.isPending || silencesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label="Loading fleet events" role="status">
        Loading fleet events…
      </section>
    )
  }

  const filterLabel = 'flex items-center gap-2 text-sm'
  // From 1600 px the filters sit in the list header at the toolbar height (D26).
  const filterControl = 'min-[1600px]:h-8'

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={History}
        eyebrow="Alerting"
        title="Fleet event timeline"
        subtitle="Everything that happened across the fleet in the selected time range: failovers, restarts, configuration changes and alert transitions."
        freshness={<FreshnessBadge dataUpdatedAt={eventsQuery.dataUpdatedAt} policy="activity" />}
      />

      {eventsQuery.error ? (
        <ErrorState
          endpoint="events"
          failure={eventsQuery.error}
          onRetry={() => void eventsQuery.refetch()}
        />
      ) : null}
      {alertsQuery.error ? (
        <ErrorState
          endpoint="alerts"
          failure={alertsQuery.error}
          onRetry={() => void alertsQuery.refetch()}
        />
      ) : null}
      {silencesQuery.error ? (
        <div className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="alert">
          Suppression state: Unknown because silences could not be loaded. Alert data remains
          visible; retry silences to restore suppression context.
          <button
            className="ml-2 underline"
            onClick={() => void silencesQuery.refetch()}
            type="button"
          >
            Retry silences
          </button>
        </div>
      ) : null}

      {eventLimitReached ? (
        <p className="border-warning/40 bg-warning/10 rounded-lg border p-3 text-sm" role="note">
          The event list is capped at 1,000 results; older events may exist outside this view.
        </p>
      ) : null}

      <AlertListTruncated truncated={alertsQuery.truncated} />
      {/* One view per tab (15.3): the event list, or the alerts with their suppression. */}
      <Tabs onValueChange={showSection} value={section}>
        <div
          className="flex flex-col gap-4 min-[1600px]:flex-row min-[1600px]:items-center min-[1600px]:justify-between"
          data-testid="events-tab-row"
        >
          <TabsList aria-label="Event sections">
            {EVENTS_SECTIONS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {entry.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {/* Alert health is evaluated independently from the event history. */}
          <FiringStatus alerts={alerts} />
        </div>
        <TabsContent value="events">
          <Section
            title="Event timeline"
            description={`${sortedEvents.length} events in the selected range (${range.label}); cluster, event type, range and result limit are kept in the URL.`}
            actions={
              <>
                <label className={filterLabel} htmlFor="events-cluster">
                  <span className="font-medium">Cluster</span>
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
                    placeholder="All clusters"
                    value={clusterId}
                  />
                </label>
                <label className={filterLabel} htmlFor="events-type">
                  <span className="font-medium">Event type</span>
                  <NativeSelect
                    className={filterControl}
                    id="events-type"
                    onChange={(event) =>
                      updateSearchParams(searchParams, setSearchParams, 'type', event.target.value)
                    }
                    value={selectedType}
                  >
                    <option value="">All event types</option>
                    {typeOptions.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                  </NativeSelect>
                </label>
                <label className={filterLabel} htmlFor="events-limit">
                  <span className="font-medium">Event limit</span>
                  <NativeSelect
                    className={filterControl}
                    id="events-limit"
                    onChange={(event) =>
                      updateSearchParams(searchParams, setSearchParams, 'limit', event.target.value)
                    }
                    value={String(eventLimit.effective)}
                  >
                    <option value="100">100 events</option>
                    <option value="500">500 events</option>
                    <option value="1000">1,000 events</option>
                  </NativeSelect>
                </label>
              </>
            }
          >
            <SimpleTable
              ariaLabel="Fleet events"
              columns={eventTableColumns}
              emptyState={
                <EmptyState
                  description="No events match the selected time range and filters."
                  title="No events in this range"
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
            title="Alert status"
            description="Alert health is evaluated independently from the event history."
          >
            <SimpleTable
              ariaLabel="Alert suppression status"
              columns={statusColumns}
              emptyState={<p className="text-muted-foreground text-sm">No alerts are loaded.</p>}
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
