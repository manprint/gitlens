import { useMemo, useState, type ReactNode } from 'react'

import type { PermTier } from '@/api/types'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { Section } from '@/components/layout/Section'
import { Badge } from '@/components/ui/badge'
import { useT, type Translate } from '@/i18n/react'
import { useKeepFocus } from '@/hooks/useKeepFocus'
import type { AdvisorRule, FindingRecord } from '@/lib/findings'
import { needsText, ruleText } from '@/lib/ruleText'
import { NativeSelect } from '@/components/ui/native-select'

import { scopeText, severityText } from './findingDisplay'

const permissionTiers: readonly PermTier[] = ['T0', 'T1', 'T2']
const tierRank: Record<PermTier, number> = { T0: 0, T1: 1, T2: 2 }

type FilterValue = string

interface RuleCatalogueRow {
  ruleId: string
  /** False for a rule disabled from Settings → Advisor rules (11.4). */
  enabled: boolean
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
    enabled: rule.enabled,
    severity: rule.severity,
    scope: rule.scope,
    needs: needsText(rule.needs),
    minTier: rule.min_tier,
    firingInstances: countTargets(findings, rule.id, (finding) => finding.state === 'open'),
    nonEvaluableInstances: countTargets(
      findings,
      rule.id,
      (finding) => finding.state === 'degraded',
    ),
  }
}

// Severity, scope and tier already have their own selects on the toolbar line.
const RULE_TOOLBAR: DataTableToolbar<RuleCatalogueRow> = {
  searchFields: [(row) => row.ruleId, (row) => ruleText(row.ruleId) ?? '', (row) => row.needs],
}

function ruleColumns(t: Translate): DataTableProps<RuleCatalogueRow>['columns'] {
  return [
    // One line per rule from 1600 px (D26): the free-text columns truncate.
    {
      accessorKey: 'ruleId',
      header: t('findings.ruleCatalogue.rule'),
      // A disabled rule is not evaluated, so its counts stay at zero (11.4).
      cell: ({ row }) => (
        <>
          {ruleText(row.original.ruleId)}
          {ruleText(row.original.ruleId) === undefined ? null : ' '}
          <span
            className={`font-mono text-xs ${ruleText(row.original.ruleId) === undefined ? '' : 'text-muted-foreground'}`}
          >
            {row.original.ruleId}
          </span>
          {row.original.enabled ? null : (
            <Badge
              className="ml-2"
              title={t('findings.ruleCatalogue.disabledTitle')}
              variant="outline"
            >
              {t('findings.ruleCatalogue.disabled')}
            </Badge>
          )}
        </>
      ),
      meta: { width: '40%', truncate: true, help: 'rules.rule' as const },
    },
    {
      accessorKey: 'severity',
      header: t('findings.table.severity'),
      enableHiding: false,
      cell: ({ row }) => severityText(t, row.original.severity),
      meta: { width: '8rem' },
    },
    {
      accessorKey: 'scope',
      header: t('findings.ruleCatalogue.scope'),
      enableHiding: false,
      cell: ({ row }) => scopeText(t, row.original.scope),
      meta: { width: '8rem', help: 'findings.scope' as const },
    },
    {
      accessorKey: 'needs',
      header: t('findings.ruleCatalogue.needs'),
      meta: { truncate: true, help: 'rules.needs' as const },
    },
    {
      accessorKey: 'minTier',
      header: t('findings.ruleCatalogue.minTier'),
      meta: { width: '9.5rem', help: 'settings.permission-tier' as const },
    },
    {
      accessorKey: 'firingInstances',
      header: t('findings.ruleCatalogue.firingInstances'),
      meta: { width: '11rem' },
    },
    {
      accessorKey: 'nonEvaluableInstances',
      header: t('findings.ruleCatalogue.cannotEvaluate'),
      meta: { width: '13rem' },
    },
  ]
}

/**
 * One catalogue filter. From 1600 px it sits on the table toolbar line (D26):
 * the name above the select is hidden and the select keeps it as its
 * accessible name.
 */
function CatalogueFilter({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="text-muted-foreground flex flex-col gap-1 text-xs">
      <span aria-hidden="true" className="font-medium min-[1600px]:hidden">
        {label}
      </span>
      {children}
    </div>
  )
}

