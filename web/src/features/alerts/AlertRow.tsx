import { Link } from 'react-router-dom'

import type { SimpleColumn } from '@/components/layout/SimpleTable'
import { Badge } from '@/components/ui/badge'
import { formatTimestamp } from '@/lib/format'
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
}

const NO_NAMES = nameLookup(undefined)

function remainingTime(seconds: number): string {
  if (seconds < 60) return `${Math.ceil(seconds)} seconds`
  if (seconds < 60 * 60) return `${Math.ceil(seconds / 60)} minutes`
  if (seconds < 24 * 60 * 60) return `${Math.ceil(seconds / (60 * 60))} hours`
  return `${Math.ceil(seconds / (24 * 60 * 60))} days`
}

const SEVERITY_BADGE: Record<string, 'destructive' | 'warning' | 'secondary'> = {
  critical: 'destructive',
  warning: 'warning',
  info: 'secondary',
}

function hasTroubleshooting(alert: Alert): boolean {
  return alert.rule_id === 'agent_down' || alert.rule_id === 'instance_unreachable'
}

function suppressionText(alert: Alert, silence: Silence | undefined): string | null {
  if (!alert.suppressed) return null
  if (!silence) return 'Suppressed; matching silence details are unavailable.'
  const remaining = remainingTime(silenceWindow(silence, new Date()).remainingSeconds)
  return `Suppressed by ${silence.reason} (${remaining} remaining)`
}

/** The full text of the notes cell, for its `title` when the cell is cut to one line. */
function notesText(alert: Alert, silence: Silence | undefined): string {
  const notes = [
    suppressionText(alert, silence),
    hasTroubleshooting(alert) ? 'View troubleshooting checklist' : null,
  ].filter((note) => note !== null)
  return notes.length === 0 ? '—' : notes.join(' · ')
}

function notesCell(alert: Alert, silence: Silence | undefined) {
  const troubleLink = hasTroubleshooting(alert) ? (
    <a className="underline" href="/README.md#troubleshooting">
      View troubleshooting checklist
    </a>
  ) : null
  const suppression = alert.suppressed ? (
    <span className="text-warning-foreground">
      {silence ? (
        <>
          Suppressed by{' '}
          <Link
            className="underline"
            to={`/alerts?silence_id=${encodeURIComponent(silence.silence_id)}`}
          >
            {silence.reason}
          </Link>{' '}
          ({remainingTime(silenceWindow(silence, new Date()).remainingSeconds)} remaining)
        </>
      ) : (
        'Suppressed; matching silence details are unavailable.'
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
}: AlertColumnsOptions): SimpleColumn<Alert>[] {
  return [
    {
      id: 'alert',
      header: 'Alert',
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
      header: 'Severity',
      width: '6rem',
      cell: (alert) => (
        <Badge variant={SEVERITY_BADGE[alert.severity] ?? 'outline'}>{alert.severity}</Badge>
      ),
    },
    {
      id: 'state',
      header: 'State',
      width: '6rem',
      sortValue: (alert) => alert.state,
      cell: (alert) => (
        <Badge variant={alert.state === 'firing' ? 'destructive' : 'outline'}>{alert.state}</Badge>
      ),
    },
    {
      id: 'summary',
      header: 'Summary',
      className: 'text-muted-foreground',
      truncate: (alert) => alert.summary,
      cell: (alert) => alert.summary,
    },
    {
      id: 'cluster',
      header: 'Cluster',
      width: '10rem',
      // The server sends null for a missing cluster or instance; both read as unknown.
      truncate: (alert) => clusterTitle(alert.cluster_id, 'Unknown'),
      cell: (alert) => clusterCell(names, alert.cluster_id, 'Unknown'),
    },
    {
      id: 'instance',
      header: 'Instance',
      width: '12rem',
      truncate: (alert) => instanceTitle(names, alert.instance_id, 'Unknown'),
      cell: (alert) => instanceCell(names, alert.instance_id, 'Unknown'),
    },
    {
      id: 'started',
      header: 'Started',
      width: '12.5rem',
      className: 'whitespace-nowrap',
      sortValue: (alert) => Date.parse(alert.started_at),
      cell: (alert) => (
        <time dateTime={alert.started_at}>{formatTimestamp(alert.started_at, 'UTC')}</time>
      ),
    },
    {
      id: 'notes',
      header: 'Notes',
      width: '16rem',
      help: 'alerts.notes',
      truncate: (alert) => notesText(alert, silenceFor(alert)),
      cell: (alert) => notesCell(alert, silenceFor(alert)),
    },
  ]
}
