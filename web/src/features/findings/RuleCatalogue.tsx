import { useMemo, useState } from 'react'

import type { PermTier } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state'
import { Section } from '@/components/layout/Section'
import type { AdvisorRule, FindingRecord } from '@/lib/findings'

const permissionTiers: readonly PermTier[] = ['T0', 'T1', 'T2']
const tierRank: Record<PermTier, number> = { T0: 0, T1: 1, T2: 2 }

type FilterValue = string

interface RuleCatalogueRow {
  ruleId: string
  severity: string
  scope: string
  needs: string
  minTier: PermTier
  firingInstances: number
  nonEvaluableInstances: number
}

export interface RuleCatalogueProps {
  rules: readonly AdvisorRule[]
  findings: readonly FindingRecord[]
}

function findingTarget(finding: FindingRecord): string {
  const target = finding.scope === 'instance' ? finding.instance_id : finding.cluster_id
  return target === undefined ? finding.finding_id : String(target)
}

function countTargets(
  findings: readonly FindingRecord[],
  ruleId: string,
  predicate: (finding: FindingRecord) => boolean,
): number {
  return new Set(
    findings
      .filter((finding) => finding.rule_id === ruleId && predicate(finding))
      .map(findingTarget),
  ).size
}

function makeRow(rule: AdvisorRule, findings: readonly FindingRecord[]): RuleCatalogueRow {
  return {
    ruleId: rule.id,
    severity: rule.severity,
    scope: rule.scope,
    needs: rule.needs.length > 0 ? rule.needs.join(', ') : 'None',
    minTier: rule.min_tier,
    firingInstances: countTargets(
      findings,
      rule.id,
      (finding) => finding.state === 'open' || finding.state === 'muted',
    ),
    nonEvaluableInstances: countTargets(
      findings,
      rule.id,
      (finding) => finding.state === 'degraded',
    ),
  }
}

function ruleColumns(): DataTableProps<RuleCatalogueRow>['columns'] {
  return [
    { accessorKey: 'ruleId', header: 'Rule ID' },
    { accessorKey: 'severity', header: 'Severity', enableHiding: false },
    { accessorKey: 'scope', header: 'Scope', enableHiding: false },
    { accessorKey: 'needs', header: 'Needs' },
    { accessorKey: 'minTier', header: 'Minimum tier' },
    {
      accessorKey: 'firingInstances',
      header: 'Firing instances',
    },
    {
      accessorKey: 'nonEvaluableInstances',
      header: 'Cannot evaluate',
    },
  ]
}

function filterOptions(values: readonly string[]): string[] {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right))
}

export function RuleCatalogue({ rules, findings }: RuleCatalogueProps) {
  const [severity, setSeverity] = useState<FilterValue>('all')
  const [scope, setScope] = useState<FilterValue>('all')
  const [maxTier, setMaxTier] = useState<FilterValue>('all')

  const severityOptions = useMemo(() => filterOptions(rules.map((rule) => rule.severity)), [rules])
  const scopeOptions = useMemo(() => filterOptions(rules.map((rule) => rule.scope)), [rules])
  const rows = useMemo(
    () =>
      rules
        .map((rule) => makeRow(rule, findings))
        .filter(
          (row) =>
            (severity === 'all' || row.severity === severity) &&
            (scope === 'all' || row.scope === scope) &&
            (maxTier === 'all' || tierRank[row.minTier] <= tierRank[maxTier as PermTier]),
        ),
    [findings, maxTier, rules, scope, severity],
  )
  const columns = useMemo(() => ruleColumns(), [])
  const emptyState =
    rules.length === 0 ? (
      <EmptyState
        description="The live advisor catalogue did not return any rules."
        title="No advisor rules available"
      />
    ) : (
      <EmptyState
        description="Try clearing one or more catalogue filters."
        title="No rules match these filters"
      />
    )

  return (
    <Section
      title="Rule catalogue"
      description="See which checks are available at each permission tier and how many targets currently exercise them."
    >
      <div className="mb-4 grid gap-4 sm:grid-cols-3">
        <label className="space-y-1 text-sm" htmlFor="catalogue-severity">
          <span className="font-medium">Rule level</span>
          <select
            className="border-input bg-background h-9 w-full rounded-md border px-3"
            id="catalogue-severity"
            onChange={(event) => setSeverity(event.target.value)}
            value={severity}
          >
            <option value="all">All severities</option>
            {severityOptions.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-sm" htmlFor="catalogue-scope">
          <span className="font-medium">Rule scope</span>
          <select
            className="border-input bg-background h-9 w-full rounded-md border px-3"
            id="catalogue-scope"
            onChange={(event) => setScope(event.target.value)}
            value={scope}
          >
            <option value="all">All scopes</option>
            {scopeOptions.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1 text-sm" htmlFor="catalogue-tier">
          <span className="font-medium">Available at or below</span>
          <select
            className="border-input bg-background h-9 w-full rounded-md border px-3"
            id="catalogue-tier"
            onChange={(event) => setMaxTier(event.target.value)}
            value={maxTier}
          >
            <option value="all">All permission tiers</option>
            {permissionTiers.map((tier) => (
              <option key={tier} value={tier}>
                {tier}
              </option>
            ))}
          </select>
        </label>
      </div>
      <DataTable
        ariaLabel="Advisor rule catalogue"
        columns={columns}
        data={rows}
        emptyState={emptyState}
        scope="rules"
        total={rules.length}
      />
    </Section>
  )
}
