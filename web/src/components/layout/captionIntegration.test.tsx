import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ChartCard } from '@/components/charts/ChartCard'
import { CAPTIONS } from '@/lib/captions'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

import { DataTable } from './DataTable'
import { Section } from './Section'

interface Row {
  query: string
  mean: number
}

const ROWS: Row[] = [
  { query: 'SELECT 1', mean: 2 },
  { query: 'SELECT 2', mean: 5 },
]

const RANGE = { from: new Date('2026-10-30T10:00:00Z'), to: new Date('2026-10-30T11:00:00Z') }

describe('captions on charts, sections and tables (D30)', () => {
  it('CAPCMP-005 a chart card shows its caption line and its ⓘ', () => {
    renderWithProviders(
      <ChartCard
        captionId="overview.tps"
        range={RANGE}
        state="ready"
        stepSeconds={60}
        title="Transactions"
      >
        <div />
      </ChartCard>,
    )
    const card = screen.getByRole('region', { name: 'Transactions' })
    expect(card).toHaveAttribute('data-caption-id', 'overview.tps')
    expect(within(card).getByText(CAPTIONS['overview.tps'].what)).toBeVisible()
    expect(within(card).getByRole('button', { name: 'About Transactions' })).toBeInTheDocument()
  })

  it('CAPCMP-005 a section caption replaces its description and adds the ⓘ', () => {
    renderWithProviders(
      <Section captionId="index.duplicate" description="Old description" title="Duplicates">
        <p>body</p>
      </Section>,
    )
    const section = screen.getByRole('region', { name: 'Duplicates' })
    expect(section).toHaveAttribute('data-caption-id', 'index.duplicate')
    expect(within(section).queryByText('Old description')).not.toBeInTheDocument()
    expect(within(section).getByTitle(CAPTIONS['index.duplicate'].what)).toHaveClass(
      'min-[1600px]:line-clamp-2',
    )
    fireEvent.focus(within(section).getByRole('button', { name: 'About Duplicates' }))
    expect(screen.getByRole('tooltip')).toHaveTextContent('Latest advisor evaluation.')
  })

  it('CAPCMP-003 a column hint keeps the sort button names of the header', () => {
    renderWithProviders(
      <DataTable
        ariaLabel="Statements"
        captionId="overview.tps"
        columns={[
          { accessorKey: 'query', header: 'Query' },
          { accessorKey: 'mean', header: 'Mean time', meta: { help: 'statements.mean-time' } },
        ]}
        data={ROWS}
        emptyState={<p>No statements</p>}
      />,
    )
    const header = screen.getByRole('columnheader', { name: /Mean time/ })
    expect(
      within(header).getByRole('button', { name: 'Sort Mean time ascending' }),
    ).toBeInTheDocument()
    expect(
      within(header).getByRole('button', { name: 'Sort Mean time descending' }),
    ).toBeInTheDocument()
    fireEvent.focus(within(header).getByRole('button', { name: 'About Mean time' }))
    expect(screen.getByRole('tooltip')).toHaveTextContent(/Average execution time of one call/)
    // The table caption sits in the toolbar row; the card's root carries the id once.
    expect(screen.getByTitle(CAPTIONS['overview.tps'].what)).not.toHaveAttribute('data-caption-id')
    expect(screen.getByRole('button', { name: 'About Statements' })).toBeInTheDocument()
  })

  it('CAPCMP-003 a captioned table with column hints has no serious accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = renderWithProviders(
      <DataTable
        ariaLabel="Statements"
        captionId="overview.tps"
        columns={[
          { accessorKey: 'query', header: 'Query' },
          { accessorKey: 'mean', header: 'Mean time', meta: { help: 'statements.mean-time' } },
        ]}
        data={ROWS}
        emptyState={<p>No statements</p>}
        toolbar={{ searchFields: [(row: Row) => row.query] }}
      />,
    )
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
