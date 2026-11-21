import type { Translate } from '@/i18n/react'
import { translate } from '@/i18n/store'
import type { JoinedFinding } from '@/lib/findings'
import { clusterNameOrId, instanceNameOrId, type NameLookup } from '@/lib/names'

// The colour follows the severity name, so an unknown one stays neutral.
export function severityAccent(severity: string): string {
  if (severity === 'critical') return 'before:bg-destructive'
  if (severity === 'warning') return 'before:bg-warning'
  if (severity === 'info') return 'before:bg-info'
  return 'before:bg-border'
}

/** The severity chip colours, shared by the finding card and the findings table rows. */
export function severityBadge(severity: string): string {
  if (severity === 'critical') return 'border-destructive/30 bg-destructive/10 text-destructive'
  if (severity === 'warning') return 'border-warning/40 bg-warning/15 text-warning-foreground'
  if (severity === 'info') return 'border-info/30 bg-info/10 text-info-foreground'
  return 'bg-muted text-muted-foreground'
}

/**
 * The database, instance and cluster a finding is in, most specific first. With
 * a name lookup an instance or cluster shows its name, not its id; the object
 * itself comes from `findingSubject`.
 */
export function findingPlace(finding: JoinedFinding, names?: NameLookup): string[] {
  const instance = finding.instance_id
    ? names
      ? instanceNameOrId(names, String(finding.instance_id))
      : String(finding.instance_id)
    : undefined
  const cluster = finding.cluster_id
    ? names
      ? clusterNameOrId(names, String(finding.cluster_id))
      : String(finding.cluster_id)
    : undefined
  return [
    finding.datname ? translate('findings.place.database', { name: finding.datname }) : undefined,
    instance ? translate('findings.place.instance', { name: instance }) : undefined,
    cluster ? translate('findings.place.cluster', { name: cluster }) : undefined,
  ].filter((value): value is string => Boolean(value))
}

// Server enum values shown as their translated name; unknown values stay raw.
export function severityText(t: Translate, severity: string): string {
  if (severity === 'critical') return t('findings.severity.critical')
  if (severity === 'warning') return t('findings.severity.warning')
  if (severity === 'info') return t('findings.severity.info')
  return severity
}

export function stateText(t: Translate, state: string): string {
  if (state === 'open') return t('findings.state.open')
  if (state === 'degraded') return t('findings.state.degraded')
  if (state === 'muted') return t('findings.state.muted')
  if (state === 'resolved') return t('findings.state.resolved')
  return state
}

export function scopeText(t: Translate, scope: string): string {
  if (scope === 'relation') return t('findings.scope.relation')
  if (scope === 'instance') return t('findings.scope.instance')
  if (scope === 'cluster') return t('findings.scope.cluster')
  return scope
}
