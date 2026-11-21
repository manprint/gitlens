import type { Schemas } from '@/api/types'

export type AutovacuumRun = Schemas['AutovacuumRun']
export type AutovacuumTableSummary = Schemas['AutovacuumTableSummary']

/** What the history needs: the agent reads the log, PostgreSQL logs the runs (names stay English). */
export const AUTOVACUUM_SETTINGS = ['checks.logs.enabled', 'log_autovacuum_min_duration'] as const

export type AutovacuumBadge = 'aggressive' | 'wraparound'

/** The badges of a run, "to prevent wraparound" first. */
export function runBadges(
  run: Pick<AutovacuumRun, 'aggressive' | 'wraparound'>,
): AutovacuumBadge[] {
  const out: AutovacuumBadge[] = []
  if (run.wraparound) out.push('wraparound')
  if (run.aggressive) out.push('aggressive')
  return out
}

export function summaryKey(
  row: Pick<AutovacuumTableSummary, 'datname' | 'schemaname' | 'relname'>,
): string {
  return `${row.datname}\u0000${row.schemaname}\u0000${row.relname}`
}

export function runKey(
  run: Pick<AutovacuumRun, 'ts' | 'kind' | 'datname' | 'schemaname' | 'relname'>,
): string {
  return `${summaryKey(run)}\u0000${run.kind}\u0000${run.ts}`
}

/** The filter of the runs of one summary row. */
export function runsQuery(
  instanceId: string,
  row: Pick<AutovacuumTableSummary, 'datname' | 'schemaname' | 'relname'>,
) {
  return {
    instance_id: instanceId,
    database: row.datname,
    schema: row.schemaname,
    relation: row.relname,
  }
}
