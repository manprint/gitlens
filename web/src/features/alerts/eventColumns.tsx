import { Link } from 'react-router-dom'

import type { SimpleColumn } from '@/components/layout/SimpleTable'
import type { Translate } from '@/i18n/react'
import { translate } from '@/i18n/store'
import {
  counterResetCount,
  counterResetScope,
  describeEvent,
  payloadInstance,
  type ClusterEvent,
  type EventSeverity,
} from '@/lib/events'
import { TIMESTAMP_COLUMN_WIDTH, formatTimestamp } from '@/lib/format'
import { nameLookup, type NameLookup } from '@/lib/names'

import { severityText } from './AlertRow'
import { clusterCell, clusterTitle, instanceCell, instanceTitle } from './nameCells'

const NO_NAMES = nameLookup(undefined)

const SEVERITY_PILL: Record<EventSeverity, string> = {
  critical: 'bg-destructive/10 text-destructive',
  warning: 'bg-warning/15 text-warning-foreground',
  info: 'bg-primary/10 text-primary',
}

// A counter_reset_detected event carries the number of counters that reset.
function counterResetText(event: ClusterEvent, t: Translate): string | null {
  const count = counterResetCount(event)
  if (count === null) return null
  return t('alerts.events.counterReset', { count, scope: counterResetScope(event) })
}

// The shared title names the internal budget; this list says what happened.
function eventTitle(event: ClusterEvent, t: Translate): string {
  return event.type === 'cardinality_truncated'
    ? t('alerts.events.cardinality')
    : describeEvent(event.type).title
}

/** The full text of the details cell, for its `title` when the cell is cut to one line. */
function detailsText(event: ClusterEvent, t: Translate): string {
  const { whatToCheck } = describeEvent(event.type)
  const unknown = t('shell.topology.unknown')
  if (event.type === 'failover_detected') {
    const oldPrimary = payloadInstance(event, 'old_primary') ?? unknown
    const newPrimary = payloadInstance(event, 'new_primary') ?? unknown
    return `${t('alerts.events.oldPrimary')} ${oldPrimary}; ${t('alerts.events.newPrimary')} ${newPrimary}. ${whatToCheck}`
  }
  if (event.type === 'cluster_id_changed') return t('alerts.events.invariant')
  const resets = event.type === 'counter_reset_detected' ? counterResetText(event, t) : null
  return resets ? `${resets} ${whatToCheck}` : whatToCheck
}

function instanceLink(id: string | null, t: Translate) {
  return id ? (
    <Link
      className="text-primary font-mono text-xs hover:underline"
      to={`/instances/${encodeURIComponent(id)}`}
    >
      {id}
    </Link>
  ) : (
    t('shell.topology.unknown')
  )
}

function detailsCell(event: ClusterEvent, t: Translate) {
  if (event.type === 'failover_detected') {
    return (
      <>
        {t('alerts.events.oldPrimary')} {instanceLink(payloadInstance(event, 'old_primary'), t)};{' '}
        {t('alerts.events.newPrimary')} {instanceLink(payloadInstance(event, 'new_primary'), t)}.{' '}
        <span className="text-muted-foreground">{describeEvent(event.type).whatToCheck}</span>
      </>
    )
  }
  if (event.type === 'cluster_id_changed') {
    return (
      <span className="text-destructive font-semibold" role="alert">
        {t('alerts.events.invariant')}
      </span>
    )
  }
  return <span className="text-muted-foreground">{detailsText(event, t)}</span>
}

/**
 * The columns of the fleet event list: one line per event from 1600 px (D26),
 * newest first, the raw type always shown so an unknown type stays visible.
 */
export function eventColumns(
  names: NameLookup = NO_NAMES,
  t: Translate = translate,
): SimpleColumn<ClusterEvent>[] {
  const unknown = t('shell.topology.unknown')
  return [
    {
      id: 'time',
      header: t('alerts.column.time'),
      width: TIMESTAMP_COLUMN_WIDTH,
      className: 'whitespace-nowrap',
      sortValue: (event) => Date.parse(event.ts),
      firstDirection: 'desc',
      cell: (event) => <time dateTime={event.ts}>{formatTimestamp(event.ts)}</time>,
    },
    {
      id: 'event',
      header: t('alerts.column.event'),
      rowHeader: true,
      width: '16rem',
      truncate: (event) => eventTitle(event, t),
      cell: (event) => eventTitle(event, t),
    },
    {
      id: 'severity',
      header: t('alerts.column.severity'),
      width: '6.5rem',
      cell: (event) => {
        const { severity } = describeEvent(event.type)
        return (
          <span
            className={`rounded-full px-2 py-0.5 text-[0.6875rem] font-semibold tracking-wide uppercase ${SEVERITY_PILL[severity]}`}
          >
            {severityText(severity, t)}
          </span>
        )
      },
    },
    {
      id: 'type',
      header: t('alerts.column.type'),
      width: '14rem',
      sortValue: (event) => event.type,
      truncate: (event) => event.type,
      cell: (event) => <code className="text-xs">{event.type}</code>,
    },
    {
      id: 'cluster',
      header: t('alerts.column.cluster'),
      width: '10rem',
      truncate: (event) => clusterTitle(event.cluster_id, unknown),
      cell: (event) => clusterCell(names, event.cluster_id, unknown),
    },
    {
      id: 'instance',
      header: t('alerts.column.instance'),
      width: '12rem',
      truncate: (event) => instanceTitle(names, event.instance_id, '—'),
      cell: (event) => instanceCell(names, event.instance_id, '—'),
    },
    {
      id: 'details',
      header: t('alerts.column.details'),
      help: 'events.details',
      truncate: (event) => detailsText(event, t),
      cell: (event) => detailsCell(event, t),
    },
  ]
}
