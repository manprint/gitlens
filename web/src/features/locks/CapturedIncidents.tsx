import { useMemo, useState, type ReactNode } from 'react'

import { useEvents } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { useT } from '@/i18n/react'
import { formatTimestamp } from '@/lib/format'
import {
  INCIDENT_EVENT_TYPES,
  parseIncidents,
  type CapturedIncident,
  type IncidentSession,
} from '@/lib/incidents'

const WINDOW = { durationMs: 7 * 24 * 60 * 60 * 1_000 }
const LIMIT = 100
const NONE = '—'

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-sm break-words">{children}</dd>
    </div>
  )
}

function SessionCard({ session }: { session: IncidentSession }) {
  const t = useT()
  return (
    <li className="space-y-2 rounded-md border p-3" data-testid="incident-session">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t('locks.sessions.pid')}>{session.pid}</Field>
        <Field label={t('locks.sessions.user')}>{session.user || NONE}</Field>
        <Field label={t('locks.sessions.application')}>{session.application || NONE}</Field>
        <Field label={t('locks.sessions.client')}>{session.client || NONE}</Field>
        <Field label={t('locks.sessions.state')}>{session.state || NONE}</Field>
        <Field label={t('locks.incidents.timing')}>{session.timing || NONE}</Field>
        <Field label={t('locks.incidents.waitsFor')}>{session.waits || NONE}</Field>
        <Field label={t('locks.detail.blockedBy')}>
          {session.blockedBy.length > 0
            ? t('locks.incidents.blockedByPids', { pids: session.blockedBy.join(', ') })
            : NONE}
        </Field>
        <Field label={t('locks.incidents.holdsRow')}>{session.holdsRow || NONE}</Field>
        <Field label={t('locks.incidents.holdsTables')}>
          {session.holdsTables.length > 0 ? session.holdsTables.join(', ') : NONE}
        </Field>
        <Field label={t('locks.incidents.xminAge')}>{session.xminAge || NONE}</Field>
      </dl>
      {session.query ? (
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="text-muted-foreground text-xs">{t('locks.incidents.statement')}</span>
            <CopyButton
              subject={t('locks.incidents.statementOfPid', { pid: session.pid })}
              text={session.query}
            />
          </div>
          <pre className="bg-muted max-h-48 overflow-auto rounded p-2 text-xs whitespace-pre-wrap">
            {session.query}
          </pre>
        </div>
      ) : null}
    </li>
  )
}

function IncidentDetail({ incident }: { incident: CapturedIncident }) {
  const t = useT()
  return (
    <div className="min-w-0 space-y-4" data-testid="incident-detail">
      <div className="space-y-1">
        <h3 className="font-semibold">
          {incident.title} · {formatTimestamp(incident.capturedAt)}
        </h3>
        <p className="text-muted-foreground text-sm">{incident.explanation}</p>
      </div>
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t('locks.sessions.database')}>{incident.database || NONE}</Field>
        <Field label={t('locks.incidents.table')}>{incident.object || NONE}</Field>
        <Field label={t('locks.incidents.lockMode')}>{incident.lockMode || NONE}</Field>
        <Field label={t('locks.incidents.longestWait')}>{incident.longestWait || NONE}</Field>
      </dl>
      {incident.sequence.length > 0 ? (
        <div className="space-y-1">
          <h4 className="text-sm font-medium">{t('locks.incidents.sequence')}</h4>
          <ol className="list-decimal space-y-0.5 pl-5 text-sm">
            {incident.sequence.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ol>
        </div>
      ) : null}
      <div className="space-y-2">
        <h4 className="text-sm font-medium">
          {t('locks.incidents.sessionsCount', { count: incident.sessions.length })}
        </h4>
        <ul className="space-y-2">
          {incident.sessions.map((session) => (
            <SessionCard key={session.pid} session={session} />
          ))}
        </ul>
        {incident.omitted > 0 ? (
          <p className="text-muted-foreground text-sm">
            {t('locks.incidents.omitted', { count: incident.omitted })}
          </p>
        ) : null}
        {incident.suppressed > 0 ? (
          <p className="text-muted-foreground text-sm">
            {t('locks.incidents.suppressed', { count: incident.suppressed })}
          </p>
        ) : null}
      </div>
    </div>
  )
}

function IncidentList({
  incidents,
  selected,
  onSelect,
}: {
  incidents: CapturedIncident[]
  selected: string
  onSelect: (key: string) => void
}) {
  const t = useT()
  return (
    <ul aria-label={t('locks.incidents.title')} className="space-y-1">
      {incidents.map((incident) => (
        <li key={incident.key}>
          <button
            aria-current={incident.key === selected ? 'true' : undefined}
            className="hover:bg-muted aria-[current=true]:bg-muted w-full rounded-md border px-3 py-2 text-left text-sm"
            onClick={() => onSelect(incident.key)}
            type="button"
          >
            <span className="block font-medium">{incident.title}</span>
            <span className="text-muted-foreground block text-xs">
              {formatTimestamp(incident.capturedAt)}
              {incident.database ? ` · ${incident.database}` : ''}
            </span>
            {incident.object ? (
              <span className="block truncate text-xs">{incident.object}</span>
            ) : null}
          </button>
        </li>
      ))}
    </ul>
  )
}

/**
 * The watcher's captures for one instance over the last seven days: the list,
 * newest first, and the full detail of the one selected (by default the newest).
 */
export function CapturedIncidents({ instanceId }: { instanceId: string }) {
  const t = useT()
  const query = useEvents(
    { instance_id: instanceId, type: INCIDENT_EVENT_TYPES.join(','), limit: LIMIT },
    WINDOW,
  )
  const incidents = useMemo(() => parseIncidents(query.data ?? []), [query.data])
  const [selectedKey, setSelectedKey] = useState('')
  const selected = incidents.find((i) => i.key === selectedKey) ?? incidents[0]

  let body: ReactNode
  if (query.isPending && query.data === undefined) {
    body = <p role="status">{t('locks.incidents.loading')}</p>
  } else if (query.isError && query.data === undefined) {
    body = (
      <ErrorState failure={query.error} onRetry={() => void query.refetch()} endpoint="events" />
    )
  } else if (!selected) {
    body = (
      <EmptyState
        title={t('locks.incidents.emptyTitle')}
        description={t('locks.incidents.emptyDescription')}
      />
    )
  } else {
    body = (
      <div className="grid gap-4 lg:grid-cols-[18rem_minmax(0,1fr)]">
        <IncidentList incidents={incidents} onSelect={setSelectedKey} selected={selected.key} />
        <IncidentDetail incident={selected} />
      </div>
    )
  }

  return (
    <Section captionId="locks.captured-incidents" title={t('locks.incidents.title')}>
      {body}
    </Section>
  )
}
