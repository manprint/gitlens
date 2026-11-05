import { query } from './pg'

/**
 * Ground truth for the session-origin proof (16.4): the client backends of
 * `pg_stat_activity` read through the QA harness, and the pure comparison with
 * what the API sample and the console tables show. Everything compared exactly
 * is named in `EXACT_APPS`; the totals carry an explicit, measured tolerance.
 */
export const MONITOR_ROLE = 'pglens_monitor'
/** The stimulus sessions whose rows must match the sample exactly. */
export const EXACT_APPS = ['qa-app-a', 'qa-app-b', 'qa-idle-tx', 'qa-waiter'] as const

export type Origin = 'application' | 'pglens'

export interface TruthSession {
  pid: number
  usename: string | null
  application_name: string
  state: string | null
  datname: string | null
  query_id: string | null
  wait_event: string | null
  /** pg_blocking_pids() of the backend, as pids. */
  blocked_by: number[]
}

export interface ApiSession {
  pid: number | string
  origin?: string
  application_name?: string
  usename?: string
  state?: string
  query_id?: string
  query_age?: number
  wait_event?: string
  query_text?: string
  query_text_source?: string
  blocked_by?: (number | string)[]
}

export interface ConnectionCount {
  key: string
  count: number
}

export interface ApiLocks {
  sampled_at: string | null
  sessions?: ApiSession[]
  connections?: {
    by_application_enabled: boolean
    truncated: boolean
    by_origin: ConnectionCount[]
    by_application: ConnectionCount[]
    totals?: { by_origin: ConnectionCount[] }
  }
}

/** The classifier of D27: `pglens`, `pglens/…`, `pglens-agent/…`, or the monitoring role. */
export function originOf(
  applicationName: string,
  user: string | null,
  role = MONITOR_ROLE,
): Origin {
  const app = applicationName
  if (app === 'pglens' || app.startsWith('pglens/') || app.startsWith('pglens-agent/')) {
    return 'pglens'
  }
  return role !== '' && user === role ? 'pglens' : 'application'
}

/** Client backends of the monitored database cluster, minus the reading backend. */
export async function truthSessions(): Promise<TruthSession[]> {
  const out = await query(
    `SELECT coalesce(json_agg(row_to_json(t) ORDER BY t.pid), '[]'::json) FROM (
       SELECT pid, usename::text AS usename, application_name, state, datname::text AS datname,
              query_id::text AS query_id, wait_event,
              pg_blocking_pids(pid) AS blocked_by
         FROM pg_stat_activity
        WHERE backend_type = 'client backend' AND pid <> pg_backend_pid()
     ) t;`,
  )
  const rows = JSON.parse(out) as TruthSession[]
  return rows.map((row) => ({
    ...row,
    query_id: row.query_id === '0' ? null : row.query_id,
  }))
}

export function truthTotals(truth: readonly TruthSession[]): Record<Origin, number> {
  const totals: Record<Origin, number> = { application: 0, pglens: 0 }
  for (const row of truth) totals[originOf(row.application_name, row.usename)] += 1
  return totals
}

/** The non-QA application sessions: the Odoo workers, whose number drifts. */
export function odooCount(truth: readonly TruthSession[]): number {
  return truth.filter(
    (row) =>
      originOf(row.application_name, row.usename) === 'application' &&
      !row.application_name.startsWith('qa-'),
  ).length
}

const sorted = (values: readonly number[]) => [...values].sort((a, b) => a - b)

/**
 * Mismatches between the ground truth and one API sample, for the sessions the
 * test owns (exact) and for the page's own consistency (I-15). Empty means they agree.
 */
export function compare(truth: readonly TruthSession[], api: ApiLocks): string[] {
  const problems: string[] = []
  const sample = api.sessions ?? []
  for (const app of EXACT_APPS) {
    const expected = sorted(
      truth.filter((row) => row.application_name === app).map((row) => row.pid),
    )
    const seen = sorted(
      sample.filter((row) => row.application_name === app).map((row) => Number(row.pid)),
    )
    if (expected.join(',') !== seen.join(',')) {
      problems.push(
        `${app}: pg_stat_activity pids [${expected.join(',')}] but the sample has [${seen.join(',')}]`,
      )
    }
  }
  for (const row of truth) {
    if (!(EXACT_APPS as readonly string[]).includes(row.application_name)) continue
    const shown = sample.find((entry) => Number(entry.pid) === row.pid)
    if (shown === undefined) continue
    const want = originOf(row.application_name, row.usename)
    if (shown.origin !== want) {
      problems.push(
        `pid ${row.pid} (${row.application_name}): origin ${shown.origin}, want ${want}`,
      )
    }
  }
  const holder = truth.find((row) => row.application_name === 'qa-idle-tx')
  const waiter = truth.find((row) => row.application_name === 'qa-waiter')
  if (holder !== undefined && waiter !== undefined) {
    const blocked = sample.find((entry) => Number(entry.pid) === waiter.pid)
    const names = (blocked?.blocked_by ?? []).map(Number)
    if (!names.includes(holder.pid)) {
      problems.push(
        `qa-waiter ${waiter.pid} is not blocked by qa-idle-tx ${holder.pid} in the sample`,
      )
    }
  }
  const connections = api.connections
  if (connections !== undefined && !connections.truncated) {
    for (const origin of ['application', 'pglens'] as const) {
      const tables = sample.filter((row) => (row.origin ?? 'application') === origin).length
      const summary = connections.by_origin.find((entry) => entry.key === origin)?.count ?? 0
      if (tables !== summary) {
        problems.push(`${origin}: sessions in the sample ${tables}, summary says ${summary}`)
      }
    }
  }
  return problems
}

/** The ground truth with every `from` session renamed to `to`: the sabotaged map. */
export function renamed(truth: readonly TruthSession[], from: string, to: string): TruthSession[] {
  return truth.map((row) =>
    row.application_name === from ? { ...row, application_name: to } : row,
  )
}

/**
 * Totals of the sample against the ground truth, as mismatches. The sampling
 * backend never counts itself (`pid <> pg_backend_pid()` in the locks query), so
 * the pglens side may be one lower than the truth; the Odoo workers change
 * between reads, so the application side may differ by `drift`, the change
 * measured between the truth reads taken before and after the API read.
 */
export function compareTotals(
  before: readonly TruthSession[],
  after: readonly TruthSession[],
  api: ApiLocks,
): string[] {
  const connections = api.connections
  if (connections === undefined) return ['the response carries no connections summary']
  const by = connections.truncated ? connections.totals?.by_origin : connections.by_origin
  const seen = (key: Origin) => by?.find((entry) => entry.key === key)?.count ?? 0
  const lo = truthTotals(before)
  const hi = truthTotals(after)
  const drift = Math.abs(odooCount(before) - odooCount(after))
  const problems: string[] = []
  const appLow = Math.min(lo.application, hi.application) - drift
  const appHigh = Math.max(lo.application, hi.application) + drift
  if (seen('application') < appLow || seen('application') > appHigh) {
    problems.push(
      `application total ${seen('application')} outside [${appLow}, ${appHigh}] (truth ${lo.application}/${hi.application}, drift ${drift})`,
    )
  }
  const pgLow = Math.min(lo.pglens, hi.pglens) - 1
  const pgHigh = Math.max(lo.pglens, hi.pglens)
  if (seen('pglens') < pgLow || seen('pglens') > pgHigh) {
    problems.push(
      `pglens total ${seen('pglens')} outside [${pgLow}, ${pgHigh}] (truth ${lo.pglens}/${hi.pglens})`,
    )
  }
  return problems
}