function filterOptions(values: readonly string[]): string[] {
  return [...new Set(values)].sort((left, right) => left.localeCompare(right))
}

export function RuleCatalogue({ rules, findings }: RuleCatalogueProps) {
  const t = useT()
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
  const columns = useMemo(() => ruleColumns(t), [t])
  const emptyState =
    rules.length === 0 ? (
      <EmptyState
        description={t('findings.ruleCatalogue.emptyRulesDescription')}
        title={t('findings.ruleCatalogue.emptyRulesTitle')}
      />
    ) : (
      <EmptyState
        description={t('findings.ruleCatalogue.noMatchDescription')}
        title={t('findings.ruleCatalogue.noMatchTitle')}
      />
    )

  const filters = (
    <>
      <CatalogueFilter label={t('findings.ruleCatalogue.severityFilter')}>
        <NativeSelect
          aria-label={t('findings.ruleCatalogue.severityFilter')}
          className="h-8 px-2 text-sm"
          id="catalogue-severity"
          onChange={(event) => setSeverity(event.target.value)}
          value={severity}
        >
          <option value="all">{t('findings.ruleCatalogue.allSeverities')}</option>
          {severityOptions.map((value) => (
            <option key={value} value={value}>
              {severityText(t, value)}
            </option>
          ))}
        </NativeSelect>
      </CatalogueFilter>
      <CatalogueFilter label={t('findings.ruleCatalogue.scopeFilter')}>
        <NativeSelect
          aria-label={t('findings.ruleCatalogue.scopeFilter')}
          className="h-8 px-2 text-sm"
          id="catalogue-scope"
          onChange={(event) => setScope(event.target.value)}
          value={scope}
        >
          <option value="all">{t('findings.ruleCatalogue.allScopes')}</option>
          {scopeOptions.map((value) => (
            <option key={value} value={value}>
              {scopeText(t, value)}
            </option>
          ))}
        </NativeSelect>
      </CatalogueFilter>
      <CatalogueFilter label={t('findings.ruleCatalogue.tierFilter')}>
        <div className="flex items-center gap-1">
          <NativeSelect
            aria-label={t('findings.ruleCatalogue.tierFilter')}
            className="h-8 px-2 text-sm"
            id="catalogue-tier"
            onChange={(event) => setMaxTier(event.target.value)}
            value={maxTier}
          >
            <option value="all">{t('findings.ruleCatalogue.allTiers')}</option>
            {permissionTiers.map((tier) => (
              <option key={tier} value={tier}>
                {tier}
              </option>
            ))}
          </NativeSelect>
          <InfoHint
            id="findings.catalogue-tier"
            label={t('findings.ruleCatalogue.tierFilterHint')}
          />
        </div>
      </CatalogueFilter>
    </>
  )

  const empty = rows.length === 0 && rules.length > 0
  const keepFocus = useKeepFocus(empty ? 'bar' : 'table')
  // `contents`: the wrapper only keeps focus, it adds no box to the layout.
  const filterControls = (
    <div className="contents" {...keepFocus}>
      {filters}
    </div>
  )

  return (
    <Section
      actions={
        <HelpLink anchor="findings#rule-catalogue" label={t('findings.ruleCatalogue.title')} />
      }
      captionId="findings.rule-catalogue"
      title={t('findings.ruleCatalogue.title')}
    >
      {empty ? (
        // The table hides its toolbar without rows, so the filters that
        // emptied it stay reachable here.
        <div className="space-y-3">
          <div className="flex flex-wrap items-end gap-3">{filterControls}</div>
          <div role="status">{emptyState}</div>
        </div>
      ) : (
        <DataTable
          ariaLabel={t('findings.ruleCatalogue.tableAriaLabel')}
          captionId="findings.rule-catalogue-table"
          tableId="advisor-rule-catalogue"
          toolbar={RULE_TOOLBAR}
          toolbarExtra={filterControls}
          columns={columns}
          data={rows}
          emptyState={emptyState}
          scope="rules"
          total={rules.length}
        />
      )}
    </Section>
  )
}
