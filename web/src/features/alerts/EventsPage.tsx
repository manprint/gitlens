import { useEffect, useMemo, useRef } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlerts, useEvents, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import type { Alert } from '@/lib/alerts'
import { EventTimeline } from '@/features/cluster/EventTimeline'
import type { ClusterEvent } from '@/lib/events'
import { parseRange } from '@/lib/timerange'

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

function AlertStatus({
  alerts,
  suppressionUnknown,
}: {
  alerts: readonly Alert[]
  suppressionUnknown: boolean
}) {
  const firingAlerts = alerts.filter((alert) => alert.state === 'firing')
  const evaluation = latestEvaluation(alerts)

  return (
    <Section
      title="Alert status"
      description="Alert health is evaluated independently from the event history."
    >
      {firingAlerts.length === 0 ? (
        <p role="status">
          No alerts firing. Last evaluated: {evaluation ?? 'no alert evaluation has been recorded'}.
        </p>
      ) : (
        <p role="status">Firing alerts: {firingAlerts.length}</p>
      )}

      {alerts.length > 0 ? (
        <div aria-label="Alert suppression status" className="mt-4 grid gap-2" role="list">
          {alerts.map((alert) => (
            <div
              className="border-muted flex flex-wrap justify-between gap-2 rounded-md border p-3 text-sm"
              key={alert.alert_key}
              role="listitem"
            >
              <span>{alert.alert_key}</span>
              <span>
                Suppression:{' '}
                {suppressionUnknown
                  ? 'Unknown'
                  : alert.suppressed
                    ? 'Suppressed'
                    : 'Not suppressed'}
              </span>
            </div>
          ))}
        </div>
      ) : null}
    </Section>
  )
}

export function EventsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const location = useLocation()
  const navigate = useNavigate()
  const searchKey = searchParams.toString()
  const range = useMemo(() => parseRange(new URLSearchParams(searchKey), new Date()), [searchKey])
  const redirectedForUnauthorized = useRef(false)
  const clusterId = searchParams.get('cluster_id') ?? ''
  const selectedType = searchParams.get('type') ?? ''
  const eventLimit = readLimit(searchParams.get('limit'))

  const eventsParams = {
    from: range.from.toISOString(),
    limit: eventLimit.effective,
    to: range.to.toISOString(),
    ...(clusterId ? { cluster_id: clusterId } : {}),
    ...(selectedType ? { type: selectedType } : {}),
  }
  const eventsQuery = useEvents(eventsParams)
  const alertsQuery = useAlerts({ state: 'all' })
  const silencesQuery = useSilences({ all: true })

  const events = eventsQuery.data ?? EMPTY_EVENTS
  const alerts = alertsQuery.data ?? EMPTY_ALERTS
  const eventLimitReached =
    eventLimit.requested > MAX_EVENT_LIMIT ||
    (eventLimit.effective === MAX_EVENT_LIMIT && events.length >= MAX_EVENT_LIMIT)
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

  return (
    <div className="space-y-8">
      <PageHeader
        title="Fleet event timeline"
        subtitle="Events across all monitored clusters, with alert health and suppression state kept separate."
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
        <div className="border-warning/40 bg-warning/10 p-3 text-sm" role="alert">
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

      <Section
        title="Filters"
        description="Cluster, event type, range, and result limit are kept in the URL."
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <label className="space-y-1 text-sm" htmlFor="events-cluster">
            <span className="font-medium">Cluster</span>
            <Input
              id="events-cluster"
              onChange={(event) =>
                updateSearchParams(searchParams, setSearchParams, 'cluster_id', event.target.value)
              }
              placeholder="All clusters"
              value={clusterId}
            />
          </label>
          <label className="space-y-1 text-sm" htmlFor="events-limit">
            <span className="font-medium">Event limit</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="events-limit"
              onChange={(event) =>
                updateSearchParams(searchParams, setSearchParams, 'limit', event.target.value)
              }
              value={String(eventLimit.effective)}
            >
              <option value="100">100 events</option>
              <option value="500">500 events</option>
              <option value="1000">1,000 events</option>
            </select>
          </label>
          <p className="text-muted-foreground self-end text-sm">Selected range: {range.label}</p>
        </div>
      </Section>

      {eventLimitReached ? (
        <p className="border-warning/40 bg-warning/10 p-3 text-sm" role="note">
          The event list is capped at 1,000 results; older events may exist outside this view.
        </p>
      ) : null}

      <AlertStatus alerts={alerts} suppressionUnknown={Boolean(silencesQuery.error)} />

      {events.length === 0 ? (
        <EmptyState
          description="No events match the selected time range and filters."
          title="No events in this range"
        />
      ) : (
        <EventTimeline events={events} />
      )}
    </div>
  )
}
