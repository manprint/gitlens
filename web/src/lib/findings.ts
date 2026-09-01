import type { Schemas } from '@/api/types'

export type Finding = Schemas['Finding']
export type AdvisorRule = Schemas['AdvisorRule']

/** Runtime fields returned by the findings API but not yet in the OpenAPI schema. */
export type FindingRecord = Finding & {
  first_seen?: string
  last_seen?: string
  resolved_at?: string | null
  muted_until?: string | null
  mute_reason?: string | null
}

export interface FindingSummary {
  critical: number
  warning: number
  info: number
  degraded: number
}

export interface JoinedFinding extends FindingRecord {
  catalogue: AdvisorRule | null
  catalogueMissing: boolean
  needs: string[]
  min_tier: AdvisorRule['min_tier'] | null
}

const severityOrder: Record<string, number> = {
  critical: 0,
  warning: 1,
  info: 2,
}

const stateOrder: Record<Finding['state'], number> = {
  open: 0,
  degraded: 1,
  muted: 2,
  resolved: 3,
}

function compareText(left: string, right: string): number {
  if (left === right) return 0
  return left < right ? -1 : 1
}

function compareRank(left: FindingRecord, right: FindingRecord): number {
  const severity = (severityOrder[left.severity] ?? Number.MAX_SAFE_INTEGER) -
    (severityOrder[right.severity] ?? Number.MAX_SAFE_INTEGER)
  if (severity !== 0) return severity

  const state = stateOrder[left.state] - stateOrder[right.state]
  if (state !== 0) return state

  const scope = compareText(left.scope, right.scope)
  if (scope !== 0) return scope

  return compareText(left.rule_id, right.rule_id)
}

/** Rank findings without mutating the API response. Equal rows retain input order. */
export function rankFindings<T extends FindingRecord>(findings: readonly T[]): T[] {
  return [...findings].sort(compareRank)
}

/** Group findings by their declared scope while preserving unknown scopes. */
export function groupByScope(
  findings: readonly FindingRecord[],
): Record<string, FindingRecord[]> {
  const groups: Record<string, FindingRecord[]> = { cluster: [], instance: [] }
  for (const finding of findings) {
    const group = groups[finding.scope] ?? []
    group.push(finding)
    groups[finding.scope] = group
  }
  return groups
}

/** Count open severities separately from rules whose inputs could not be evaluated. */
export function summariseFindings(findings: readonly FindingRecord[]): FindingSummary {
  const summary: FindingSummary = { critical: 0, warning: 0, info: 0, degraded: 0 }
  for (const finding of findings) {
    if (finding.state === 'degraded') {
      summary.degraded += 1
      continue
    }
    if (finding.state !== 'open') continue

    if (finding.severity === 'critical') summary.critical += 1
    if (finding.severity === 'warning') summary.warning += 1
    if (finding.severity === 'info') summary.info += 1
  }
  return summary
}

/** Attach the authoritative catalogue entry without losing findings for missing rules. */
export function joinCatalogue(
  findings: readonly FindingRecord[],
  rules: readonly AdvisorRule[],
): JoinedFinding[] {
  const catalogue = new Map(rules.map((rule) => [rule.id, rule]))
  return findings.map((finding) => {
    const rule = catalogue.get(finding.rule_id) ?? null
    return {
      ...finding,
      catalogue: rule,
      catalogueMissing: rule === null,
      min_tier: rule?.min_tier ?? null,
      needs: rule?.needs ?? [],
    }
  })
}

/** Return remaining mute time in seconds using the caller's frozen clock. */
export function muteExpiry(finding: FindingRecord, now: Date): number | null {
  if (finding.state !== 'muted' || !finding.muted_until) return null

  const expiry = Date.parse(finding.muted_until)
  if (!Number.isFinite(expiry)) return null
  return Math.max(0, (expiry - now.getTime()) / 1000)
}
