import { ListChecks } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
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
      truncate: (rule) => rule.id,
      cell: (rule) => rule.id,
    },
    { id: 'tier', header: 'Tier', width: '6rem', sortValue: tierLabel, cell: tierLabel },
    {
      id: 'severity',
      header: 'Severity',
      width: '8rem',
      sortValue: (rule) => rule.severity,
      cell: (rule) => rule.severity,
    },
    {
      id: 'threshold',
      header: 'Threshold',
      width: '9rem',
      sortValue: (rule) => rule.threshold,
      cell: (rule) => valueLabel(rule.threshold),
    },
    {
      id: 'duration',
      header: 'Duration',
      width: '9rem',
      sortValue: (rule) => rule.for_seconds,
      cell: (rule) => (rule.for_seconds === undefined ? '—' : `${rule.for_seconds}s`),
    },
    { id: 'enabled', header: 'Enabled', width: '7rem', cell: enabledLabel },
    {
      id: 'action',
      header: 'Action',
      width: '18rem',
      cell: (rule) =>
        isTierZero(rule) ? (
          <span className="text-muted-foreground">Always on by design</span>
        ) : (
          // From 1600 px the button fits the one-line row (D26).
          <Button
            className="min-[1600px]:h-6 min-[1600px]:text-xs"
            onClick={() => onEdit(rule.id)}
            size="sm"
            type="button"
            variant="outline"
          >
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
  // The editor opens above the list, away from the Edit button: focus its heading.
  const editingId = editingRule?.id
  useEffect(() => {
    if (editingId !== undefined) document.getElementById('alert-rule-editor-title')?.focus()
  }, [editingId])
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
    <div className="space-y-8 min-[1600px]:space-y-4">
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

      {/* Above the list: with 20 rows per page a form below it would open off screen (15.3). */}
      {editingRule ? (
        <Section captionId="rules.edit" title="Edit rule">
          <RuleEditor
            key={editingRule.id}
            onCancel={() => setEditingRuleId(undefined)}
            rule={editingRule}
          />
        </Section>
      ) : null}

      {rules.length === 0 ? (
        <EmptyState description="No alert rules are currently available." title="No alert rules" />
      ) : (
        <Section captionId="rules.list" title="Configured rules">
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
    </div>
  )
}
