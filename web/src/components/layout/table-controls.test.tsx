import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { initialTableState, type TableState } from '@/hooks/useTableState'
import type { FilterSpec } from '@/lib/tableControls'
import { expectNoA11yViolations } from '@/test/a11y'
import { realTimeUser } from '@/test/render'

import { SortButtons } from './SortButtons'
import { TablePagination } from './TablePagination'
import { TableToolbar } from './TableToolbar'

const noop = () => undefined

describe('SortButtons', () => {
  it('SORTBTN-001 names both directions and marks the active one pressed', () => {
    render(<SortButtons label="Calls" direction="desc" onChange={noop} />)

    const ascending = screen.getByRole('button', { name: 'Sort Calls ascending' })
    const descending = screen.getByRole('button', { name: 'Sort Calls descending' })
    expect(ascending).toHaveAttribute('aria-pressed', 'false')
    expect(descending).toHaveAttribute('aria-pressed', 'true')
  })

  it('SORTBTN-002 chooses a direction, and pressing the active one clears the sort', () => {
    const onChange = vi.fn()
    const { rerender } = render(<SortButtons label="Calls" direction={null} onChange={onChange} />)

    fireEvent.click(screen.getByRole('button', { name: 'Sort Calls descending' }))
    expect(onChange).toHaveBeenLastCalledWith('desc')

    rerender(<SortButtons label="Calls" direction="desc" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Sort Calls descending' }))
    expect(onChange).toHaveBeenLastCalledWith(null)
    fireEvent.click(screen.getByRole('button', { name: 'Sort Calls ascending' }))
    expect(onChange).toHaveBeenLastCalledWith('asc')
  })

  it('SORTBTN-003 is reachable and operable from the keyboard', async () => {
    const user = realTimeUser()
    const onChange = vi.fn()
    render(<SortButtons label="Calls" direction={null} onChange={onChange} />)

    await user.tab()
    expect(screen.getByRole('button', { name: 'Sort Calls ascending' })).toHaveFocus()
    await user.keyboard('{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('asc')

    await user.tab()
    expect(screen.getByRole('button', { name: 'Sort Calls descending' })).toHaveFocus()
    await user.keyboard(' ')
    expect(onChange).toHaveBeenLastCalledWith('desc')
    expect(onChange).toHaveBeenCalledTimes(2)
  })
})

interface Row {
  kind: string
  size: number
  state: string
}

const FILTERS: FilterSpec<Row>[] = [
  { id: 'kind', label: 'Kind', kind: 'select', accessor: (row) => row.kind },
  { id: 'state', label: 'State', kind: 'multi', accessor: (row) => row.state },
  { id: 'size', label: 'Size', kind: 'min', accessor: (row) => row.size },
]
const OPTIONS = {
  kind: [
    { value: 'table', label: 'table' },
    { value: 'index', label: 'index' },
  ],
  state: [
    { value: 'valid', label: 'Valid' },
    { value: 'invalid', label: 'Invalid' },
  ],
}

function renderToolbar(state: TableState = initialTableState(), activeFilterCount = 0) {
  const onChange = vi.fn()
  const onClear = vi.fn()
  render(
    <TableToolbar
      activeFilterCount={activeFilterCount}
      filters={FILTERS}
      label="Relations"
      onChange={onChange}
      onClear={onClear}
      options={OPTIONS}
      state={state}
    />,
  )
  return { onChange, onClear }
}

describe('TableToolbar', () => {
  it('renders the search box and one labelled control per filter', () => {
    const { onChange } = renderToolbar()

    const search = within(screen.getByRole('search')).getByRole('searchbox', {
      name: 'Search Relations',
    })
    fireEvent.change(search, { target: { value: 'orders' } })
    expect(onChange).toHaveBeenLastCalledWith({ q: 'orders' })

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Relations by Kind' }), {
      target: { value: 'index' },
    })
    expect(onChange).toHaveBeenLastCalledWith({ filters: { kind: 'index' } })

    const state = screen.getByRole('group', { name: 'Filter Relations by State' })
    fireEvent.click(within(state).getByRole('checkbox', { name: 'Invalid' }))
    expect(onChange).toHaveBeenLastCalledWith({ filters: { state: 'invalid' } })

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Filter Relations by Size' }), {
      target: { value: '100' },
    })
    expect(onChange).toHaveBeenLastCalledWith({ filters: { size: '100' } })
    expect(screen.queryByRole('button', { name: /clear filters/i })).toBeNull()
  })

  it('keeps the other checked values of a multi filter and shows Clear filters when active', () => {
    const state = { ...initialTableState(), q: 'x', filters: { state: 'valid' } }
    const { onChange, onClear } = renderToolbar(state, 2)

    fireEvent.click(screen.getByRole('checkbox', { name: 'Invalid' }))
    expect(onChange).toHaveBeenLastCalledWith({ filters: { state: 'valid,invalid' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Valid' }))
    expect(onChange).toHaveBeenLastCalledWith({ filters: { state: '' } })

    expect(screen.getByText('2 filters active')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(onClear).toHaveBeenCalledOnce()
  })

  it('takes a custom search label', () => {
    render(
      <TableToolbar
        activeFilterCount={0}
        filters={[]}
        label="Dead rows"
        onChange={noop}
        onClear={noop}
        options={{}}
        searchLabel="Search tables"
        state={initialTableState()}
      />,
    )
    expect(screen.getByRole('searchbox', { name: 'Search tables' })).toBeInTheDocument()
  })
})

describe('TablePagination', () => {
  it('moves between pages, disables the ends and changes the page size', () => {
    const onPage = vi.fn()
    const onPageSize = vi.fn()
    const { rerender } = render(
      <TablePagination
        ariaLabel="Relations pagination"
        onPage={onPage}
        onPageSize={onPageSize}
        page={1}
        pageCount={3}
        pageSize={20}
      />,
    )

    const nav = screen.getByRole('navigation', { name: 'Relations pagination' })
    expect(within(nav).getByText('Page 1 of 3')).toBeVisible()
    expect(within(nav).getByRole('button', { name: 'First page' })).toBeDisabled()
    expect(within(nav).getByRole('button', { name: 'Previous page' })).toBeDisabled()
    fireEvent.click(within(nav).getByRole('button', { name: 'Next page' }))
    expect(onPage).toHaveBeenLastCalledWith(2)
    fireEvent.click(within(nav).getByRole('button', { name: 'Last page' }))
    expect(onPage).toHaveBeenLastCalledWith(3)
    fireEvent.change(within(nav).getByRole('combobox', { name: 'Rows per page' }), {
      target: { value: '50' },
    })
    expect(onPageSize).toHaveBeenLastCalledWith(50)

    rerender(
      <TablePagination
        ariaLabel="Relations pagination"
        onPage={onPage}
        onPageSize={onPageSize}
        page={3}
        pageCount={3}
        pageSize={20}
      />,
    )
    expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Last page' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }))
    expect(onPage).toHaveBeenLastCalledWith(2)
  })

  it('hides the page buttons when everything fits on one page', () => {
    render(
      <TablePagination
        ariaLabel="Relations pagination"
        onPage={noop}
        onPageSize={noop}
        page={1}
        pageCount={1}
        pageSize={50}
      />,
    )
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull()
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('50')
  })

  it('has no serious accessibility violations together with the toolbar and sort buttons', async () => {
    vi.useRealTimers()
    const { container } = render(
      <div>
        <TableToolbar
          activeFilterCount={1}
          filters={FILTERS}
          label="Relations"
          onChange={noop}
          onClear={noop}
          options={OPTIONS}
          state={{ ...initialTableState(), q: 'a' }}
        />
        <table>
          <thead>
            <tr>
              <th aria-sort="ascending" scope="col">
                Size <SortButtons direction="asc" label="Size" onChange={noop} />
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>1</td>
            </tr>
          </tbody>
        </table>
        <TablePagination
          ariaLabel="Relations pagination"
          onPage={noop}
          onPageSize={noop}
          page={2}
          pageCount={3}
          pageSize={20}
        />
      </div>,
    )
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
