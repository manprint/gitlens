import { ListChecks } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import type { AlertRule } from '@/api/alerts'
import { useAlertRules } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'

import { RuleEditor } from './RuleEditor'
import { Button } from '@/components/ui/button'

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

export function RulesPage() {
  const rulesQuery = useAlertRules()
  const [editingRuleId, setEditingRuleId] = useState<string>()

  const rules = useMemo(
    () => [...(rulesQuery.data ?? [])].sort((left, right) => left.id.localeCompare(right.id)),
    [rulesQuery.data],
  )
  const editingRule = rules.find((rule) => rule.id === editingRuleId)

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
          <div className="relative overflow-x-auto" tabIndex={0}>
            <table className="w-full text-left text-sm" aria-label="Alert rules">
              <thead>
                <tr className="border-b">
                  <th className="px-3 py-2 font-medium" scope="col">
                    Rule
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Tier
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Severity
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Threshold
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Duration
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Enabled
                  </th>
                  <th className="px-3 py-2 font-medium" scope="col">
                    Action
                  </th>
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => {
                  const tierZero = isTierZero(rule)
                  return (
                    <tr className="border-b align-top last:border-b-0" key={rule.id}>
                      <th className="px-3 py-3 font-medium" scope="row">
                        {rule.id}
                      </th>
                      <td className="px-3 py-3">{tierLabel(rule)}</td>
                      <td className="px-3 py-3">{rule.severity}</td>
                      <td className="px-3 py-3">{valueLabel(rule.threshold)}</td>
                      <td className="px-3 py-3">
                        {rule.for_seconds === undefined ? '—' : `${rule.for_seconds}s`}
                      </td>
                      <td className="px-3 py-3">
                        {rule.enabled === undefined ? '—' : rule.enabled ? 'Yes' : 'No'}
                      </td>
                      <td className="px-3 py-3">
                        {tierZero ? (
                          <span className="text-muted-foreground">Always on by design</span>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"

                            onClick={() => setEditingRuleId(rule.id)}
                            type="button"
                          >
                            Edit {rule.id}
                          </Button>
                        )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
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
