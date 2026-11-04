import type { ReactNode } from 'react'

import type { PermTier } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
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
  const query = sessionQueryText(session)
  const blockedBy = (session.blocked_by ?? []).map(String)
  const backendStart =
    typeof session.backend_start === 'string' ? formatTimestamp(session.backend_start, 'UTC') : NONE
  return (
    <div className="space-y-3" data-testid="session-detail">
      <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="PID">{sessionPid(session)}</Field>
        <Field label="Origin">
          <OriginBadge session={session} />
        </Field>
        <Field label="State">{sessionState(session) ?? NONE}</Field>
        <Field label="Wait event type">{textOr(session.wait_event_type)}</Field>
        <Field label="Wait event">{textOr(session.wait_event)}</Field>
        <Field label="Transaction age">{formatAge(transactionAge(session))}</Field>
        <Field label="Query age">{formatAge(queryAge(session))}</Field>
        <Field label="State age">{formatAge(stateAge(session))}</Field>
        <Field label="Backend start">{backendStart}</Field>
        <Field label="Client address">{sessionClient(session) ?? NONE}</Field>
        <Field label="Application">{sessionApplication(session) ?? NONE}</Field>
        <Field label="User">{sessionUser(session) ?? NONE}</Field>
        <Field label="Database">{sessionDatabase(session) ?? NONE}</Field>
        <Field label="Backend XID age">{countText(session.backend_xid_age)}</Field>
        <Field label="Backend xmin age">{countText(session.backend_xmin_age)}</Field>
        <Field label="Query ID">{sessionQueryId(session) ?? NONE}</Field>
        <Field label="Leader PID">
          {typeof session.leader_pid === 'number' || typeof session.leader_pid === 'string'
            ? String(session.leader_pid)
            : NONE}
        </Field>
        <Field label="Blocked by">{listText(blockedBy)}</Field>
        <Field label="Blocking">{listText(blockedByThis(sessions, session.pid))}</Field>
      </dl>
      <div>
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground text-xs">
            {query === null
              ? 'Query text'
              : query.source === 'raw'
                ? 'Query text (raw)'
                : 'Query text (normalised)'}
          </span>
          {query === null ? null : <CopyButton text={query.text} />}
        </div>
        {query === null ? (
          <p className="text-muted-foreground text-sm">
            No text is known for this session: the agent sends the raw text only when
            checks.locks.session_query_text is on, and the normalised text needs a query ID that
            pg_stat_statements has recorded.
          </p>
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
