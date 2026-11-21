import { Link } from 'react-router-dom'

import type { SimpleColumn } from '@/components/layout/SimpleTable'
import { Badge } from '@/components/ui/badge'
import type { MessageKey } from '@/i18n/messages/en'
import type { Translate } from '@/i18n/react'
import { translate } from '@/i18n/store'
import { TIMESTAMP_COLUMN_WIDTH, formatTimestamp } from '@/lib/format'
import { nameLookup, type NameLookup } from '@/lib/names'

import type { Alert, Silence } from '@/lib/alerts'
import { silenceWindow } from '@/lib/alerts'

import { clusterCell, clusterTitle, instanceCell, instanceTitle } from './nameCells'

interface AlertColumnsOptions {
  onSelect: (alertKey: string) => void
  /** The active silence that suppresses a suppressed alert, when it is known. */
  silenceFor: (alert: Alert) => Silence | undefined
  /** Names of the clusters and instances the rows point at; ids are shown without it. */
  names?: NameLookup
  /** Translates the column text; the store's translate when omitted. */
  t?: Translate
}

const NO_NAMES = nameLookup(undefined)

function remainingTime(seconds: number, t: Translate): string {
  if (seconds < 60) return t('alerts.time.seconds', { count: Math.ceil(seconds) })
  if (seconds < 60 * 60) return t('alerts.time.minutes', { count: Math.ceil(seconds / 60) })
  if (seconds < 24 * 60 * 60) {
    return t('alerts.time.hours', { count: Math.ceil(seconds / (60 * 60)) })
  }
  return t('alerts.time.days', { count: Math.ceil(seconds / (24 * 60 * 60)) })
}

const SEVERITY_BADGE: Record<string, 'destructive' | 'warning' | 'secondary'> = {
  critical: 'destructive',
  warning: 'warning',
  info: 'secondary',
}

const SEVERITY_KEY: Readonly<Record<string, MessageKey | undefined>> = {
  critical: 'alerts.severity.critical',
  warning: 'alerts.severity.warning',
  info: 'alerts.severity.info',
}

const STATE_KEY: Readonly<Record<string, MessageKey | undefined>> = {
  firing: 'alerts.state.firing',
  resolved: 'alerts.state.resolved',
}

/** The label of a severity; a value the console does not know shows as the server sent it. */
export function severityText(severity: string, t: Translate): string {
  const key = SEVERITY_KEY[severity]
  return key ? t(key) : severity
}

/** The label of an alert state, with the same fallback as severityText. */
export function stateText(state: string, t: Translate): string {
  const key = STATE_KEY[state]
  return key ? t(key) : state
}

function hasTroubleshooting(alert: Alert): boolean {
  return alert.rule_id === 'agent_down' || alert.rule_id === 'instance_unreachable'
}

function suppressionText(alert: Alert, silence: Silence | undefined, t: Translate): string | null {
  if (!alert.suppressed) return null
  if (!silence) return t('alerts.row.suppressedNoDetail')
  const remaining = remainingTime(silenceWindow(silence, new Date()).remainingSeconds, t)
  return `${t('alerts.row.suppressedBy')} ${silence.reason} ${t('alerts.row.suppressedRemaining', { remaining })}`
}

/** The full text of the notes cell, for its `title` when the cell is cut to one line. */
function notesText(alert: Alert, silence: Silence | undefined, t: Translate): string {
  const notes = [
    suppressionText(alert, silence, t),
    hasTroubleshooting(alert) ? t('alerts.row.troubleshooting') : null,
  ].filter((note) => note !== null)
  return notes.length === 0 ? '—' : notes.join(' · ')
}

