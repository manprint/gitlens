import type { Schemas } from '@/api/types'
import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'

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

export const CHANNEL_LABEL_KEY = {
  console: 'readiness.channel.console',
  agent_config: 'readiness.channel.agent_config',
  sql: 'readiness.channel.sql',
  postgres_config: 'readiness.channel.postgres_config',
  host: 'readiness.channel.host',
} as const satisfies Record<ReadinessFix['channel'], MessageKey>

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
  if (level === 'full') return translate('readiness.badge.full')
  return translate('readiness.badge.partial', { warnings })
}

/** The accessible name of the badge. */
export function readinessBadgeLabel(
  level: 'full' | 'partial',
  warnings: number,
  infos: number,
): string {
  if (level === 'full') return translate('readiness.badge.labelFull')
  return infos > 0
    ? translate('readiness.badge.labelPartialNotes', { warnings, notes: infos })
    : translate('readiness.badge.labelPartial', { warnings })
}

/** The instance tab that lists the readiness warnings and their fixes. */
export function readinessFixPath(instanceId: string): string {
  return `/instances/${encodeURIComponent(instanceId)}?section=readiness`
}

export interface ClusterReadiness {
  level: 'full' | 'partial'
  warnings: number
  infos: number
  /** The instance with the most warnings, where the badge leads; null when none has one. */
  fixInstanceId?: string | null
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
): ClusterReadiness | null {
  if (summary === undefined) return null
  const rows = summary.filter((row) => instanceIds.includes(row.instance_id))
  if (rows.length === 0) return null
  const warnings = rows.reduce((sum, row) => sum + row.warnings, 0)
  const infos = rows.reduce((sum, row) => sum + row.infos, 0)
  const worst = rows.reduce<SummaryInstance | null>(
    (best, row) => (row.warnings > (best?.warnings ?? 0) ? row : best),
    null,
  )
  return {
    level: warnings > 0 ? 'partial' : 'full',
    warnings,
    infos,
    fixInstanceId: worst?.instance_id ?? null,
  }
}
