import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { describeEvent, sortEventsNewestFirst, type ClusterEvent } from '@/lib/events'
import { formatTimestamp } from '@/lib/format/timestamp'

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

function eventTime(ts: string): string {
  const formatted = formatTimestamp(ts, 'UTC', 'en-GB')
  return formatted === 'Unknown' ? 'Unknown time' : formatted.slice(11, 19)
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

function instanceLink(id: string | null) {
  return id ? <a href={`/instances/${encodeURIComponent(id)}`}>{id}</a> : 'Unknown'
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

  return (
    <article
      className={`border-l-4 p-3 ${
        description.severity === 'critical'
          ? 'border-critical bg-critical/10'
          : description.severity === 'warning'
            ? 'border-warning bg-warning/10'
            : 'border-muted'
      }`}
      data-event-type={event.type}
      data-severity={description.severity}
    >
      <header className="flex flex-wrap items-baseline gap-2">
        <strong>{description.title}</strong>
        <span className="text-muted-foreground text-xs uppercase">{description.severity}</span>
        <time dateTime={event.ts}>
          {eventDay(event.ts)} {eventTime(event.ts)}
        </time>
      </header>
      {unknown ? (
        <p>
          Raw event type: <code>{event.type}</code>
        </p>
      ) : null}
      <p>{description.whatToCheck}</p>
      {event.type === 'failover_detected' ? <FailoverDetails event={event} /> : null}
      {invariantViolation ? (
        <p role="alert" className="font-semibold">
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
    <section aria-labelledby="event-timeline-heading">
      <h2 id="event-timeline-heading">Event timeline</h2>
      <label>
        Event type
        <select
          aria-label="Filter events by type"
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
      {groups.length === 0 ? (
        <p role="status">No events match this filter.</p>
      ) : (
        <div className="space-y-6">
          {groups.map((group) => (
            <section key={group.day} aria-labelledby={`event-day-${group.day}`}>
              <h3 id={`event-day-${group.day}`}>{group.day}</h3>
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
