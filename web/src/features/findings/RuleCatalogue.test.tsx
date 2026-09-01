import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { AdvisorRule, FindingRecord } from '@/lib/findings'
import { renderWithProviders } from '@/test/render'

import { RuleCatalogue } from './RuleCatalogue'

const findings: FindingRecord[] = [
  {
    finding_id: 'finding-cluster-1',
    rule_id: 'rule-open',
    severity: 'warning',
    state: 'open',
    scope: 'cluster',
    cluster_id: 'cluster-1',
    title: 'Open finding',
  },
  {
    finding_id: 'finding-cluster-2',
    rule_id: 'rule-open',
    severity: 'warning',
    state: 'open',
    scope: 'cluster',
    cluster_id: 'cluster-2',
    title: 'Open finding',
  },
  {
    finding_id: 'finding-degraded',
    rule_id: 'rule-open',
    severity: 'warning',
    state: 'degraded',
    scope: 'cluster',
    cluster_id: 'cluster-3',
    title: 'Degraded finding',
  },
]

const rules: AdvisorRule[] = [
  { id: 'rule-open', severity: 'warning', scope: 'cluster', needs: ['metric:lag'], min_tier: 'T0' },
  { id: 'rule-instance', severity: 'critical', scope: 'instance', needs: [], min_tier: 'T1' },
  { id: 'rule-sensitive', severity: 'info', scope: 'cluster', needs: ['host'], min_tier: 'T2' },
]

function renderCatalogue(
  catalogueRules: AdvisorRule[] = rules,
  catalogueFindings: FindingRecord[] = findings,
) {
  return renderWithProviders(
    <RuleCatalogue findings={catalogueFindings} rules={catalogueRules} />,
  )
}

describe('RuleCatalogue', () => {
  it('UI-FIND-030 renders every rule returned by the catalogue', () => {
    renderCatalogue()

    const table = screen.getByRole('table', { name: 'Advisor rule catalogue' })
    expect(within(table).getByText('rule-open')).toBeInTheDocument()
    expect(within(table).getByText('rule-instance')).toBeInTheDocument()
    expect(within(table).getByText('rule-sensitive')).toBeInTheDocument()
    expect(within(table).getByText('metric:lag')).toBeInTheDocument()
  })

  it('UI-FIND-031 shows firing and non-evaluable counts per rule', () => {
    renderCatalogue()

    const row = within(screen.getByRole('table')).getByRole('row', { name: /rule-open/ })
    expect(row).toHaveTextContent('2')
    expect(row).toHaveTextContent('1')
  })

  it('UI-FIND-032 filtering by min_tier shows what a grant would unlock', () => {
    renderCatalogue()

    fireEvent.change(screen.getByLabelText('Available at or below'), { target: { value: 'T1' } })

    const table = screen.getByRole('table', { name: 'Advisor rule catalogue' })
    expect(within(table).getByText('rule-open')).toBeInTheDocument()
    expect(within(table).getByText('rule-instance')).toBeInTheDocument()
    expect(within(table).queryByText('rule-sensitive')).not.toBeInTheDocument()
  })

  it('UI-FIND-033 an empty catalogue renders the empty state, not a blank table', () => {
    renderCatalogue([])

    expect(screen.getByRole('status')).toHaveTextContent('No advisor rules available')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })
})
