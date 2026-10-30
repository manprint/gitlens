import { Link } from 'react-router-dom'

import type { SimpleColumn } from '@/components/layout/SimpleTable'
import {
  counterResetCount,
  counterResetScope,
  describeEvent,
  payloadInstance,
  type ClusterEvent,
  type EventSeverity,
} from '@/lib/events'
import { formatTimestamp } from '@/lib/format'

const SEVERITY_PILL: Record<EventSeverity, string> = {
  critical: 'bg-destructive/10 text-destructive',
  warning: 'bg-warning/15 text-warning-foreground',
  info: 'bg-primary/10 text-primary',
}

// A counter_reset_detected event carries the number of counters that reset.
function counterResetText(event: ClusterEvent): string | null {
  const count = counterResetCount(event)
  if (count === null) return null
  return `${count} ${count === 1 ? 'counter' : 'counters'} reset${counterResetScope(event)}.`
}

const INVARIANT_TEXT = 'Critical invariant violation I-1: cluster identity must never change.'

/** The full text of the details cell, for its `title` when the cell is cut to one line. */
function detailsText(event: ClusterEvent): string {
  const { whatToCheck } = describeEvent(event.type)
  if (event.type === 'failover_detected') {
    return `Old primary: ${payloadInstance(event, 'old_primary') ?? 'Unknown'}; New primary: ${payloadInstance(event, 'new_primary') ?? 'Unknown'}. ${whatToCheck}`
  }
  if (event.type === 'cluster_id_changed') return INVARIANT_TEXT
  const resets = event.type === 'counter_reset_detected' ? counterResetText(event) : null
  return resets ? `${resets} ${whatToCheck}` : whatToCheck
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

function detailsCell(event: ClusterEvent) {
  if (event.type === 'failover_detected') {
    return (
      <>
        Old primary: {instanceLink(payloadInstance(event, 'old_primary'))}; New primary:{' '}
        {instanceLink(payloadInstance(event, 'new_primary'))}.{' '}
        <span className="text-muted-foreground">{describeEvent(event.type).whatToCheck}</span>
      </>
    )
  }
  if (event.type === 'cluster_id_changed') {
    return (
      <span className="text-destructive font-semibold" role="alert">
        {INVARIANT_TEXT}
      </span>
    )
  }
  return <span className="text-muted-foreground">{detailsText(event)}</span>
}

/**
 * The columns of the fleet event list: one line per event from 1600 px (D26),
 * newest first, the raw type always shown so an unknown type stays visible.
 */
export function eventColumns(): SimpleColumn<ClusterEvent>[] {
  return [
    {
      id: 'time',
      header: 'Time',
      width: '12.5rem',
      className: 'whitespace-nowrap',
      sortValue: (event) => Date.parse(event.ts),
      firstDirection: 'desc',
      cell: (event) => <time dateTime={event.ts}>{formatTimestamp(event.ts, 'UTC')}</time>,
    },
    {
      id: 'event',
      header: 'Event',
      rowHeader: true,
      width: '16rem',
      truncate: (event) => describeEvent(event.type).title,
      cell: (event) => describeEvent(event.type).title,
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
    {
      id: 'cluster',
      header: 'Cluster',
      width: '10rem',
      truncate: (event) => event.cluster_id ?? 'Unknown',
      cell: (event) =>
        event.cluster_id === null ? (
          'Unknown'
        ) : (
          <Link
            className="text-primary font-mono text-xs hover:underline"
            to={`/clusters/${encodeURIComponent(event.cluster_id)}`}
          >
            {event.cluster_id}
          </Link>
        ),
    },
    {
      id: 'instance',
      header: 'Instance',
      width: '12rem',
      truncate: (event) => event.instance_id ?? '—',
      cell: (event) => (event.instance_id === null ? '—' : instanceLink(event.instance_id)),
    },
    {
      id: 'details',
      header: 'Details',
      truncate: detailsText,
      cell: detailsCell,
    },
  ]
}
