import { describe, expect, it } from 'vitest'

import {
  applyFilters,
  compareValues,
  DEFAULT_PAGE_SIZE,
  filterOptions,
  matchesSearch,
  PAGE_SIZES,
  paginate,
  sortRows,
  type FilterSpec,
} from './tableControls'

interface Row {
  name: string
  kind: string | null
  size: number | null
  valid: boolean
}

const ROWS: Row[] = [
  { name: 'orders', kind: 'table', size: 300, valid: true },
  { name: 'Order_lines', kind: 'table', size: 50, valid: false },
  { name: 'orders_pkey', kind: 'index', size: null, valid: true },
  { name: 'partners', kind: 'view', size: 1_000, valid: true },
]

describe('compareValues', () => {
  it('TCTRL-001 orders numbers, strings and booleans and keeps null and NaN last in both directions', () => {
    expect([3, 1, 2].sort((a, b) => compareValues(a, b, 'asc'))).toEqual([1, 2, 3])
    expect([3, 1, 2].sort((a, b) => compareValues(a, b, 'desc'))).toEqual([3, 2, 1])
    expect([10, 9].sort((a, b) => compareValues(a, b, 'asc'))).toEqual([9, 10])
    expect(['b', 'A', 'a'].sort((a, b) => compareValues(a, b, 'asc'))[0]).toMatch(/a/i)
    expect([true, false].sort((a, b) => compareValues(a, b, 'asc'))).toEqual([false, true])
    for (const dir of ['asc', 'desc'] as const) {
      const sorted = [null, 2, Number.NaN, undefined, 1].sort((a, b) => compareValues(a, b, dir))
      expect(sorted.slice(0, 2)).toEqual(dir === 'asc' ? [1, 2] : [2, 1])
      expect(
        sorted
          .slice(2)
          .every((value) => value === null || value === undefined || Number.isNaN(value)),
      ).toBe(true)
    }
    expect(compareValues(null, undefined, 'asc')).toBe(0)
  })

  it('TCTRL-002 sorts strings in natural order, ignoring case', () => {
    expect(['a10', 'a2', 'a1'].sort((a, b) => compareValues(a, b, 'asc'))).toEqual([
      'a1',
      'a2',
      'a10',
    ])
    expect(compareValues('Orders', 'orders', 'asc')).toBe(0)
  })
})

describe('sortRows', () => {
  it('TCTRL-003 sorts by an accessor, keeps ties in their original order and copies the input', () => {
    const accessors = { kind: (row: Row) => row.kind, size: (row: Row) => row.size }
    const byKind = sortRows(ROWS, 'kind', 'asc', accessors)
    expect(byKind.map((row) => row.name)).toEqual([
      'orders_pkey',
      'orders',
      'Order_lines',
      'partners',
    ])
    const byKindDesc = sortRows(ROWS, 'kind', 'desc', accessors)
    // Ties keep the input order in both directions.
    expect(byKindDesc.map((row) => row.name)).toEqual([
      'partners',
      'orders',
      'Order_lines',
      'orders_pkey',
    ])
    expect(sortRows(ROWS, 'size', 'desc', accessors).map((row) => row.size)).toEqual([
      1_000,
      300,
      50,
      null,
    ])
    expect(sortRows(ROWS, null, 'asc', accessors)).toEqual(ROWS)
    expect(sortRows(ROWS, 'unknown', 'asc', accessors)).toEqual(ROWS)
    expect(sortRows(ROWS, 'kind', 'asc', accessors)).not.toBe(ROWS)
  })
})

describe('matchesSearch', () => {
  it('TCTRL-004 matches a trimmed, case-insensitive substring of any field; an empty query matches', () => {
    const fields = [(row: Row) => row.name, (row: Row) => row.kind]
    expect(matchesSearch(ROWS[1]!, '  order_LINES ', fields)).toBe(true)
    expect(matchesSearch(ROWS[3]!, 'VIEW', fields)).toBe(true)
    expect(matchesSearch(ROWS[3]!, 'index', fields)).toBe(false)
    expect(matchesSearch(ROWS[2]!, '', fields)).toBe(true)
    expect(matchesSearch(ROWS[2]!, '   ', fields)).toBe(true)
    expect(matchesSearch({ id: 42 }, '4', [(row) => row.id])).toBe(true)
    expect(matchesSearch({ id: null }, 'null', [(row) => row.id])).toBe(false)
  })
})

