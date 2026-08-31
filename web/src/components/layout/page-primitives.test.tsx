import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DataTable } from './DataTable'
import { MetricTile } from './MetricTile'
import { PageHeader } from './PageHeader'
import { Section } from './Section'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

interface TestRow {
  name: string
  value: number
}

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'value', header: 'Value' },
]

const rows: TestRow[] = [
  { name: 'Bravo', value: 2 },
  { name: 'Alpha', value: 1 },
]

describe('page scaffolding primitives', () => {
  it('UI-LAYOUT-001 renders Unknown for an absent metric and never substitutes zero', () => {
    renderWithProviders(<MetricTile label="Latency" value={null} />)

    const tile = screen.getByRole('article', { name: 'Latency' })
    expect(within(tile).getByLabelText('not measured')).toBeInTheDocument()
    expect(tile).not.toHaveTextContent('0')
  })

  it('UI-LAYOUT-002 keeps the unit adjacent to the metric value', () => {
    renderWithProviders(<MetricTile label="Latency" value={42} unit="ms" />)

    const tile = screen.getByRole('article', { name: 'Latency' })
    expect(tile).toHaveTextContent('42ms')
    expect(within(tile).getByText('ms')).toBeInTheDocument()
  })

  it('renders page headers and titled sections with their optional content', () => {
    renderWithProviders(
      <>
        <PageHeader
          title="Fleet"
          subtitle="Current inventory"
          freshness={<span>Fresh</span>}
          actions={<button>Refresh</button>}
        />
        <Section title="Overview" description="At a glance">
          <p>Content</p>
        </Section>
      </>,
    )

    expect(screen.getByRole('heading', { name: 'Fleet' })).toBeInTheDocument()
    expect(screen.getByText('Current inventory')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Overview' })).toBeInTheDocument()
    expect(screen.getByText('At a glance')).toBeInTheDocument()
  })

  it('UI-LAYOUT-003 sorts by a column and exposes aria-sort', () => {
    renderWithProviders(<DataTable columns={columns} data={rows} emptyState={<p>No rows</p>} />)

    const nameHeader = screen.getByRole('columnheader', { name: /Name/ })
    expect(nameHeader).toHaveAttribute('aria-sort', 'none')

    fireEvent.click(within(nameHeader).getByRole('button', { name: /Name/ }))

    expect(nameHeader).toHaveAttribute('aria-sort', 'ascending')
    const tableRows = within(screen.getByTestId('data-table-rows')).getAllByRole('row')
    expect(tableRows[0]).toHaveTextContent('Alpha')
  })

  it('UI-LAYOUT-004 requires and renders an explicit empty state', () => {
    renderWithProviders(<DataTable columns={columns} data={[]} emptyState={<p>No findings</p>} />)

    expect(screen.getByRole('status')).toHaveTextContent('No findings')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
  })

  it('UI-LAYOUT-005 renders Truncated when a result budget is reported', () => {
    renderWithProviders(
      <DataTable
        columns={columns}
        data={rows}
        total={5}
        budget={2}
        scope="findings"
        emptyState={<p>No findings</p>}
      />,
    )

    expect(screen.getByText(/Showing 2 of 2 findings/)).toHaveTextContent('truncated; budget 2')
  })

  it('UI-LAYOUT-006 virtualizes tables above 200 rows', () => {
    const manyRows = Array.from({ length: 250 }, (_, index) => ({
      name: `Row ${index}`,
      value: index,
    }))
    renderWithProviders(<DataTable columns={columns} data={manyRows} emptyState={<p>No rows</p>} />)

    const renderedRows = within(screen.getByTestId('data-table-rows')).getAllByRole('row')
    expect(renderedRows.length).toBeLessThan(50)
    expect(renderedRows.length).toBeLessThan(manyRows.length)
  })

  it('has no serious or critical accessibility violations for a populated table', async () => {
    vi.useRealTimers()
    const { container } = renderWithProviders(
      <DataTable columns={columns} data={rows} emptyState={<p>No rows</p>} />,
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
