import type { ReactNode } from 'react'

import type { PermTier } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { useT } from '@/i18n/react'
import { formatCount, formatTimestamp } from '@/lib/format'
import {
  blockedByThis,
  formatAge,
  queryAge,
  sessionApplication,
  sessionClient,
  sessionDatabase,
  sessionPid,
  sessionQueryId,
  sessionQueryText,
  sessionState,
  sessionUser,
  stateAge,
  transactionAge,
  type Session,
} from '@/lib/sessions'
import { OriginBadge } from './OriginBadge'
import { SignalActions } from './SignalActions'

const NONE = '—'

function countText(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? formatCount(value) : NONE
}

function textOr(value: unknown): string {
  return typeof value === 'string' && value.trim() !== '' ? value : NONE
}

function listText(pids: readonly string[]): string {
  return pids.length === 0 ? NONE : pids.join(', ')
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground text-xs">{label}</dt>
      <dd className="text-sm break-words">{children}</dd>
    </div>
  )
}

/**
 * Every field the lock sample holds for one session, its relations to other
 * sessions, the statement text with its source, and the signal actions. Opened
 * by the operator from the session's row.
 */
export function SessionDetail({
  session,
  sessions,
  currentTier,
  instanceId,
  sampleStale,
}: {
  session: Session
  /** Every session of the sample, for the sessions this one blocks. */
  sessions: readonly Session[]
  currentTier: PermTier
  instanceId: string | undefined
  sampleStale: boolean
}) {
  const t = useT()
  const query = sessionQueryText(session)
  const blockedBy = (session.blocked_by ?? []).map(String)
  const backendStart =
    typeof session.backend_start === 'string' ? formatTimestamp(session.backend_start) : NONE
  return (
    <div className="space-y-3" data-testid="session-detail">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t('locks.sessions.pid')}>{sessionPid(session)}</Field>
        <Field label={t('locks.detail.origin')}>
          <OriginBadge session={session} />
        </Field>
        <Field label={t('locks.sessions.state')}>{sessionState(session) ?? NONE}</Field>
        <Field label={t('locks.detail.waitEventType')}>{textOr(session.wait_event_type)}</Field>
        <Field label={t('locks.detail.waitEvent')}>{textOr(session.wait_event)}</Field>
        <Field label={t('locks.sessions.transactionAge')}>
          {formatAge(transactionAge(session))}
        </Field>
        <Field label={t('locks.sessions.queryAge')}>{formatAge(queryAge(session))}</Field>
        <Field label={t('locks.detail.stateAge')}>{formatAge(stateAge(session))}</Field>
        <Field label={t('locks.detail.backendStart')}>{backendStart}</Field>
        <Field label={t('locks.detail.clientAddress')}>{sessionClient(session) ?? NONE}</Field>
        <Field label={t('locks.sessions.application')}>{sessionApplication(session) ?? NONE}</Field>
        <Field label={t('locks.sessions.user')}>{sessionUser(session) ?? NONE}</Field>
        <Field label={t('locks.sessions.database')}>{sessionDatabase(session) ?? NONE}</Field>
        <Field label={t('locks.detail.backendXidAge')}>{countText(session.backend_xid_age)}</Field>
        <Field label={t('locks.detail.backendXminAge')}>
          {countText(session.backend_xmin_age)}
        </Field>
        <Field label={t('locks.detail.queryId')}>{sessionQueryId(session) ?? NONE}</Field>
        <Field label={t('locks.detail.leaderPid')}>
          {typeof session.leader_pid === 'number' || typeof session.leader_pid === 'string'
            ? String(session.leader_pid)
            : NONE}
        </Field>
        <Field label={t('locks.detail.blockedBy')}>{listText(blockedBy)}</Field>
        <Field label={t('locks.detail.blocking')}>
          {listText(blockedByThis(sessions, session.pid))}
        </Field>
      </dl>
      <div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground text-xs">
            {query === null
              ? t('locks.detail.queryText')
              : query.source === 'raw'
                ? t('locks.detail.queryTextRaw')
                : t('locks.detail.queryTextNormalised')}
          </span>
          {query === null ? null : <CopyButton text={query.text} />}
        </div>
        {query === null ? (
          <p className="text-muted-foreground text-sm">{t('locks.detail.noQueryText')}</p>
        ) : (
          <pre className="bg-muted/50 max-h-40 overflow-auto rounded-md border p-2 font-mono text-xs break-words whitespace-pre-wrap">
            {query.text}
          </pre>
        )}
      </div>
      {instanceId ? (
        <SignalActions
          currentTier={currentTier}
          instanceId={instanceId}
          node={{ ...session, query: query?.text }}
          sampleStale={sampleStale}
        />
      ) : null}
    </div>
  )
}
