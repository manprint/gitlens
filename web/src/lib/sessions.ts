import { formatDuration } from './format'
import { isWaiting, sessionKey, type LockNode, type LockPID } from './locks'
import type { Accessor, Comparable, FilterSpec, SearchField } from './tableControls'

/** A client session of the lock sample, as the table rows hold it. */
export type Session = LockNode
export type SessionOrigin = 'application' | 'pglens'

export interface ConnectionCount {
  key: string
  count: number
}

export interface ConnectionGroups {
  by_origin: readonly ConnectionCount[]
  by_application: readonly ConnectionCount[]
  by_user: readonly ConnectionCount[]
  by_client: readonly ConnectionCount[]
  by_database: readonly ConnectionCount[]
  by_state: readonly ConnectionCount[]
}

export interface ConnectionsSummary extends ConnectionGroups {
  by_application_enabled: boolean
  truncated: boolean
  totals?: ConnectionGroups
}

export type ConnectionGroupId = keyof ConnectionGroups

export const SESSION_ORIGINS: readonly SessionOrigin[] = ['application', 'pglens']

function text(value: unknown): string | null {
  return typeof value === 'string' && value.trim() !== '' ? value : null
}

function finite(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/** The origin the server set; null for a session of a server older than 16.2. */
export function sessionOrigin(session: Session): SessionOrigin | null {
  return session.origin === 'application' || session.origin === 'pglens' ? session.origin : null
}

export function hasOrigin(sessions: readonly Session[]): boolean {
  return sessions.some((session) => sessionOrigin(session) !== null)
}

/**
 * The one fallback rule of the page and of the activity section: the origin
 * tables show when any session carries an origin, or when the sample holds no
 * session but the response carries a connection summary; a list of sessions
 * without origin (an older server) keeps the previous markup.
 */
export function usesOriginTables(
  sessions: readonly Session[],
  connections: ConnectionsSummary | undefined,
): boolean {
  return hasOrigin(sessions) || (sessions.length === 0 && connections !== undefined)
}

/** A session without origin inside an origin response is counted with the application. */
export function splitByOrigin(sessions: readonly Session[]): Record<SessionOrigin, Session[]> {
  const split: Record<SessionOrigin, Session[]> = { application: [], pglens: [] }
  for (const session of sessions) split[sessionOrigin(session) ?? 'application'].push(session)
  return split
}

export function sessionPid(session: Session): string {
  return String(session.pid)
}

export function sessionUser(session: Session): string | null {
  return text(session.usename) ?? text(session.username) ?? text(session.user)
}

export function sessionApplication(session: Session): string | null {
  return text(session.application_name) ?? text(session.application)
}

export function sessionDatabase(session: Session): string | null {
  return text(session.datname) ?? text(session.database)
}

export function sessionClient(session: Session): string | null {
  return text(session.client_addr)
}

export function sessionState(session: Session): string | null {
  return text(session.state)
}

/** The wait the session reports: `type: event`, or the type alone. */
export function sessionWait(session: Session): string | null {
  const type = text(session.wait_event_type)
  const event = text(session.wait_event)
  if (type !== null && event !== null) return `${type}: ${event}`
  return type ?? event
}

export function transactionAge(session: Session): number | null {
  return finite(session.xact_age)
}

export function queryAge(session: Session): number | null {
  return finite(session.query_age)
}

export function stateAge(session: Session): number | null {
  return finite(session.state_age)
}

export function sessionQueryId(session: Session): string | null {
  return text(session.query_id)
}

export interface SessionQueryText {
  text: string
  source: 'normalised' | 'raw'
}

/** The text the server resolved: normalised through the query id, or the raw text the agent sent. */
export function sessionQueryText(session: Session): SessionQueryText | null {
  const value = text(session.query_text)
  if (value === null) return null
  return { text: value, source: session.query_text_source === 'raw' ? 'raw' : 'normalised' }
}

/** The first line of a text, trimmed: what one table row shows. */
export function firstLine(value: string | null): string | null {
  if (value === null) return null
  const line = value.split(/\r?\n/u, 1)[0]?.trim()
  return line === undefined || line === '' ? null : line
}

/** A duration in seconds as text; a missing age is a dash. */
export function formatAge(seconds: number | null): string {
  return formatDuration(seconds)
}

export function isIdleInTransaction(session: Session): boolean {
  return sessionState(session)?.startsWith('idle in transaction') ?? false
}

/** PIDs of the sessions that wait on this one. */
export function blockedByThis(sessions: readonly Session[], pid: LockPID): string[] {
  return sessions
    .filter((candidate) =>
      (candidate.blocked_by ?? []).some((blocker) => String(blocker) === String(pid)),
    )
    .map(sessionPid)
}

export function rowKey(session: Session): string {
  return sessionKey(session)
}

export const SESSION_SORT = {
  pid: (session): Comparable => finite(session.pid) ?? sessionPid(session),
  user: sessionUser,
  application: sessionApplication,
  database: sessionDatabase,
  client: sessionClient,
  state: sessionState,
  wait: sessionWait,
  xact_age: transactionAge,
  query_age: queryAge,
  query: (session) => sessionQueryText(session)?.text ?? null,
} satisfies Record<string, Accessor<Session>>

export const SESSION_SEARCH_FIELDS: readonly SearchField<Session>[] = [
  sessionPid,
  sessionUser,
  sessionApplication,
  sessionClient,
  sessionDatabase,
  (session) => sessionQueryText(session)?.text ?? null,
]

/** Filter specs of the toolbar; the two "only" switches keep the rows that match. */
export const SESSION_FILTERS: readonly FilterSpec<Session>[] = [
  { id: 'state', label: 'State', kind: 'select', accessor: sessionState },
  {
    id: 'wait',
    label: 'Wait event type',
    kind: 'select',
    accessor: (s) => text(s.wait_event_type),
  },
  { id: 'database', label: 'Database', kind: 'select', accessor: sessionDatabase },
  {
    id: 'waiting',
    label: 'Waiting',
    kind: 'select',
    options: [{ value: 'only', label: 'Waiting only' }],
    accessor: (session) => isWaiting(session),
    predicate: (session, value) => value !== 'only' || isWaiting(session),
  },
  {
    id: 'idle-tx',
    label: 'Idle in transaction',
    kind: 'select',
    options: [{ value: 'only', label: 'Idle in transaction only' }],
    accessor: isIdleInTransaction,
    predicate: (session, value) => value !== 'only' || isIdleInTransaction(session),
  },
]

/** The count of one origin in a group list; 0 when the list does not name it. */
export function countOf(rows: readonly ConnectionCount[], key: string): number {
  return rows.find((row) => row.key === key)?.count ?? 0
}

/**
 * The groups the page states totals from: the activity totals when the sample
 * was cut to its cap (they cover every client backend), else the sample itself.
 */
export function connectionGroup(
  connections: ConnectionsSummary,
  group: ConnectionGroupId,
): readonly ConnectionCount[] {
  return connections.truncated && connections.totals
    ? connections.totals[group]
    : connections[group]
}

/** `application 240 · pglens 4`: the split of the client connections by origin. */
export function originSentence(connections: ConnectionsSummary): string {
  const rows = connectionGroup(connections, 'by_origin')
  return `application ${countOf(rows, 'application')} · pglens ${countOf(rows, 'pglens')}`
}

export interface ShareRow extends ConnectionCount {
  /** Fraction of the group total, 0 to 1. */
  share: number
}

export function withShare(rows: readonly ConnectionCount[]): ShareRow[] {
  const total = rows.reduce((sum, row) => sum + row.count, 0)
  return rows.map((row) => ({ ...row, share: total === 0 ? 0 : row.count / total }))
}

/** The label of a key: the empty name of an application, user or database is stated. */
export function connectionKeyLabel(key: string): string {
  return key === '' ? '(empty name)' : key
}

export const CONNECTION_GROUPS: readonly { id: ConnectionGroupId; label: string; noun: string }[] =
  [
    { id: 'by_origin', label: 'By origin', noun: 'Origin' },
    { id: 'by_application', label: 'By application', noun: 'Application' },
    { id: 'by_user', label: 'By user', noun: 'User' },
    { id: 'by_client', label: 'By client', noun: 'Client' },
    { id: 'by_database', label: 'By database', noun: 'Database' },
    { id: 'by_state', label: 'By state', noun: 'State' },
  ]
