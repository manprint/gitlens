import { ListChecks } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import type { AlertRule } from '@/api/alerts'
import { useAlertRules } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { useT, type Translate } from '@/i18n/react'
import { ruleText } from '@/lib/ruleText'

import { severityText } from './AlertRow'
import { RuleEditor } from './RuleEditor'
import { thresholdText } from './ruleThreshold'
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

function enabledLabel(rule: AlertRule): string {
  return rule.enabled === undefined ? '—' : rule.enabled ? 'Yes' : 'No'
}

function enabledText(value: string, t: Translate): string {
  if (value === 'Yes') return t('alerts.rules.yes')
  return value === 'No' ? t('alerts.rules.no') : value
}

// The options are the values the rules have, sorted as the derived options are; only the labels are translated.
function distinctOptions(values: readonly string[], label: (value: string) => string) {
  return [...new Set(values)]
    .sort((left, right) => left.localeCompare(right))
    .map((value) => ({ value, label: label(value) }))
}

function ruleToolbar(rules: readonly AlertRule[], t: Translate): TableToolbarSpec<AlertRule> {
  return {
    searchFields: [(rule) => rule.id, (rule) => ruleText(rule.id) ?? ''],
    filters: [
      {
        id: 'severity',
        label: t('alerts.column.severity'),
        kind: 'select',
        accessor: (rule) => rule.severity,
        options: distinctOptions(
          rules.map((rule) => rule.severity),
          (value) => severityText(value, t),
        ),
      },
      {
        id: 'enabled',
        label: t('alerts.rules.enabled'),
        kind: 'select',
        accessor: enabledLabel,
        options: distinctOptions(rules.map(enabledLabel), (value) => enabledText(value, t)),
      },
    ],
  }
}

function ruleLine(ruleId: string): string {
  const text = ruleText(ruleId)
  return text === undefined ? ruleId : `${text} ${ruleId}`
}

function RuleCell({ ruleId }: { ruleId: string }) {
  const text = ruleText(ruleId)
  if (text === undefined) return <>{ruleId}</>
  return (
    <>
      {text} <span className="text-muted-foreground font-mono text-xs">{ruleId}</span>
    </>
  )
}

function ruleColumns(onEdit: (ruleId: string) => void, t: Translate): SimpleColumn<AlertRule>[] {
  return [
    {
      id: 'rule',
      header: t('alerts.column.rule'),
      rowHeader: true,
      sortValue: (rule) => rule.id,
      help: 'rules.rule',
      truncate: (rule) => ruleLine(rule.id),
      cell: (rule) => <RuleCell ruleId={rule.id} />,
    },
    {
      id: 'tier',
      header: t('alerts.rules.tier'),
      width: '6rem',
      help: 'rules.tier',
      sortValue: tierLabel,
      cell: tierLabel,
    },
    {
      id: 'severity',
      header: t('alerts.column.severity'),
      width: '8rem',
      sortValue: (rule) => rule.severity,
      cell: (rule) => severityText(rule.severity, t),
    },
    {
      id: 'threshold',
      header: t('alerts.rules.threshold'),
      width: '16rem',
      help: 'rules.threshold',
      sortValue: (rule) => rule.threshold,
      truncate: thresholdText,
      cell: thresholdText,
    },
    {
      id: 'duration',
      header: t('alerts.rules.duration'),
      width: '8rem',
      help: 'rules.duration',
      sortValue: (rule) => rule.for_seconds,
      cell: (rule) => (rule.for_seconds === undefined ? '—' : `${rule.for_seconds}s`),
    },
    {
      id: 'enabled',
      header: t('alerts.rules.enabled'),
      width: '7rem',
      cell: (rule) => enabledText(enabledLabel(rule), t),
    },
    {
      id: 'action',
      header: t('alerts.column.action'),
      width: '18rem',
      cell: (rule) =>
        isTierZero(rule) ? (
          <span className="text-muted-foreground">{t('alerts.rules.alwaysOn')}</span>
        ) : (
          // From 1600 px the button fits the one-line row (D26).
          <Button
            className="min-[1600px]:h-6 min-[1600px]:text-xs"
            onClick={() => onEdit(rule.id)}
            size="sm"
            type="button"
            variant="outline"
          >
            {t('alerts.rules.edit', { id: rule.id })}
          </Button>
        ),
    },
  ]
}

export function RulesPage() {
  const t = useT()
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
  const columns = useMemo(() => ruleColumns(setEditingRuleId, t), [t])
  const toolbar = useMemo(() => ruleToolbar(rules, t), [rules, t])

  if (rulesQuery.error && !rulesQuery.data) {
    return (
      <ErrorState
        endpoint={t('alerts.endpoint.rules')}
        failure={rulesQuery.error}
        onRetry={() => void rulesQuery.refetch()}
      />
    )
  }
  if (rulesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label={t('alerts.rules.loadingLabel')} role="status">
        {t('alerts.rules.loading')}
      </section>
    )
  }

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="alert-rules" label={t('alerts.rules.helpLabel')} />}
        icon={ListChecks}
        eyebrow={t('alerts.page.eyebrow')}
        actions={
          <Link
            className="hover:bg-muted inline-flex h-9 items-center gap-1.5 rounded-md border px-3 text-sm font-medium transition-colors"
            to="/alerts"
          >
            {t('alerts.rules.back')}
          </Link>
        }
        freshness={<FreshnessBadge dataUpdatedAt={rulesQuery.dataUpdatedAt} policy="findings" />}
        subtitle={t('alerts.rules.subtitle')}
        title={t('alerts.rules.title')}
      />

      {rulesQuery.error ? (
        <ErrorState
          endpoint={t('alerts.endpoint.rules')}
          failure={rulesQuery.error}
          onRetry={() => void rulesQuery.refetch()}
        />
      ) : null}

      {/* Above the list: with 20 rows per page a form below it would open off screen (15.3). */}
      {editingRule ? (
        <Section
          actions={<HelpLink anchor="alert-rules#editing" label={t('alerts.rules.editTitle')} />}
          captionId="rules.edit"
          title={t('alerts.rules.editTitle')}
        >
          <RuleEditor
            key={editingRule.id}
            onCancel={() => setEditingRuleId(undefined)}
            rule={editingRule}
          />
        </Section>
      ) : null}

      {rules.length === 0 ? (
        <EmptyState
          description={t('alerts.rules.emptyDescription')}
          title={t('alerts.rules.emptyTitle')}
        />
      ) : (
        <Section
          actions={
            <HelpLink anchor="alert-rules#rules-table" label={t('alerts.rules.configured')} />
          }
          captionId="rules.list"
          title={t('alerts.rules.configured')}
        >
          <SimpleTable
            ariaLabel={t('alerts.rules.title')}
            captionId="rules.table"
            columns={columns}
            emptyState={null}
            rowKey={(rule) => rule.id}
            rows={rules}
            scope="rules"
            tableId="alert-rules"
            toolbar={toolbar}
          />
        </Section>
      )}
    </div>
  )
}
