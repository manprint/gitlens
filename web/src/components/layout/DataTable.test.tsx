import { fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { FilterSpec } from '@/lib/tableControls'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

import { DataTable, type DataTableToolbar } from './DataTable'

interface Row {
  name: string
  kind: 'table' | 'index'
  size: number | null
}

const columns = [
  { accessorKey: 'name', header: 'Name' },
  { accessorKey: 'kind', header: 'Kind' },
  { accessorKey: 'size', header: 'Size' },
]

// 45 rows: row-01 … row-45, every third an index, sizes 10 … 450 except
// row-07 without a size.
const ROWS: Row[] = Array.from({ length: 45 }, (_, index) => ({
  name: `row-${String(index + 1).padStart(2, '0')}`,
  kind: index % 3 === 0 ? 'index' : 'table',
  size: index === 6 ? null : (index + 1) * 10,
}))

const FILTERS: FilterSpec<Row>[] = [
  { id: 'kind', label: 'Kind', kind: 'select', accessor: (row) => row.kind },
]
const TOOLBAR: DataTableToolbar<Row> = {
  searchFields: [(row) => row.name],
  filters: FILTERS,
}

function bodyRows() {
  return within(screen.getByTestId('data-table-rows')).getAllByRole('row')
}

function firstName() {
  return within(bodyRows()[0]!).getAllByRole('cell')[0]?.textContent
}

function renderTable(route = '/', extra: Partial<Parameters<typeof DataTable<Row>>[0]> = {}) {
  return renderWithProviders(
    <DataTable
      ariaLabel="Relations"
      columns={columns}
      data={ROWS}
      emptyState={<p>No relations</p>}
      tableId="rel"
      toolbar={TOOLBAR}
      {...extra}
    />,
    { route },
  )
}

describe('DataTable pagination, sorting and filters', () => {
  it('TBL-UI-001 renders 20 of 45 rows with a pager and the footer range', () => {
    renderTable()

    expect(bodyRows()).toHaveLength(20)
    expect(screen.getByText('Page 1 of 3')).toBeVisible()
    expect(screen.getByText('Showing 1–20 of 45 rows.')).toBeVisible()
    expect(screen.getByRole('navigation', { name: 'Relations pagination' })).toBeInTheDocument()
  })

  it('TBL-UI-002 the next page shows rows 21–40 and writes the page to the URL', () => {
    const { router } = renderTable()

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))

    expect(firstName()).toBe('row-21')
    expect(bodyRows()).toHaveLength(20)
    expect(screen.getByText('Showing 21–40 of 45 rows.')).toBeVisible()
    expect(router.state.location.search).toBe('?rel.page=2')
  })

  it('TBL-UI-003 sort buttons sort the whole set across pages and update aria-sort', () => {
    const { router } = renderTable('/?rel.page=2')
    const sizeHeader = screen.getByRole('columnheader', { name: /Size/ })

    fireEvent.click(within(sizeHeader).getByRole('button', { name: 'Sort Size descending' }))

    expect(sizeHeader).toHaveAttribute('aria-sort', 'descending')
    // Sorting returns to page 1, whose first row is the largest of all 45.
    expect(firstName()).toBe('row-45')
    expect(router.state.location.search).toBe('?rel.sort=size&rel.dir=desc')

    fireEvent.click(within(sizeHeader).getByRole('button', { name: 'Sort Size ascending' }))
    expect(firstName()).toBe('row-01')
    fireEvent.click(screen.getByRole('button', { name: 'Last page' }))
    // The row without a size sorts last.
    const rows = bodyRows()
    expect(within(rows[rows.length - 1]!).getAllByRole('cell')[0]).toHaveTextContent('row-07')

    fireEvent.click(within(sizeHeader).getByRole('button', { name: 'Sort Size ascending' }))
    expect(sizeHeader).toHaveAttribute('aria-sort', 'none')
    expect(router.state.location.search).toBe('')
  })

  it('TBL-UI-004 search narrows the rows, returns to page 1 and states the unfiltered count', () => {
    const { router } = renderTable('/?rel.page=2')

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Relations' }), {
      target: { value: 'row-1' },
    })

    expect(bodyRows()).toHaveLength(10)
    expect(screen.getByText('Showing 1–10 of 10 rows (filtered from 45).')).toBeVisible()
    expect(router.state.location.search).toBe('?rel.q=row-1')

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Relations by Kind' }), {
      target: { value: 'index' },
    })
    expect(bodyRows().map((row) => within(row).getAllByRole('cell')[0]?.textContent)).toEqual([
      'row-10',
      'row-13',
      'row-16',
      'row-19',
    ])
  })

  it('TBL-UI-005 zero matches show "No rows match" and Clear filters restores every row', () => {
    renderTable('/?rel.q=nothing-like-this&rel.f.kind=table')

    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.getByRole('status')).toHaveTextContent('No rows match')
    expect(screen.queryByText('No relations')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))

    expect(bodyRows()).toHaveLength(20)
    expect(screen.getByText('Showing 1–20 of 45 rows.')).toBeVisible()
  })

  it('TBL-UI-006 a page size of 50 shows all 45 rows without page buttons', () => {
    const { router } = renderTable()

    fireEvent.change(screen.getByRole('combobox', { name: 'Rows per page' }), {
      target: { value: '50' },
    })

    expect(bodyRows()).toHaveLength(45)
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull()
    expect(router.state.location.search).toBe('?rel.size=50')
  })

  it('TBL-UI-007 has no serious accessibility violations with the toolbar and pager', async () => {
    vi.useRealTimers()
    const { container } = renderTable('/?rel.sort=name&rel.dir=desc&rel.q=row')

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('starts from the default sort without writing it to the URL', () => {
    const { router } = renderTable('/', { defaultSort: { id: 'size', desc: true } })

    expect(firstName()).toBe('row-45')
    expect(screen.getByRole('columnheader', { name: /Size/ })).toHaveAttribute(
      'aria-sort',
      'descending',
    )
    expect(router.state.location.search).toBe('')
  })

  it('ignores a sort column that does not exist', () => {
    renderTable('/?rel.sort=bogus&rel.dir=desc')
    expect(firstName()).toBe('row-01')
  })
})

