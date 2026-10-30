import type { JoinedFinding } from '@/lib/findings'

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

/** The object, database, instance and cluster a finding is about, most specific first. */
export function findingTarget(finding: JoinedFinding): string[] {
  return [
    finding.object_name,
    finding.datname ? `database ${finding.datname}` : undefined,
    finding.instance_id ? `instance ${finding.instance_id}` : undefined,
    finding.cluster_id ? `cluster ${finding.cluster_id}` : undefined,
  ].filter((value): value is string => Boolean(value))
}
