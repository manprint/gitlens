import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderWithProviders } from '@/test/render'
import { seq } from '@/test/table'

import { DataTable } from './DataTable'
import {
  FULLHD_CELL,
  FULLHD_FIXED,
  FULLHD_HEADER_CELL,
  FULLHD_ROW,
  FULLHD_TABLE,
  FULLHD_TRUNCATE,
} from './fullhdTable'
import { MetricTile } from './MetricTile'
import { PageHeader } from './PageHeader'
import { Section } from './Section'
import { SimpleTable, type SimpleColumn } from './SimpleTable'

interface Row {
  name: string
  note: string
}

const ROWS: Row[] = seq(45).map((n) => ({
  name: `row_${n}`,
  note: `a long free-text note for row ${n} that does not fit one line`,
}))

const COLUMNS = [
  { accessorKey: 'name', header: 'Name', meta: { width: '12rem' } },
  { accessorKey: 'note', header: 'Note', meta: { truncate: true } },
]

const SIMPLE_COLUMNS: SimpleColumn<Row>[] = [
  { id: 'name', header: 'Name', width: '12rem', cell: (row) => row.name },
  { id: 'note', header: 'Note', truncate: (row) => row.note, cell: (row) => row.note },
]

function renderData(route = '/') {
  return renderWithProviders(
    <DataTable
      ariaLabel="Notes"
      columns={COLUMNS}
      data={ROWS}
      emptyState={<p>No notes</p>}
      tableId="notes"
    />,
    { route },
  )
}

function renderSimple(route = '/') {
  return renderWithProviders(
    <SimpleTable
      ariaLabel="Notes"
      columns={SIMPLE_COLUMNS}
      emptyState={<p>No notes</p>}
      rowKey={(row) => row.name}
      rows={ROWS}
      tableId="notes"
    />,
    { route },
  )
}

const classesOf = (value: string) => value.split(/\s+/).filter(Boolean)

