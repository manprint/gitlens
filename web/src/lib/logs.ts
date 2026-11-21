/**
 * PostgreSQL log lines (plan 007 phase 6): the classes the agent ships, the
 * filters of the Logs page and what an expanded row shows.
 */

/** The classes the agent ships, as pglog.Classes() minus the unshipped ones (guard in internal/pglog). */
export const LOG_CLASSES = [
  'auth.failed',
  'autovacuum.run',
  'checkpoint',
  'checkpoint.frequent',
  'connection.limit',
  'corruption',
  'disk.full',
  'error.statement',
  'fatal.other',
  'idle_tx.timeout',
  'lock.deadlock',
  'lock.timeout',
  'lock.wait',
  'memory.out',
  'panic',
  'replication.conflict',
  'serialization.failure',
  'server.crash',
  'server.recovery',
  'server.start',
  'slow.statement',
  'statement.canceled',
  'statement.timeout',
  'temp_file',
  'warning.other',
  'wraparound.warning',
] as const

/** The error_severity values the server accepts as a filter, most severe first. */
export const LOG_SEVERITIES = [
  'PANIC',
  'FATAL',
  'ERROR',
  'WARNING',
  'NOTICE',
  'INFO',
  'LOG',
  'DEBUG1',
  'DEBUG2',
  'DEBUG3',
  'DEBUG4',
  'DEBUG5',
] as const

export const LOGS_PAGE_LIMIT = 100

export interface LogFilters {
  class: string
  severity: string
  database: string
  q: string
  before: string
}

const FILTER_PARAMS = {
  class: 'class',
  severity: 'severity',
  database: 'db',
  q: 'q',
  before: 'before',
} as const satisfies Record<keyof LogFilters, string>

function known(value: string | null, list: readonly string[]): string {
  return value !== null && list.includes(value) ? value : ''
}

/** The filters in the page URL; an unknown class or severity is dropped. */
export function readLogFilters(params: URLSearchParams): LogFilters {
  return {
    class: known(params.get(FILTER_PARAMS.class), LOG_CLASSES),
    severity: known(params.get(FILTER_PARAMS.severity), LOG_SEVERITIES),
    database: params.get(FILTER_PARAMS.database)?.trim() ?? '',
    q: params.get(FILTER_PARAMS.q)?.trim() ?? '',
    before: params.get(FILTER_PARAMS.before) ?? '',
  }
}

/** The URL with one filter changed; any change but paging starts on the newest lines. */
export function withLogFilter(
  params: URLSearchParams,
  key: keyof LogFilters,
  value: string,
): URLSearchParams {
  const next = new URLSearchParams(params)
  const name = FILTER_PARAMS[key]
  if (value === '') next.delete(name)
  else next.set(name, value)
  if (key !== 'before') next.delete(FILTER_PARAMS.before)
  return next
}

export interface LogsRequest {
  instance_id: string
  limit: number
  class?: string
  severity?: string
  database?: string
  q?: string
  before?: string
}

/** The getLogs parameters for the filters; empty filters are left out. */
export function logsRequest(instanceId: string, filters: LogFilters): LogsRequest {
  const request: LogsRequest = { instance_id: instanceId, limit: LOGS_PAGE_LIMIT }
  if (filters.class) request.class = filters.class
  if (filters.severity) request.severity = filters.severity
  if (filters.database) request.database = filters.database
  if (filters.q) request.q = filters.q
  if (filters.before) request.before = filters.before
  return request
}

/** The detail fields of a line (detail, hint, context, statement…), sorted by name. */
export function logExtraEntries(
  extra: Readonly<Record<string, string>> | null | undefined,
): [string, string][] {
  if (!extra) return []
  return Object.entries(extra)
    .filter(([, value]) => value !== '')
    .sort(([a], [b]) => a.localeCompare(b, 'en'))
}

/** The query inspector of a statement, or null when the line names none. */
export function logQueryHref(
  instanceId: string,
  queryid: string | null | undefined,
): string | null {
  if (!queryid || queryid === '0') return null
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`
}

/** The badge variant of a severity. */
export function logSeverityVariant(severity: string): 'destructive' | 'warning' | 'outline' {
  switch (severity) {
    case 'PANIC':
    case 'FATAL':
    case 'ERROR':
      return 'destructive'
    case 'WARNING':
      return 'warning'
    default:
      return 'outline'
  }
}