function notesCell(alert: Alert, silence: Silence | undefined, t: Translate) {
  const troubleLink = hasTroubleshooting(alert) ? (
    <a className="underline" href="/README.md#troubleshooting">
      {t('alerts.row.troubleshooting')}
    </a>
  ) : null
  const suppression = alert.suppressed ? (
    <span className="text-warning-foreground">
      {silence ? (
        <>
          {t('alerts.row.suppressedBy')}{' '}
          <Link
            className="underline"
            to={`/alerts?silence_id=${encodeURIComponent(silence.silence_id)}`}
          >
            {silence.reason}
          </Link>{' '}
          {t('alerts.row.suppressedRemaining', {
            remaining: remainingTime(silenceWindow(silence, new Date()).remainingSeconds, t),
          })}
        </>
      ) : (
        t('alerts.row.suppressedNoDetail')
      )}
    </span>
  ) : null

  if (!suppression && !troubleLink) return <span className="text-muted-foreground">—</span>
  return (
    <>
      {suppression}
      {suppression && troubleLink ? ' · ' : null}
      {troubleLink}
    </>
  )
}

/**
 * The columns of the alert list: one line per alert from 1600 px (D26), the
 * alert key opens its detail, free text is cut with the full text in `title`.
 */
export function alertColumns({
  onSelect,
  silenceFor,
  names = NO_NAMES,
  t = translate,
}: AlertColumnsOptions): SimpleColumn<Alert>[] {
  const unknown = t('shell.topology.unknown')
  return [
    {
      id: 'alert',
      header: t('alerts.column.alert'),
      rowHeader: true,
      width: '18%',
      sortValue: (alert) => alert.alert_key,
      cell: (alert) => (
        <button
          className="text-primary min-w-0 text-left font-mono text-sm font-medium break-all hover:underline min-[1600px]:block min-[1600px]:max-w-full min-[1600px]:truncate min-[1600px]:break-normal"
          onClick={() => onSelect(alert.alert_key)}
          title={alert.alert_key}
          type="button"
        >
          {alert.alert_key}
        </button>
      ),
    },
    {
      id: 'severity',
      header: t('alerts.column.severity'),
      width: '6rem',
      cell: (alert) => (
        <Badge variant={SEVERITY_BADGE[alert.severity] ?? 'outline'}>
          {severityText(alert.severity, t)}
        </Badge>
      ),
    },
    {
      id: 'state',
      header: t('alerts.column.state'),
      width: '6rem',
      sortValue: (alert) => alert.state,
      cell: (alert) => (
        <Badge variant={alert.state === 'firing' ? 'destructive' : 'outline'}>
          {stateText(alert.state, t)}
        </Badge>
      ),
    },
    {
      id: 'summary',
      header: t('alerts.column.summary'),
      className: 'text-muted-foreground',
      truncate: (alert) => alert.summary,
      cell: (alert) => alert.summary,
    },
    {
      id: 'cluster',
      header: t('alerts.column.cluster'),
      width: '10rem',
      // The server sends null for a missing cluster or instance; both read as unknown.
      truncate: (alert) => clusterTitle(alert.cluster_id, unknown),
      cell: (alert) => clusterCell(names, alert.cluster_id, unknown),
    },
    {
      id: 'instance',
      header: t('alerts.column.instance'),
      width: '12rem',
      truncate: (alert) => instanceTitle(names, alert.instance_id, unknown),
      cell: (alert) => instanceCell(names, alert.instance_id, unknown),
    },
    {
      id: 'started',
      header: t('alerts.column.started'),
      width: TIMESTAMP_COLUMN_WIDTH,
      className: 'whitespace-nowrap',
      sortValue: (alert) => Date.parse(alert.started_at),
      cell: (alert) => <time dateTime={alert.started_at}>{formatTimestamp(alert.started_at)}</time>,
    },
    {
      id: 'notes',
      header: t('alerts.column.notes'),
      width: '16rem',
      help: 'alerts.notes',
      truncate: (alert) => notesText(alert, silenceFor(alert), t),
      cell: (alert) => notesCell(alert, silenceFor(alert), t),
    },
  ]
}