describe('applyFilters', () => {
  const specs: FilterSpec<Row>[] = [
    { id: 'kind', label: 'Kind', kind: 'select', accessor: (row) => row.kind },
    { id: 'size', label: 'Size', kind: 'min', accessor: (row) => row.size },
    {
      id: 'valid',
      label: 'State',
      kind: 'multi',
      accessor: (row) => (row.valid ? 'valid' : 'invalid'),
    },
    {
      id: 'big',
      label: 'Big',
      kind: 'select',
      accessor: (row) => row.size,
      predicate: (row, value) => value === 'yes' && (row.size ?? 0) >= 300,
    },
  ]

  it('TCTRL-005 combines select, min, multi and custom predicates; empty values do not filter', () => {
    const names = (values: Record<string, string>) =>
      applyFilters(ROWS, specs, values).map((row) => row.name)
    expect(names({})).toEqual(ROWS.map((row) => row.name))
    expect(names({ kind: 'table' })).toEqual(['orders', 'Order_lines'])
    expect(names({ kind: 'table', size: '100' })).toEqual(['orders'])
    // A min filter drops rows without a value.
    expect(names({ size: '0' })).toEqual(['orders', 'Order_lines', 'partners'])
    expect(names({ size: 'abc' })).toEqual(ROWS.map((row) => row.name))
    expect(names({ valid: 'invalid' })).toEqual(['Order_lines'])
    expect(names({ valid: 'invalid,valid', kind: 'table' })).toEqual(['orders', 'Order_lines'])
    expect(names({ big: 'yes' })).toEqual(['orders', 'partners'])
    expect(names({ kind: '', unknown: 'x' })).toEqual(ROWS.map((row) => row.name))
  })

  it('lists the distinct values of a filter column in natural order', () => {
    expect(filterOptions(ROWS, specs[0]!)).toEqual(['index', 'table', 'view'])
    expect(filterOptions(ROWS, { ...specs[0]!, options: [{ value: 'x', label: 'X' }] })).toEqual([
      'x',
    ])
  })
})

describe('paginate', () => {
  const rows = Array.from({ length: 45 }, (_, index) => index + 1)

  it('TCTRL-006 returns the slice of a page with its 1-based range', () => {
    expect(paginate(rows, 3, 20)).toEqual({
      rows: [41, 42, 43, 44, 45],
      page: 3,
      pageCount: 3,
      from: 41,
      to: 45,
      total: 45,
    })
    expect(PAGE_SIZES).toEqual([20, 50, 100])
    expect(DEFAULT_PAGE_SIZE).toBe(20)
  })

  it('TCTRL-007 clamps an out-of-range page', () => {
    expect(paginate(rows, 99, 20).page).toBe(3)
    expect(paginate(rows, 0, 20).page).toBe(1)
    expect(paginate(rows, Number.NaN, 20).page).toBe(1)
  })

  it('TCTRL-008 reports one empty page for an empty set', () => {
    expect(paginate([], 1, 20)).toEqual({
      rows: [],
      page: 1,
      pageCount: 1,
      from: 0,
      to: 0,
      total: 0,
    })
  })

  it('TPAG-001 concatenating every page gives back the input exactly once (I-12)', () => {
    for (const size of PAGE_SIZES) {
      for (const count of [0, 1, 20, 21, 45, 101]) {
        const input = Array.from({ length: count }, (_, index) => `row-${index}`)
        const { pageCount } = paginate(input, 1, size)
        const pages = Array.from({ length: pageCount }, (_, index) =>
          paginate(input, index + 1, size),
        )
        expect(pages.flatMap((page) => page.rows)).toEqual(input)
        expect(pages.every((page) => page.rows.length <= size)).toBe(true)
      }
    }
  })
})
