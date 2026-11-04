import type { Schemas } from '@/api/types'

export type ReadinessReport = Schemas['ReadinessReport']
export type ReadinessItem = Schemas['ReadinessItem']
export type ReadinessFix = Schemas['ReadinessFix']
type SummaryInstance = Schemas['ReadinessSummaryInstance']

/** The level a report shows: full only when there is no warning (I-14). */
export function readinessLevel(report: Pick<ReadinessReport, 'counts'>): 'full' | 'partial' {
  return report.counts.warning > 0 ? 'partial' : 'full'
}

/** Warnings sort before infos. */
export function severityRank(severity: ReadinessItem['severity']): number {
  return severity === 'warning' ? 0 : 1
}

const CHANNEL_ORDER: readonly ReadinessFix['channel'][] = [
  'console',
  'agent_config',
  'sql',
  'postgres_config',
  'host',
]

export const CHANNEL_LABEL: Record<ReadinessFix['channel'], string> = {
  console: 'In the console',
  agent_config: 'Agent configuration',
  sql: 'SQL',
  postgres_config: 'PostgreSQL configuration',
  host: 'On the host',
}

/** The fixes grouped by channel, in the fixed channel order; empty channels are left out. */
export function groupByChannel(
  fixes: readonly ReadinessFix[],
): { channel: ReadinessFix['channel']; fixes: ReadinessFix[] }[] {
  return CHANNEL_ORDER.map((channel) => ({
    channel,
    fixes: fixes.filter((fix) => fix.channel === channel),
  })).filter((group) => group.fixes.length > 0)
}

/** The badge text of a level: "Full" or "Partial · N warning(s)". */
export function readinessBadgeText(level: 'full' | 'partial', warnings: number): string {
  if (level === 'full') return 'Full'
  return `Partial · ${warnings} warning${warnings === 1 ? '' : 's'}`
}

/** The accessible name of the badge. */
export function readinessBadgeLabel(
  level: 'full' | 'partial',
  warnings: number,
  infos: number,
): string {
  if (level === 'full') return 'Monitoring: full'
  const notes = infos > 0 ? `, ${infos} note${infos === 1 ? '' : 's'}` : ''
  return `Monitoring: partial, ${warnings} warning${warnings === 1 ? '' : 's'}${notes}`
}

/**
 * The readiness of a cluster from its instances' summary rows: partial when
 * any is, the warnings and notes added up. Null when no instance has a row
 * (the summary has not answered for it), so no badge is shown rather than a
 * "Full" nobody checked.
 */
export function clusterReadiness(
  instanceIds: readonly string[],
  summary: readonly SummaryInstance[] | undefined,
): { level: 'full' | 'partial'; warnings: number; infos: number } | null {
  if (summary === undefined) return null
  const rows = summary.filter((row) => instanceIds.includes(row.instance_id))
  if (rows.length === 0) return null
  const warnings = rows.reduce((sum, row) => sum + row.warnings, 0)
  const infos = rows.reduce((sum, row) => sum + row.infos, 0)
  return { level: warnings > 0 ? 'partial' : 'full', warnings, infos }
}
