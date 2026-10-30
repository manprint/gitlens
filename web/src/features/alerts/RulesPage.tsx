import { ListChecks } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import type { AlertRule } from '@/api/alerts'
import { useAlertRules } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'

import { RuleEditor } from './RuleEditor'
import { Button } from '@/components/ui/button'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'

function tierLabel(rule: AlertRule): string {
  const tier = String(rule.min_tier ?? rule.tier ?? '')
  if (tier === 'T0' || tier === '0') return 'T0'
  if (tier === 'T1' || tier === '1') return 'T1'
  if (tier === 'T2' || tier === '2') return 'T2'
  return tier === '' ? '—' : tier
}

function isTierZero(rule: AlertRule): boolean {
  return tierLabel(rule) === 'T0'
}

function valueLabel(value: number | undefined): string {
  return value === undefined ? '—' : String(value)
}

function enabledLabel(rule: AlertRule): string {
  return rule.enabled === undefined ? '—' : rule.enabled ? 'Yes' : 'No'
}

const RULE_TOOLBAR: TableToolbarSpec<AlertRule> = {
  searchFields: [(rule) => rule.id],
  filters: [
    { id: 'severity', label: 'Severity', kind: 'select', accessor: (rule) => rule.severity },
    { id: 'enabled', label: 'Enabled', kind: 'select', accessor: enabledLabel },
  ],
}

function ruleColumns(onEdit: (ruleId: string) => void): SimpleColumn<AlertRule>[] {
  return [
    {
      id: 'rule',
      header: 'Rule',
      rowHeader: true,
      sortValue: (rule) => rule.id,
      cell: (rule) => rule.id,
    },
    { id: 'tier', header: 'Tier', sortValue: tierLabel, cell: tierLabel },
    {
      id: 'severity',
      header: 'Severity',
      sortValue: (rule) => rule.severity,
      cell: (rule) => rule.severity,
    },
    {
      id: 'threshold',
      header: 'Threshold',
      sortValue: (rule) => rule.threshold,
      cell: (rule) => valueLabel(rule.threshold),
    },
    {
      id: 'duration',
      header: 'Duration',
      sortValue: (rule) => rule.for_seconds,
      cell: (rule) => (rule.for_seconds === undefined ? '—' : `${rule.for_seconds}s`),
    },
    { id: 'enabled', header: 'Enabled', cell: enabledLabel },
    {
      id: 'action',
      header: 'Action',
      cell: (rule) =>
        isTierZero(rule) ? (
          <span className="text-muted-foreground">Always on by design</span>
        ) : (
          <Button onClick={() => onEdit(rule.id)} size="sm" type="button" variant="outline">
            Edit {rule.id}
          </Button>
        ),
    },
  ]
}

export function RulesPage() {
  const rulesQuery = useAlertRules()
  const [editingRuleId, setEditingRuleId] = useState<string>()

  const rules = useMemo(
    () => [...(rulesQuery.data ?? [])].sort((left, right) => left.id.localeCompare(right.id)),
    [rulesQuery.data],
  )
  const editingRule = rules.find((rule) => rule.id === editingRuleId)
  const columns = useMemo(() => ruleColumns(setEditingRuleId), [])

  if (rulesQuery.error && !rulesQuery.data) {
    return (
      <ErrorState
        endpoint="alert rules"
        failure={rulesQuery.error}
        onRetry={() => void rulesQuery.refetch()}
      />
    )
  }
  if (rulesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label="Loading alert rules" role="status">
        Loading alert rules…
      </section>
    )
  }

  return (
    <div className="space-y-8">
      <PageHeader
        icon={ListChecks}
        eyebrow="Alerting"
        actions={
          <Link
            className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
            to="/alerts"
          >
            Back to alerts
          </Link>
        }
        freshness={<FreshnessBadge dataUpdatedAt={rulesQuery.dataUpdatedAt} policy="findings" />}
        subtitle="Review built-in and operator-managed alert thresholds."
        title="Alert rules"
      />

      {rulesQuery.error ? (
        <ErrorState
          endpoint="alert rules"
          failure={rulesQuery.error}
          onRetry={() => void rulesQuery.refetch()}
        />
      ) : null}

      {rules.length === 0 ? (
        <EmptyState description="No alert rules are currently available." title="No alert rules" />
      ) : (
        <Section
          description="Tier 0 staleness rules are always on by design. Tier 1 rules can be changed and are saved by the server."
          title="Configured rules"
        >
          <SimpleTable
            ariaLabel="Alert rules"
            columns={columns}
            emptyState={null}
            rowKey={(rule) => rule.id}
            rows={rules}
            scope="rules"
            tableId="alert-rules"
            toolbar={RULE_TOOLBAR}
          />
        </Section>
      )}

      {editingRule ? (
        <Section title="Edit rule">
          <RuleEditor
            key={editingRule.id}
            onCancel={() => setEditingRuleId(undefined)}
            rule={editingRule}
          />
        </Section>
      ) : null}
    </div>
  )
}
