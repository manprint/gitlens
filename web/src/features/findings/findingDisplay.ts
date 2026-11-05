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

/** A finding scope in words: a relation is a table, index or another database object. */
export function scopeLabel(scope: string): string {
  return scope === 'relation' ? 'database object' : scope
}

/**
 * The object, database, instance and cluster a finding is about, most specific
 * first. With a name lookup an instance or cluster shows its name, not its id.
 */
export function findingTarget(finding: JoinedFinding, names?: NameLookup): string[] {
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
    finding.object_name,
    finding.datname ? `database ${finding.datname}` : undefined,
    instance ? `instance ${instance}` : undefined,
    cluster ? `cluster ${cluster}` : undefined,
  ].filter((value): value is string => Boolean(value))
}