describe('Full HD table layout (D26)', () => {
  for (const [kind, render] of [
    ['DataTable', renderData],
    ['SimpleTable', renderSimple],
  ] as const) {
    it(`FHD-001 ${kind} renders a fixed table and applies the column width`, () => {
      render()
      const table = screen.getByRole('table', { name: 'Notes' })
      expect(table).toHaveClass('min-[1600px]:table-fixed')
      const name = screen.getByRole('columnheader', { name: /Name/ })
      expect(name).toHaveClass('min-[1600px]:w-(--col-w)')
      expect(name.style.getPropertyValue('--col-w')).toBe('12rem')
      expect(screen.getByRole('columnheader', { name: /Note/ })).not.toHaveClass(
        'min-[1600px]:w-(--col-w)',
      )
    })

    it(`FHD-002 ${kind} truncates a free-text column with the full text in its title`, () => {
      render()
      const note = screen.getByTitle(ROWS[0]!.note)
      expect(note).toHaveClass('min-[1600px]:truncate')
      expect(note).toHaveTextContent(ROWS[0]!.note)
    })

    it(`FHD-003 ${kind} keeps a sticky header inside the card above 20 rows`, () => {
      render('/?notes.size=50')
      const table = screen.getByRole('table', { name: 'Notes' })
      const head = within(table).getAllByRole('rowgroup')[0]!
      expect(head).toHaveClass('sticky', 'top-0')
    })

    it(`FHD-004 ${kind} rows carry the row-height token`, () => {
      render()
      const table = screen.getByRole('table', { name: 'Notes' })
      const body = within(table).getAllByRole('rowgroup')[1]!
      for (const row of within(body).getAllByRole('row')) {
        expect(row).toHaveClass('min-[1600px]:h-(--table-row-h)')
      }
    })

    it(`FHD-006 ${kind} caps the card height only at 50 and 100 rows`, () => {
      const first = render()
      const box = screen.getByTestId('data-table-scroll')
      expect(classesOf(box.className).some((name) => name.startsWith('max-h-'))).toBe(false)
      first.unmount()
      for (const size of [50, 100]) {
        const view = render(`/?notes.size=${size}`)
        const capped = screen.getByTestId('data-table-scroll')
        expect(capped).toHaveClass('max-h-[80vh]', 'overflow-auto')
        view.unmount()
      }
    })
  }

  it('FHD-005 every density class applies from 1600 px only (I-7)', () => {
    const added = [
      FULLHD_FIXED,
      FULLHD_ROW,
      FULLHD_CELL,
      FULLHD_HEADER_CELL,
      FULLHD_TRUNCATE,
    ].flatMap(classesOf)
    expect(added.length).toBeGreaterThan(0)
    for (const name of added) expect(name).toMatch(/^min-\[1600px\]:/)
    // The base table classes are the ones the tables had before 15.1.
    expect(classesOf(FULLHD_TABLE)).toEqual(['w-full', 'border-collapse', 'text-sm'])
  })

  it('FHD-001 a table without width or truncation hints keeps the automatic layout', () => {
    renderWithProviders(
      <DataTable
        ariaLabel="Plain"
        columns={[{ accessorKey: 'name', header: 'Name' }]}
        data={ROWS}
        emptyState={<p>No rows</p>}
      />,
    )
    expect(screen.getByRole('table', { name: 'Plain' })).not.toHaveClass('min-[1600px]:table-fixed')
  })

  it('FHD-001 a SimpleTable without width or truncation hints keeps the automatic layout', () => {
    renderWithProviders(
      <SimpleTable
        ariaLabel="Plain simple"
        columns={[{ id: 'name', header: 'Name', cell: (row: Row) => row.name }]}
        emptyState={<p>No rows</p>}
        rowKey={(row) => row.name}
        rows={ROWS}
        tableId="plain"
      />,
    )
    expect(screen.getByRole('table', { name: 'Plain simple' })).not.toHaveClass(
      'min-[1600px]:table-fixed',
    )
  })

  it('FHD-APPLY-006 the page header puts eyebrow, title and subtitle on one line', () => {
    const subtitle = 'A long page explanation that ends with an ellipsis from 1600 px.'
    renderWithProviders(<PageHeader eyebrow="Checkup" subtitle={subtitle} title="Findings" />)
    const title = screen.getByRole('heading', { level: 1, name: 'Findings' })
    expect(title).toHaveClass('min-[1600px]:text-xl', 'min-[1600px]:shrink-0')
    expect(screen.getByText('Checkup')).toHaveClass('min-[1600px]:mb-0')
    expect(screen.getByTitle(subtitle)).toHaveClass('min-[1600px]:truncate')
  })

  it('FHD-APPLY-007 a section puts its title and description on one line', () => {
    const description = 'What the section lists, cut to one line from 1600 px.'
    renderWithProviders(
      <Section description={description} title="Rules">
        <p>body</p>
      </Section>,
    )
    const title = screen.getByRole('heading', { level: 2, name: 'Rules' })
    expect(title).toHaveClass('min-[1600px]:shrink-0')
    expect(screen.getByTitle(description)).toHaveClass('min-[1600px]:mt-0', 'min-[1600px]:truncate')
  })

  it('FHD-APPLY-008 page controls share the table toolbar line with the search box', () => {
    renderWithProviders(
      <DataTable
        ariaLabel="Notes"
        columns={COLUMNS}
        data={ROWS}
        emptyState={<p>No notes</p>}
        tableId="notes"
        toolbar={{ searchFields: [(row: Row) => row.name] }}
        toolbarExtra={<label htmlFor="extra-control">Server</label>}
      />,
    )
    expect(
      screen.getByRole('search').compareDocumentPosition(screen.getByText('Server')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(
      screen
        .getByText('Server')
        .compareDocumentPosition(screen.getByRole('table', { name: 'Notes' })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('FHD-APPLY-009 a metric tile shows its label and value on one line', () => {
    renderWithProviders(<MetricTile label="Calls" value="42" />)
    expect(screen.getByRole('article', { name: 'Calls' })).toHaveClass(
      'min-[1600px]:flex',
      'min-[1600px]:justify-between',
    )
  })
})
