import { Link } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import { formatTimestamp } from '@/lib/format'

import type { Alert, Silence } from '@/lib/alerts'
import { silenceWindow } from '@/lib/alerts'

interface AlertRowProps {
  alert: Alert
  matchingSilence?: Silence | undefined
  onSelect: (alertKey: string) => void
}

function remainingTime(seconds: number): string {
  if (seconds < 60) return `${Math.ceil(seconds)} seconds`
  if (seconds < 60 * 60) return `${Math.ceil(seconds / 60)} minutes`
  if (seconds < 24 * 60 * 60) return `${Math.ceil(seconds / (60 * 60))} hours`
  return `${Math.ceil(seconds / (24 * 60 * 60))} days`
}

function objectLink(path: string, value: string | undefined, label: string) {
  if (value === undefined) return <span>{label}: Unknown</span>
  return (
    <span>
      {label}:{' '}
      <Link
        className="text-primary font-mono text-xs hover:underline"
        to={`${path}/${encodeURIComponent(value)}`}
      >
        {value}
      </Link>
    </span>
  )
}

const SEVERITY_BORDER: Record<string, string> = {
  critical: 'border-l-destructive',
  warning: 'border-l-warning',
  info: 'border-l-primary',
}

const SEVERITY_BADGE: Record<string, 'destructive' | 'warning' | 'secondary'> = {
  critical: 'destructive',
  warning: 'warning',
  info: 'secondary',
}

export function AlertRow({ alert, matchingSilence, onSelect }: AlertRowProps) {
  // The server sends null for a missing cluster or instance; both read as unknown.
  const clusterId = alert.cluster_id ?? undefined
  const instanceId = alert.instance_id ?? undefined
  const troubleLink =
    alert.rule_id === 'agent_down' || alert.rule_id === 'instance_unreachable' ? (
      <a className="underline" href="/README.md#troubleshooting">
        View troubleshooting checklist
      </a>
    ) : null

  return (
    <article
      className={`bg-card space-y-3 rounded-lg border border-l-4 p-4 ${SEVERITY_BORDER[alert.severity] ?? ''}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <button
          className="text-primary min-w-0 text-left font-mono text-sm font-medium break-all hover:underline"
          onClick={() => onSelect(alert.alert_key)}
          type="button"
        >
          {alert.alert_key}
        </button>
        <div className="flex gap-2 text-sm">
          <Badge variant={SEVERITY_BADGE[alert.severity] ?? 'outline'}>{alert.severity}</Badge>
          <Badge variant={alert.state === 'firing' ? 'destructive' : 'outline'}>
            {alert.state}
          </Badge>
        </div>
      </div>
      <p className="text-muted-foreground text-sm">{alert.summary}</p>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {objectLink('/clusters', clusterId, 'Cluster')}
        {objectLink('/instances', instanceId, 'Instance')}
        <span>
          Started:{' '}
          <time dateTime={alert.started_at}>{formatTimestamp(alert.started_at, 'UTC')}</time>
        </span>
      </div>
      {alert.suppressed ? (
        <p className="text-warning-foreground text-sm" role="status">
          {matchingSilence ? (
            <>
              Suppressed by{' '}
              <Link
                className="underline"
                to={`/alerts?silence_id=${encodeURIComponent(matchingSilence.silence_id)}`}
              >
                {matchingSilence.reason}
              </Link>{' '}
              ({remainingTime(silenceWindow(matchingSilence, new Date()).remainingSeconds)}{' '}
              remaining)
            </>
          ) : (
            'Suppressed; matching silence details are unavailable.'
          )}
        </p>
      ) : null}
      {troubleLink ? <p className="text-sm">{troubleLink}</p> : null}
    </article>
  )
}