describe('DataTable state holders', () => {
  it('TSTATE-005 works outside a Router without tableId and writes <id>.page with one', () => {
    const { unmount } = render(
      <DataTable columns={columns} data={ROWS} emptyState={<p>No rows</p>} />,
    )

    expect(bodyRows()).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(firstName()).toBe('row-21')
    unmount()

    const { router } = renderWithProviders(
      <DataTable columns={columns} data={ROWS} emptyState={<p>No rows</p>} tableId="rel" />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(router.state.location.search).toBe('?rel.page=2')
  })

  it('sorts only sortable columns and counts only filters it applies', () => {
    const fixed = [
      { accessorKey: 'name', header: 'Name' },
      { accessorKey: 'size', header: 'Size', enableSorting: false },
    ]
    renderWithProviders(
      <DataTable
        ariaLabel="Relations"
        columns={fixed}
        data={ROWS}
        emptyState={<p>No rows</p>}
        tableId="rel"
        toolbar={TOOLBAR}
      />,
      { route: '/?rel.sort=size&rel.dir=desc&rel.f.bogus=x&rel.f.kind=' },
    )

    expect(firstName()).toBe('row-01')
    expect(screen.queryByText(/filters? active/)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Clear filters' })).toBeNull()
  })

  it('caps the card height only above the default page size', () => {
    renderTable('/?rel.size=50')
    expect(screen.getByTestId('data-table-scroll')).toHaveClass('max-h-[80vh]')
  })

  it('does not cap the card height at 20 rows', () => {
    renderTable()
    expect(screen.getByTestId('data-table-scroll')).not.toHaveClass('max-h-[80vh]')
  })

  it('TSTATE-006 toggling tableId on the same element keeps the hooks valid', () => {
    function Toggle() {
      const [withId, setWithId] = useState(false)
      return (
        <>
          <button onClick={() => setWithId((value) => !value)} type="button">
            Toggle id
          </button>
          <DataTable
            columns={columns}
            data={ROWS}
            emptyState={<p>No rows</p>}
            {...(withId ? { tableId: 'rel' } : {})}
          />
        </>
      )
    }
    const { router } = renderWithProviders(<Toggle />, { route: '/?rel.page=3' })
    // Without an id the URL page is not read.
    expect(firstName()).toBe('row-01')

    fireEvent.click(screen.getByRole('button', { name: 'Toggle id' }))
    expect(firstName()).toBe('row-41')
    fireEvent.click(screen.getByRole('button', { name: 'First page' }))
    expect(router.state.location.search).toBe('')

    fireEvent.click(screen.getByRole('button', { name: 'Toggle id' }))
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(firstName()).toBe('row-21')
    expect(router.state.location.search).toBe('')
  })

  it('CLNAME-UI-015 getRowId keys rows by the caller id, so a cell keeps its state across a reorder', () => {
    const noteColumns = [
      { accessorKey: 'name', header: 'Name' },
      {
        id: 'note',
        header: 'Note',
        enableSorting: false,
        cell: ({ row }: { row: { original: Row } }) => (
          <input aria-label={`Note ${row.original.name}`} defaultValue="" />
        ),
      },
    ]
    function Reorder({ keyed }: { keyed: boolean }) {
      const [reversed, setReversed] = useState(false)
      const rows = ROWS.slice(0, 3)
      return (
        <>
          <button onClick={() => setReversed(true)} type="button">
            Reverse
          </button>
          <DataTable
            columns={noteColumns}
            data={reversed ? [...rows].reverse() : rows}
            emptyState={<p>No rows</p>}
            {...(keyed ? { getRowId: (row: Row) => row.name } : {})}
          />
        </>
      )
    }

    const keyed = renderWithProviders(<Reorder keyed />)
    fireEvent.change(screen.getByRole('textbox', { name: 'Note row-01' }), {
      target: { value: 'kept' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Reverse' }))
    expect(screen.getByRole('textbox', { name: 'Note row-01' })).toHaveValue('kept')
    keyed.unmount()

    // By position, the typed value stays on the first line, whichever row is there now.
    renderWithProviders(<Reorder keyed={false} />)
    fireEvent.change(screen.getByRole('textbox', { name: 'Note row-01' }), {
      target: { value: 'moved' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Reverse' }))
    expect(screen.getByRole('textbox', { name: 'Note row-01' })).toHaveValue('')
  })
})
