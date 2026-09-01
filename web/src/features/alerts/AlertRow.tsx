import { Link } from 'react-router-dom'

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
      <Link className="underline" to={`${path}/${encodeURIComponent(value)}`}>
        {value}
      </Link>
    </span>
  )
}

export function AlertRow({ alert, matchingSilence, onSelect }: AlertRowProps) {
  const clusterId = alert.cluster_id === undefined ? undefined : String(alert.cluster_id)
  const instanceId = alert.instance_id === undefined ? undefined : String(alert.instance_id)
  const troubleLink =
    alert.rule_id === 'agent_down' || alert.rule_id === 'instance_unreachable' ? (
      <a className="underline" href="/README.md#troubleshooting">
        View troubleshooting checklist
      </a>
    ) : null

  return (
    <article className="border-muted space-y-3 rounded-md border p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <button
          className="font-medium underline"
          onClick={() => onSelect(alert.alert_key)}
          type="button"
        >
          {alert.alert_key}
        </button>
        <div className="flex gap-2 text-sm">
          <span className="rounded border px-2 py-1">{alert.severity}</span>
          <span className="rounded border px-2 py-1">{alert.state}</span>
        </div>
      </div>
      <p className="text-muted-foreground text-sm">{alert.summary}</p>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {objectLink('/clusters', clusterId, 'Cluster')}
        {objectLink('/instances', instanceId, 'Instance')}
        <span>Started: {alert.started_at}</span>
      </div>
      {alert.suppressed ? (
        <p className="text-warning text-sm" role="status">
          {matchingSilence ? (
            <>
              Suppressed by{' '}
              <Link
                className="underline"
                to={`/alerts?silence_id=${encodeURIComponent(matchingSilence.silence_id)}`}
              >
                {matchingSilence.reason}
              </Link>{' '}
              ({remainingTime(silenceWindow(matchingSilence, new Date()).remainingSeconds)} remaining)
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
