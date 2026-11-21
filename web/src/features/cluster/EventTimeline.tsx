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
import { useT, type Translate } from '@/i18n/react'
import { UNKNOWN } from '@/lib/format/shared'
import { formatTimestamp } from '@/lib/format/timestamp'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { NativeSelect } from '@/components/ui/native-select'

const SEVERITY_PILL: Record<EventSeverity, string> = {
  critical: 'bg-destructive/10 text-destructive',
  warning: 'bg-warning/15 text-warning-foreground',
  info: 'bg-primary/10 text-primary',
}

const SEVERITY_KEY = {
  critical: 'cluster.severity.critical',
  warning: 'cluster.severity.warning',
  info: 'cluster.severity.info',
} as const

export interface EventTimelineProps {
  events: readonly ClusterEvent[]
}

// A malformed timestamp has no day or time, so the cell says that once
// instead of showing a placeholder day next to an empty time.
function eventWhen(ts: string, t: Translate): string {
  const formatted = formatTimestamp(ts, undefined, 'en-GB')
  if (formatted === UNKNOWN) return t('cluster.events.unknownTime')
  return `${new Date(ts).toISOString().slice(0, 10)} ${formatted.slice(11, 19)}`
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
    t('cluster.common.unknown')
  )
}

function resetText(event: ClusterEvent, t: Translate): string | null {
  const count = event.type === 'counter_reset_detected' ? counterResetCount(event) : null
  if (count === null) return null
  return t('cluster.events.resets', { count, scope: counterResetScope(event) })
}

/** The full text of the details cell, for its `title` when the cell is cut to one line. */
function detailsText(event: ClusterEvent, t: Translate): string {
  const { whatToCheck } = describeEvent(event.type)
  if (event.type === 'cluster_id_changed') return t('cluster.events.invariant')
  if (event.type === 'failover_detected') {
    return t('cluster.events.failover', {
      oldPrimary: payloadInstance(event, 'old_primary') ?? t('cluster.common.unknown'),
      newPrimary: payloadInstance(event, 'new_primary') ?? t('cluster.common.unknown'),
      whatToCheck,
    })
  }
  const resets = resetText(event, t)
  return resets ? `${resets} ${whatToCheck}` : whatToCheck
}

function detailsCell(event: ClusterEvent, t: Translate) {
  const { whatToCheck } = describeEvent(event.type)
  if (event.type === 'cluster_id_changed') {
    return (
      <span className="text-destructive font-semibold" role="alert">
        {t('cluster.events.invariant')}
      </span>
    )
  }
  if (event.type === 'failover_detected') {
    return (
      <>
        {t('cluster.events.oldPrimary')} {instanceLink(payloadInstance(event, 'old_primary'), t)};{' '}
        {t('cluster.events.newPrimary')} {instanceLink(payloadInstance(event, 'new_primary'), t)}.{' '}
        <span className="text-muted-foreground">{whatToCheck}</span>
      </>
    )
  }
  const resets = resetText(event, t)
  return (
    <>
      {resets ? <span>{resets}</span> : null}
      {resets ? ' ' : null}
      <span className="text-muted-foreground">{whatToCheck}</span>
    </>
  )
}

// One line per event (D1, 7.5.8): the list pages instead of growing the page.
function eventColumns(t: Translate): readonly SimpleColumn<ClusterEvent>[] {
  return [
    {
      id: 'time',
      header: t('cluster.events.colTime'),
      width: '11rem',
      className: 'whitespace-nowrap',
      sortValue: (event) => Date.parse(event.ts) || null,
      firstDirection: 'desc',
      cell: (event) => (
        <time className="tabular-nums" dateTime={event.ts}>
          {eventWhen(event.ts, t)}
        </time>
      ),
    },
    {
      id: 'event',
      header: t('cluster.events.colEvent'),
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
      header: t('cluster.events.colSeverity'),
      width: '6.5rem',
      cell: (event) => {
        const { severity } = describeEvent(event.type)
        return (
          <span
            className={`rounded-full px-2 py-0.5 text-[0.6875rem] font-semibold tracking-wide uppercase ${SEVERITY_PILL[severity]}`}
            data-severity={severity}
          >
            {t(SEVERITY_KEY[severity])}
          </span>
        )
      },
    },
    {
      id: 'type',
      header: t('cluster.events.colType'),
      width: '14rem',
      sortValue: (event) => event.type,
      truncate: (event) => event.type,
      cell: (event) => <code className="text-xs">{event.type}</code>,
    },
    {
      id: 'details',
      header: t('cluster.events.colDetails'),
      truncate: (event) => detailsText(event, t),
      cell: (event) => detailsCell(event, t),
    },
  ]
}

export function EventTimeline({ events }: EventTimelineProps) {
  const t = useT()
  const columns = useMemo(() => eventColumns(t), [t])
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
        <>
          <label className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">{t('cluster.events.typeLabel')}</span>
            <NativeSelect
              aria-label={t('cluster.events.filterAria')}
              value={selectedType}
              onChange={(event) => updateType(event.target.value)}
            >
              <option value="">{t('cluster.events.allTypes')}</option>
              {[...new Set([...events.map((event) => event.type)])]
                .sort((left, right) => left.localeCompare(right))
                .map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
            </NativeSelect>
          </label>
          <HelpLink anchor="cluster#events" label={t('cluster.events.heading')} />
        </>
      }
      captionId="cluster.events"
      title={t('cluster.events.heading')}
    >
      {visibleEvents.length === 0 ? (
        <p className="text-muted-foreground text-sm" role="status">
          {t('cluster.events.noMatch')}
        </p>
      ) : (
        <SimpleTable
          ariaLabel={t('cluster.events.tableLabel')}
          captionId="cluster.events-table"
          columns={columns}
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
