import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import {
  counterResetCount,
  counterResetScope,
  describeEvent,
  payloadInstance,
  sortEventsNewestFirst,
  type ClusterEvent,
  type EventSeverity,
} from '@/lib/events'
import { UNKNOWN } from '@/lib/format/shared'
import { formatTimestamp } from '@/lib/format/timestamp'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { NativeSelect } from '@/components/ui/native-select'

const SEVERITY_PILL: Record<EventSeverity, string> = {
  critical: 'bg-destructive/10 text-destructive',
  warning: 'bg-warning/15 text-warning-foreground',
  info: 'bg-primary/10 text-primary',
}

export interface EventTimelineProps {
  events: readonly ClusterEvent[]
}

// A malformed timestamp has no day or time, so the cell says that once
// instead of showing a placeholder day next to an empty time.
function eventWhen(ts: string): string {
  const formatted = formatTimestamp(ts, 'UTC', 'en-GB')
  if (formatted === UNKNOWN) return 'Unknown time'
  return `${new Date(ts).toISOString().slice(0, 10)} ${formatted.slice(11, 19)}`
}

function instanceLink(id: string | null) {
  return id ? (
    <Link
      className="text-primary font-mono text-xs hover:underline"
      to={`/instances/${encodeURIComponent(id)}`}
    >
      {id}
    </Link>
  ) : (
    'Unknown'
  )
}

const INVARIANT_TEXT = 'Critical invariant violation I-1: cluster identity must never change.'

function resetText(event: ClusterEvent): string | null {
  const count = event.type === 'counter_reset_detected' ? counterResetCount(event) : null
  if (count === null) return null
  return `${count} ${count === 1 ? 'counter' : 'counters'} reset${counterResetScope(event)}.`
}

/** The full text of the details cell, for its `title` when the cell is cut to one line. */
function detailsText(event: ClusterEvent): string {
  const { whatToCheck } = describeEvent(event.type)
  if (event.type === 'cluster_id_changed') return INVARIANT_TEXT
  if (event.type === 'failover_detected') {
    return `Old primary: ${payloadInstance(event, 'old_primary') ?? 'Unknown'}; New primary: ${payloadInstance(event, 'new_primary') ?? 'Unknown'}. ${whatToCheck}`
  }
  const resets = resetText(event)
  return resets ? `${resets} ${whatToCheck}` : whatToCheck
}

function detailsCell(event: ClusterEvent) {
  const { whatToCheck } = describeEvent(event.type)
  if (event.type === 'cluster_id_changed') {
    return (
      <span className="text-destructive font-semibold" role="alert">
        {INVARIANT_TEXT}
      </span>
    )
  }
  if (event.type === 'failover_detected') {
    return (
      <>
        Old primary: {instanceLink(payloadInstance(event, 'old_primary'))}; New primary:{' '}
        {instanceLink(payloadInstance(event, 'new_primary'))}.{' '}
        <span className="text-muted-foreground">{whatToCheck}</span>
      </>
    )
  }
  const resets = resetText(event)
  return (
    <>
      {resets ? <span>{resets}</span> : null}
      {resets ? ' ' : null}
      <span className="text-muted-foreground">{whatToCheck}</span>
    </>
  )
}

// One line per event (D1, 7.5.8): the list pages instead of growing the page.
const COLUMNS: readonly SimpleColumn<ClusterEvent>[] = [
  {
    id: 'time',
    header: 'Time',
    width: '11rem',
    className: 'whitespace-nowrap',
    sortValue: (event) => Date.parse(event.ts) || null,
    firstDirection: 'desc',
    cell: (event) => (
      <time className="tabular-nums" dateTime={event.ts}>
        {eventWhen(event.ts)}
      </time>
    ),
  },
  {
    id: 'event',
    header: 'Event',
    rowHeader: true,
    width: '16rem',
    truncate: (event) => describeEvent(event.type).title,
    cell: (event) => (
      <strong className="font-semibold" data-event-type={event.type}>
        {describeEvent(event.type).title}
      </strong>
    ),
  },
  {
    id: 'severity',
    header: 'Severity',
    width: '6.5rem',
    cell: (event) => {
      const { severity } = describeEvent(event.type)
      return (
        <span
          className={`rounded-full px-2 py-0.5 text-[0.6875rem] font-semibold tracking-wide uppercase ${SEVERITY_PILL[severity]}`}
          data-severity={severity}
        >
          {severity}
        </span>
      )
    },
  },
  {
    id: 'type',
    header: 'Type',
    width: '14rem',
    sortValue: (event) => event.type,
    truncate: (event) => event.type,
    cell: (event) => <code className="text-xs">{event.type}</code>,
  },
  { id: 'details', header: 'Details', truncate: detailsText, cell: detailsCell },
]

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

  function updateType(type: string) {
    const next = new URLSearchParams(searchParams)
    if (type) next.set('type', type)
    else next.delete('type')
    setSearchParams(next)
  }

  return (
    <Section
      actions={
        <label className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Event type</span>
          <NativeSelect
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
          </NativeSelect>
        </label>
      }
      captionId="cluster.events"
      title="Event timeline"
    >
      {visibleEvents.length === 0 ? (
        <p className="text-muted-foreground text-sm" role="status">
          No events match this filter.
        </p>
      ) : (
        <SimpleTable
          ariaLabel="Cluster events"
          captionId="cluster.events-table"
          columns={COLUMNS}
          emptyState={null}
          rowKey={(event) => String(event.event_id)}
          rows={visibleEvents}
          scope="events"
          tableId="cluster-events"
        />
      )}
    </Section>
  )
}
