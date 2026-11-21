import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { PlanView } from './PlanView'

import { parsePlan, type ParsedPlan } from '@/lib/planTree'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

function plan(value: unknown, valuesFrom: Parameters<typeof parsePlan>[1] = null): ParsedPlan {
  const parsed = parsePlan(value, valuesFrom)
  if (!parsed) throw new Error('no plan')
  return parsed
}

const ANALYZED = plan({
  'Execution Time': 80,
  Plan: {
    'Node Type': 'Nested Loop',
    'Total Cost': 120,
    'Plan Rows': 5,
    'Actual Rows': 900,
    'Actual Loops': 1,
    'Actual Total Time': 79,
    'Shared Hit Blocks': 128,
    'Shared Read Blocks': 0,
    Plans: [
      {
        'Node Type': 'Seq Scan',
        'Relation Name': 'events',
        'Total Cost': 100,
        'Plan Rows': 5,
        'Actual Rows': 900,
        'Actual Loops': 1,
        'Actual Total Time': 70,
        Filter: '(kind = 2)',
        'Rows Removed by Filter': 50000,
        'Shared Hit Blocks': 100,
        'Shared Read Blocks': 28,
      },
    ],
  },
})

describe('PlanView', () => {
  it('UI-QRY-060 shows what to look at first and jumps to the operation', () => {
    vi.useRealTimers()
    renderWithProviders(<PlanView plan={ANALYZED} valuesFrom="values" />)

    expect(screen.getByLabelText('Plan summary')).toHaveTextContent('Values you entered')
    const insights = screen.getByRole('region', { name: 'What to look at' })
    expect(insights).toHaveTextContent('Seq Scan on events takes 88% of the execution time')
    expect(insights).toHaveTextContent('Row estimate off ×180 at Seq Scan on events')

    expect(screen.getByRole('button', { name: 'Show details of Seq Scan' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    fireEvent.click(within(insights).getAllByRole('button', { name: 'Show in plan' })[0]!)
    const scan = screen.getByRole('button', { name: 'Hide details of Seq Scan' })
    expect(scan).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getAllByRole('row')[2]).toHaveTextContent('Rows removed by Filter')
  })

  it('UI-QRY-061 shows rows, loops, time and buffers for an executed plan', () => {
    vi.useRealTimers()
    renderWithProviders(<PlanView plan={ANALYZED} />)

    const tree = screen.getByRole('table', { name: 'Query plan tree' })
    const headers = within(tree)
      .getAllByRole('columnheader')
      .map((header) => header.textContent)
    expect(headers).toEqual([
      'Operation',
      'Rows (actual / est.)',
      'Loops',
      'Time in node',
      'Buffers',
    ])
    const scanRow = within(tree).getAllByRole('row')[2]!
    expect(scanRow).toHaveTextContent('×180 under')
    expect(scanRow).toHaveTextContent('Filter: (kind = 2)')
    expect(scanRow).toHaveTextContent('cached')
  })

  it('UI-QRY-062 shows estimates and cost for a plan-only result', () => {
    vi.useRealTimers()
    const planOnly = plan(
      {
        Plan: { 'Node Type': 'Seq Scan', 'Relation Name': 't', 'Total Cost': 10, 'Plan Rows': 100 },
      },
      'generic',
    )
    renderWithProviders(<PlanView plan={planOnly} valuesFrom="generic" />)

    const headers = within(screen.getByRole('table', { name: 'Query plan tree' }))
      .getAllByRole('columnheader')
      .map((header) => header.textContent)
    expect(headers).toEqual(['Operation', 'Rows (est.)', 'Cost in node'])
    expect(screen.getByRole('region', { name: 'What to look at' })).toHaveTextContent(
      'Generic plan',
    )
  })

  it('UI-QRY-063 switches between tree, psql text and JSON', async () => {
    vi.useRealTimers()
    const { container } = renderWithProviders(<PlanView plan={ANALYZED} />)

    fireEvent.click(screen.getByRole('button', { name: 'Text' }))
    expect(screen.getByRole('button', { name: 'Text' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByLabelText('Plan as text').textContent).toContain(
      '\n  ->  Seq Scan on events',
    )
    expect(screen.queryByRole('table', { name: 'Query plan tree' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'JSON' }))
    expect(screen.getByLabelText('Plan as JSON')).toHaveTextContent('"Relation Name": "events"')
    fireEvent.click(screen.getByRole('button', { name: 'Tree' }))
    expect(screen.getByRole('table', { name: 'Query plan tree' })).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })
})
