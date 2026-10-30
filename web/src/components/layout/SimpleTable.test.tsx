import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

import { SimpleTable, type SimpleColumn } from './SimpleTable'

interface Row {
  name: string
  kind: 'table' | 'index'
  size: number | null
}

const ROWS: Row[] = seq(25).map((n, index) => ({
  name: `rel_${n}`,
  kind: index % 5 === 0 ? 'index' : 'table',
  size: index === 3 ? null : (index + 1) * 10,
}))

const COLUMNS: SimpleColumn<Row>[] = [
  {
    id: 'name',
    header: 'Name',
    rowHeader: true,
    sortValue: (row) => row.name,
    cell: (row) => row.name,
  },
  { id: 'kind', header: 'Kind', cell: (row) => row.kind },
  { id: 'size', header: 'Size', sortValue: (row) => row.size, cell: (row) => row.size ?? '—' },
]
const TOOLBAR = {
  searchFields: [(row: Row) => row.name],
  filters: [
    { id: 'kind', label: 'Kind', kind: 'select' as const, accessor: (row: Row) => row.kind },
  ],
}

function renderTable(route = '/', rows: Row[] = ROWS) {
  return renderWithProviders(
    <SimpleTable
      ariaLabel="Relations"
      columns={COLUMNS}
      detailLabel={(row, open) => `${open ? 'Hide' : 'Show'} details of ${row.name}`}
      emptyState={<p>No relations</p>}
      renderDetail={(row) => <p>Detail of {row.name}</p>}
      rowKey={(row) => row.name}
      rows={rows}
      scope="relations"
      tableId="rel"
      toolbar={TOOLBAR}
    />,
    { route },
  )
}

// Body rows without the detail rows.
function names() {
  return dataRows('Relations')
    .map((row) => within(row).queryByRole('rowheader')?.textContent)
    .filter((name): name is string => name !== undefined)
}

describe('SimpleTable', () => {
  it('SIMPLE-001 renders the toolbar, 20 rows, the pager and the range footer', () => {
    renderTable()

    expect(screen.getByRole('searchbox', { name: 'Search Relations' })).toBeInTheDocument()
    expect(names()).toHaveLength(20)
    expect(screen.getByText('Page 1 of 2')).toBeVisible()
    expect(screen.getByText('Showing 1–20 of 25 relations.')).toBeVisible()
    searchTable('Relations', 'rel_2')
    expect(names()).toEqual(['rel_20', 'rel_21', 'rel_22', 'rel_23', 'rel_24', 'rel_25'])
    expect(screen.getByText('Showing 1–6 of 6 relations (filtered from 25).')).toBeVisible()
    searchTable('Relations', '')
    filterTable('Relations', 'Kind', 'index')
    expect(names()).toEqual(['rel_01', 'rel_06', 'rel_11', 'rel_16', 'rel_21'])
    searchTable('Relations', 'nothing')
    expect(screen.getByRole('status')).toHaveTextContent('No rows match')
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(names()).toHaveLength(20)
  })

  it('SIMPLE-002 sorts with the sort buttons, keeps aria-sort and the missing values last', () => {
    const { router } = renderTable()
    const size = screen.getByRole('columnheader', { name: /Size/ })
    expect(screen.getByRole('columnheader', { name: 'Kind' })).toHaveAttribute('aria-sort', 'none')
    expect(
      within(screen.getByRole('columnheader', { name: 'Kind' })).queryByRole('button'),
    ).toBeNull()

    sortTable('Relations', 'Size', 'descending')
    expect(size).toHaveAttribute('aria-sort', 'descending')
    expect(names()[0]).toBe('rel_25')
    expect(router.state.location.search).toBe('?rel.sort=size&rel.dir=desc')
    sortTable('Relations', 'Size', 'ascending')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(names().at(-1)).toBe('rel_04')
    sortTable('Relations', 'Size', 'ascending')
    expect(size).toHaveAttribute('aria-sort', 'none')
  })

  it('SIMPLE-003 an expanded row keeps its detail through a re-sort', () => {
    renderTable()

    fireEvent.click(screen.getByRole('button', { name: 'Show details of rel_02' }))
    expect(screen.getByText('Detail of rel_02')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Hide details of rel_02' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    sortTable('Relations', 'Name', 'descending')
    sortTable('Relations', 'Name', 'ascending')
    // rel_02 is still on page 1: its detail follows it, directly under its row.
    const rows = dataRows('Relations')
    const index = rows.findIndex(
      (row) => within(row).queryByRole('rowheader')?.textContent === 'rel_02',
    )
    expect(rows[index + 1]).toHaveTextContent('Detail of rel_02')
    fireEvent.click(screen.getByRole('button', { name: 'Hide details of rel_02' }))
    expect(screen.queryByText('Detail of rel_02')).toBeNull()
  })

  it('SIMPLE-004 a page change collapses the expanded rows', () => {
    renderTable()

    fireEvent.click(screen.getByRole('button', { name: 'Show details of rel_01' }))
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }))
    expect(screen.queryByText('Detail of rel_01')).toBeNull()
    expect(screen.getByRole('button', { name: 'Show details of rel_01' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
  })

  it('shows the empty state only without rows, and has no serious accessibility violations', async () => {
    renderTable('/', [])
    expect(screen.getByRole('status')).toHaveTextContent('No relations')
    expect(screen.queryByRole('searchbox')).toBeNull()

    vi.useRealTimers()
    const { container } = renderTable('/?rel.sort=size&rel.q=rel')
    fireEvent.click(screen.getByRole('button', { name: 'Show details of rel_01' }))
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
