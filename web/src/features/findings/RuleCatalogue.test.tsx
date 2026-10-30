import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { AdvisorRule, FindingRecord } from '@/lib/findings'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'

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
  {
    id: 'rule-open',
    severity: 'warning',
    scope: 'cluster',
    needs: ['metric:lag'],
    min_tier: 'T0',
    enabled: true,
    overridden: false,
  },
  {
    id: 'rule-instance',
    severity: 'critical',
    scope: 'instance',
    needs: [],
    min_tier: 'T1',
    enabled: true,
    overridden: false,
  },
  {
    id: 'rule-sensitive',
    severity: 'info',
    scope: 'cluster',
    needs: ['host'],
    min_tier: 'T2',
    enabled: true,
    overridden: false,
  },
]

function renderCatalogue(
  catalogueRules: AdvisorRule[] = rules,
  catalogueFindings: FindingRecord[] = findings,
) {
  return renderWithProviders(<RuleCatalogue findings={catalogueFindings} rules={catalogueRules} />)
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

  it('UI-FIND-034 a muted finding is not counted as firing', () => {
    renderCatalogue(
      [
        {
          id: 'rule-muted',
          severity: 'warning',
          scope: 'cluster',
          needs: [],
          min_tier: 'T0',
          enabled: true,
          overridden: false,
        },
      ],
      [
        {
          finding_id: 'finding-muted',
          rule_id: 'rule-muted',
          severity: 'warning',
          state: 'muted',
          scope: 'cluster',
          cluster_id: 'cluster-1',
          title: 'Muted finding',
        },
      ],
    )

    const row = within(screen.getByRole('table')).getByRole('row', { name: /rule-muted/ })
    expect(
      within(row)
        .getAllByRole('cell')
        .map((cell) => cell.textContent),
    ).toEqual(['rule-muted', 'warning', 'cluster', 'None', 'T0', '0', '0'])
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

  it('TBL-UI-012 advisor-rule-catalogue pages, searches and sorts the rules', () => {
    const many: AdvisorRule[] = seq(25).map((n, index) => ({
      id: `rule-${n}`,
      severity: 'warning',
      scope: 'instance',
      needs: index < 3 ? ['host'] : [],
      min_tier: 'T0',
      enabled: true,
      overridden: false,
    }))
    renderCatalogue(many, [])
    const name = 'Advisor rule catalogue'

    expect(dataRows(name)).toHaveLength(20)
    searchTable(name, 'host')
    expect(columnTexts(name)).toEqual(['rule-01', 'rule-02', 'rule-03'])
    searchTable(name, '')
    sortTable(name, 'Rule ID', 'descending')
    expect(columnTexts(name)[0]).toBe('rule-25')
  })

  it('FHD-APPLY-018 puts the catalogue filters on the toolbar line and keeps each rule on one line', () => {
    renderCatalogue()

    const search = screen.getByRole('search', { name: 'Advisor rule catalogue search' })
    for (const name of ['Rule level', 'Rule scope', 'Available at or below']) {
      const select = screen.getByRole('combobox', { name })
      // After the search box, on its line, the name above hidden from 1600 px.
      expect(search.compareDocumentPosition(select) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      expect(select).toHaveClass('h-8')
      expect(screen.getByText(name, { selector: 'span' })).toHaveClass('min-[1600px]:hidden')
    }
    const table = screen.getByRole('table', { name: 'Advisor rule catalogue' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getByTitle('metric:lag')).toHaveClass('min-[1600px]:truncate')
  })

  it('FHD-APPLY-019 keeps the filters reachable when they leave no rule to show', () => {
    renderCatalogue()

    fireEvent.change(screen.getByLabelText('Rule level'), { target: { value: 'info' } })
    fireEvent.change(screen.getByLabelText('Available at or below'), { target: { value: 'T0' } })

    expect(screen.getByRole('status')).toHaveTextContent('No rules match these filters')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Rule level'), { target: { value: 'all' } })
    expect(screen.getByRole('table', { name: 'Advisor rule catalogue' })).toBeInTheDocument()
  })

  it('ADVO-UI-004 a rule disabled in Settings shows a Disabled badge; the enabled ones none', () => {
    renderCatalogue(
      rules.map((rule) =>
        rule.id === 'rule-instance' ? { ...rule, enabled: false, overridden: true } : rule,
      ),
      [],
    )

    const table = screen.getByRole('table', { name: 'Advisor rule catalogue' })
    const badges = within(table).getAllByText('Disabled')
    expect(badges).toHaveLength(1)
    // The badge sits in the rule's own cell, whose title keeps the bare rule id.
    expect(within(table).getByTitle('rule-instance')).toContainElement(badges[0]!)
    expect(within(table).getByTitle('rule-instance')).toHaveTextContent('rule-instanceDisabled')
  })
})
