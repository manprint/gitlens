import { translate } from '@/i18n/store'

import { describeEvent, type ClusterEvent } from './events'
import { formatCount, formatDuration, formatTimestamp } from './format'

/**
 * Captures of the agent watcher (internal/watch): one event per incident, its
 * payload a watch.Incident. The Locks page reads them for its Captured
 * incidents view; the notification names the same table and statements.
 */
export const INCIDENT_EVENT_TYPES = [
  'deadlock_captured',
  'lock_wait_captured',
  'idle_in_transaction_captured',
  'long_transaction_captured',
  'long_query_captured',
] as const

export interface IncidentSession {
  pid: number
  user: string
  application: string
  client: string
  state: string
  /** "transaction open for 4s · statement running for 1.5s", empty when unknown. */
  timing: string
  /** "ShareLock on transaction 900 (1.2s)", empty when not waiting. */
  waits: string
  blockedBy: number[]
  /** "(3,7) of public.orders", empty when it holds no row lock. */
  holdsRow: string
  holdsTables: string[]
  xminAge: string
  query: string
}

export interface CapturedIncident {
  key: string
  type: string
  title: string
  capturedAt: string
  database: string
  /** "public.orders, row ctid (3,7)", "public.orders" or empty. */
  object: string
  lockMode: string
  longestWait: string
  /** What happened, in one sentence, for the type and how it was captured. */
  explanation: string
  sessions: IncidentSession[]
  /** One line per waiting session: who waits for what, held by whom. */
  sequence: string[]
  omitted: number
  suppressed: number
}

type Json = Record<string, unknown>

function obj(value: unknown): Json | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Json)
    : null
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function pids(value: unknown): number[] {
  return Array.isArray(value)
    ? value.filter((pid): pid is number => typeof pid === 'number' && Number.isInteger(pid))
    : []
}

/** The "(page,tuple)" of a lock reference, empty when it names no row. */
export function lockCtid(ref: Json | null): string {
  if (!ref) return ''
  const page = ref.page
  const tuple = ref.tuple
  return typeof page === 'number' && typeof tuple === 'number' ? `(${page},${tuple})` : ''
}

function lockTarget(ref: Json): string {
  const relation = str(ref.relation)
  const ctid = lockCtid(ref)
  const locktype = str(ref.locktype)
  if (locktype === 'transactionid' && relation === '') {
    const xid = str(ref.transactionid)
    return xid
      ? translate('locks.incident.transaction', { xid })
      : translate('locks.incident.aTransaction')
  }
  if (relation) return ctid ? `${relation} ${ctid}` : relation
  return locktype || translate('locks.incident.aLock')
}

function sessionTiming(s: Json): string {
  const parts: string[] = []
  const xact = num(s.xact_seconds)
  const state = str(s.state)
  if (xact > 0) parts.push(translate('locks.incident.openFor', { duration: formatDuration(xact) }))
  if (state.startsWith('idle in transaction') && num(s.state_seconds) > 0) {
    parts.push(
      translate('locks.incident.idleFor', { duration: formatDuration(num(s.state_seconds)) }),
    )
  } else if (state === 'active' && num(s.query_seconds) >= 1) {
    parts.push(
      translate('locks.incident.runningFor', { duration: formatDuration(num(s.query_seconds)) }),
    )
  }
  return parts.join(' · ')
}

function parseSession(value: unknown): IncidentSession | null {
  const s = obj(value)
  if (!s || typeof s.pid !== 'number') return null
  const wait = obj(s.wait)
  const row = obj(s.row)
  const rowCtid = lockCtid(row)
  const rowRelation = row ? str(row.relation) : ''
  let waits = ''
  if (wait) {
    waits = translate('locks.incident.waitsOn', {
      mode: str(wait.mode) || translate('locks.incident.aLock'),
      target: lockTarget(wait),
    })
    if (num(wait.wait_seconds) > 0) waits += ` (${formatDuration(num(wait.wait_seconds))})`
  }
  const held = Array.isArray(s.held_relations)
    ? s.held_relations.filter((r): r is string => typeof r === 'string' && r !== '')
    : []
  return {
    pid: s.pid,
    user: str(s.usename),
    application: str(s.application_name),
    client: str(s.client_addr),
    state: str(s.state),
    timing: sessionTiming(s),
    waits,
    blockedBy: pids(s.blocked_by),
    holdsRow:
      rowCtid && rowRelation
        ? translate('locks.incident.rowOf', { ctid: rowCtid, relation: rowRelation })
        : '',
    holdsTables: held,
    xminAge: num(s.xmin_age) > 0 ? formatCount(num(s.xmin_age)) : '',
    query: str(s.query),
  }
}

function explanation(type: string, p: Json): string {
  switch (type) {
    case 'deadlock': {
      if (p.rebuilt !== true) {
        return translate('locks.incident.explain.deadlock')
      }
      const n = Math.max(1, num(p.deadlocks))
      const since = str(p.since)
      return since
        ? translate('locks.incident.explain.rebuiltSince', { n, since: formatTimestamp(since) })
        : translate('locks.incident.explain.rebuilt', { n })
    }
    case 'lock_wait':
      return translate('locks.incident.explain.lockWait')
    case 'idle_in_transaction':
      return translate('locks.incident.explain.idleInTransaction')
    case 'long_transaction':
      return translate('locks.incident.explain.longTransaction')
    case 'long_query':
      return translate('locks.incident.explain.longQuery')
    default:
      return ''
  }
}

function sequence(sessions: IncidentSession[]): string[] {
  return sessions
    .filter((s) => s.waits !== '')
    .map((s) => {
      return s.blockedBy.length > 0
        ? translate('locks.incident.waitsBlocked', {
            pid: s.pid,
            waits: s.waits,
            blockers: s.blockedBy.join(', '),
          })
        : translate('locks.incident.waits', { pid: s.pid, waits: s.waits })
    })
}

/** The incident an event carries, or null when its payload is not one. */
export function parseIncident(event: ClusterEvent): CapturedIncident | null {
  const p = obj(event.payload)
  if (!p) return null
  const type = str(p.type)
  if (!type) return null
  const sessions = Array.isArray(p.sessions)
    ? p.sessions.map(parseSession).filter((s): s is IncidentSession => s !== null)
    : []
  const relation = str(p.relation)
  const ctid = str(p.ctid)
  const wait = num(p.wait_seconds)
  return {
    key: `${event.event_id}`,
    type,
    title: describeEvent(event.type).title,
    capturedAt: str(p.captured_at) || event.ts,
    database: str(p.database),
    object: relation
      ? ctid
        ? translate('locks.incident.objectRow', { relation, ctid })
        : relation
      : '',
    lockMode: str(p.lock_mode),
    longestWait: wait > 0 ? formatDuration(wait) : '',
    explanation: explanation(type, p),
    sessions,
    sequence: sequence(sessions),
    omitted: num(p.sessions_omitted),
    suppressed: num(p.suppressed_before),
  }
}

/** The incidents of a list of events, newest first, skipping other payloads. */
export function parseIncidents(events: readonly ClusterEvent[]): CapturedIncident[] {
  return events
    .map(parseIncident)
    .filter((i): i is CapturedIncident => i !== null)
    .sort((a, b) => Date.parse(b.capturedAt) - Date.parse(a.capturedAt))
}
