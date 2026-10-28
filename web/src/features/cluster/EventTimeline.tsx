import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import {
  describeEvent,
  sortEventsNewestFirst,
  type ClusterEvent,
  type EventSeverity,
} from '@/lib/events'
import { UNKNOWN } from '@/lib/format/shared'
import { formatTimestamp } from '@/lib/format/timestamp'

const SEVERITY_BORDER: Record<EventSeverity, string> = {
  critical: 'border-l-destructive',
  warning: 'border-l-warning',
  info: 'border-l-info',
}

const SEVERITY_PILL: Record<EventSeverity, string> = {
  critical: 'bg-destructive/10 text-destructive',
  warning: 'bg-warning/15 text-warning-foreground',
  info: 'bg-primary/10 text-primary',
}

export interface EventTimelineProps {
  events: readonly ClusterEvent[]
}

interface EventDay {
  day: string
  events: ClusterEvent[]
}

function eventDay(ts: string): string {
  const date = new Date(ts)
  return Number.isNaN(date.getTime()) ? 'Unknown day' : date.toISOString().slice(0, 10)
}

// A malformed timestamp has no day or time, so the element says that once
// instead of showing the day's placeholder next to an empty time.
function eventWhen(ts: string): string {
  const formatted = formatTimestamp(ts, 'UTC', 'en-GB')
  return formatted === UNKNOWN ? 'Unknown time' : `${eventDay(ts)} ${formatted.slice(11, 19)}`
}

function groupEvents(events: readonly ClusterEvent[]): EventDay[] {
  const groups = new Map<string, ClusterEvent[]>()
  for (const event of sortEventsNewestFirst(events)) {
    const day = eventDay(event.ts)
    const group = groups.get(day)
    if (group) group.push(event)
    else groups.set(day, [event])
  }
  return [...groups.entries()].map(([day, groupedEvents]) => ({ day, events: groupedEvents }))
}

function payloadInstance(event: ClusterEvent, key: string): string | null {
  const value = event.payload[key]
  return typeof value === 'string' && value.length > 0 ? value : null
}

// A counter_reset_detected event is written per database and carries the number
// of counters that reset, so the card can say how many.
function counterResetCount(event: ClusterEvent): number | null {
  const value = event.payload.count
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 ? value : null
}

// The event names its database, empty for a result that covers the whole
// instance. An event written before the server sent the name says neither.
function counterResetScope(event: ClusterEvent): string {
  const database = event.payload.database
  if (typeof database !== 'string') return ''
  return database === '' ? ' on this instance' : ` in database ${database}`
}

function instanceLink(id: string | null) {
  return id ? (
    <a
      className="text-primary font-mono text-xs hover:underline"
      href={`/instances/${encodeURIComponent(id)}`}
    >
      {id}
    </a>
  ) : (
    'Unknown'
  )
}

function FailoverDetails({ event }: { event: ClusterEvent }) {
  return (
    <p>
      Old primary: {instanceLink(payloadInstance(event, 'old_primary'))}; New primary:{' '}
      {instanceLink(payloadInstance(event, 'new_primary'))}
    </p>
  )
}

function EventCard({ event }: { event: ClusterEvent }) {
  const description = describeEvent(event.type)
  const unknown = description.title === 'Unknown event type'
  const invariantViolation = event.type === 'cluster_id_changed'
  const resetCount = event.type === 'counter_reset_detected' ? counterResetCount(event) : null

  return (
    <article
      className={`bg-card relative space-y-1.5 rounded-lg border border-l-4 py-3 pr-4 pl-4 text-sm ${SEVERITY_BORDER[description.severity]}`}
      data-event-type={event.type}
      data-severity={description.severity}
    >
      <header className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <strong className="font-semibold">{description.title}</strong>
        <span
          className={`rounded-full px-2 py-0.5 text-[0.6875rem] font-semibold tracking-wide uppercase ${SEVERITY_PILL[description.severity]}`}
        >
          {description.severity}
        </span>
        <time className="text-muted-foreground ml-auto text-xs tabular-nums" dateTime={event.ts}>
          {eventWhen(event.ts)}
        </time>
      </header>
      {unknown ? (
        <p>
          Raw event type: <code>{event.type}</code>
        </p>
      ) : null}
      <p className="text-muted-foreground">{description.whatToCheck}</p>
      {resetCount !== null ? (
        <p>
          {resetCount} {resetCount === 1 ? 'counter' : 'counters'} reset
          {counterResetScope(event)}.
        </p>
      ) : null}
      {event.type === 'failover_detected' ? <FailoverDetails event={event} /> : null}
      {invariantViolation ? (
        <p role="alert" className="text-destructive font-semibold">
          Critical invariant violation I-1: cluster identity must never change.
        </p>
      ) : null}
    </article>
  )
}

export function EventTimeline({ events }: EventTimelineProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedType = searchParams.get('type') ?? ''
  const visibleEvents = useMemo(
    () =>
      sortEventsNewestFirst(
        selectedType ? events.filter((event) => event.type === selectedType) : events,
      ),
    [events, selectedType],
  )
  const groups = useMemo(() => groupEvents(visibleEvents), [visibleEvents])

  function updateType(type: string) {
    const next = new URLSearchParams(searchParams)
    if (type) next.set('type', type)
    else next.delete('type')
    setSearchParams(next)
  }

  return (
    <section
      aria-labelledby="event-timeline-heading"
      className="bg-card shadow-card rounded-xl border"
    >
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
        <h2 id="event-timeline-heading" className="text-base font-semibold">
          Event timeline
        </h2>
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Event type</span>
          <select
            aria-label="Filter events by type"
            className="border-input bg-background h-9 rounded-md border px-3 text-sm"
            value={selectedType}
            onChange={(event) => updateType(event.target.value)}
          >
            <option value="">All event types</option>
            {[...new Set([...events.map((event) => event.type)])]
              .sort((left, right) => left.localeCompare(right))
              .map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
          </select>
        </label>
      </div>
      {groups.length === 0 ? (
        <p className="text-muted-foreground p-5 text-sm" role="status">
          No events match this filter.
        </p>
      ) : (
        <div className="space-y-6 p-5">
          {groups.map((group) => (
            <section key={group.day} aria-labelledby={`event-day-${group.day}`}>
              <h3
                id={`event-day-${group.day}`}
                className="text-muted-foreground mb-2 text-xs font-semibold tracking-wide uppercase"
              >
                {group.day}
              </h3>
              <div className="space-y-2">
                {group.events.map((event) => (
                  <EventCard key={event.event_id} event={event} />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </section>
  )
}
