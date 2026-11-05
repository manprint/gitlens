import { act, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { base, empty } from '@/test/fixtures/getIndexSuggestions'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'

const NAME = 'Index suggestions'
const FIRST = base.suggestions[0]!

function suggestion(name: string, extra: Record<string, unknown> = {}) {
  return {
    ...FIRST,
    id: `id-${name}`,
    table: name,
    columns: [{ name: `${name}_col`, desc: false }],
    create_statement: `CREATE INDEX CONCURRENTLY idx_${name} ON public.${name} (${name}_col);`,
    statements: [
      { queryid: 11, share_pct: 100, query_text: `SELECT id FROM ${name} WHERE ${name}_col = $1` },
    ],
    ...extra,
  }
}

function render(response: unknown = base, route = '/indexes?tab=suggestions') {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', []),
    ok('getIndexSuggestions', response),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

function stubClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('SuggestionsPanel', () => {
  it('SUGG-UI-001 says the suggestions are estimates and never created by pglens', async () => {
    render()
    await flushNetwork(30)

    const callout = screen
      .getAllByRole('status')
      .find((status) => status.textContent?.includes('pglens never creates an index'))
    expect(callout).toBeDefined()
    expect(callout).toHaveTextContent(
      'Suggestions are estimates from statement statistics; pglens never creates an index.',
    )
  })

  it('SUGG-UI-002 renders a row with a ~ size and the upper-bound label', async () => {
    render()
    await flushNetwork(30)

    const row = within(dataRows(NAME)[0]!)
    expect(row.getByText('res_partner')).toBeInTheDocument()
    expect(row.getByText('ref')).toBeInTheDocument()
    expect(row.getByText('40%')).toBeInTheDocument()
    expect(row.getByText('upper bound')).toBeInTheDocument()
    expect(row.getByText('~1.3 MiB')).toBeInTheDocument()
    expect(row.getByText('0.5/s · 1/s · 0.1/s')).toBeInTheDocument()
  })

  it('SUGG-UI-003 sorts by weight and by estimated size', async () => {
    const rows = [
      suggestion('aaa', { weight_pct: 10, est_size_bytes: 3_000_000 }),
      suggestion('bbb', { weight_pct: 30, est_size_bytes: 1_000_000 }),
      suggestion('ccc', { weight_pct: 20, est_size_bytes: null }),
    ]
    render({ ...base, suggestions: rows })
    await flushNetwork(30)

    // Weight, heaviest first, is the default.
    expect(columnTexts(NAME, 1).map((text) => text.slice(-3))).toEqual(['bbb', 'ccc', 'aaa'])
    sortTable(NAME, 'Est. size', 'ascending')
    expect(columnTexts(NAME, 1).map((text) => text.slice(-3))).toEqual(['bbb', 'aaa', 'ccc'])
    sortTable(NAME, 'Weight %', 'ascending')
    expect(columnTexts(NAME, 1).map((text) => text.slice(-3))).toEqual(['aaa', 'ccc', 'bbb'])
  })

  it('SUGG-UI-004 searches the table, the key columns and the statement text', async () => {
    const rows = [
      suggestion('orders', {
        statements: [{ queryid: 5, share_pct: 100, query_text: 'SELECT 1 WHERE flag_xyz = $1' }],
      }),
      suggestion('events'),
      suggestion('users'),
    ]
    render({ ...base, suggestions: rows })
    await flushNetwork(30)

    searchTable(NAME, 'events', 'Search suggestions')
    expect(dataRows(NAME)).toHaveLength(1)
    searchTable(NAME, 'users_col', 'Search suggestions')
    expect(dataRows(NAME)).toHaveLength(1)
    searchTable(NAME, 'flag_xyz', 'Search suggestions')
    expect(dataRows(NAME)).toHaveLength(1)
    expect(dataRows(NAME)[0]).toHaveTextContent('orders')
    searchTable(NAME, '', 'Search suggestions')
    expect(dataRows(NAME)).toHaveLength(3)
  })

  it('SUGG-UI-005 pages the table by 20 rows', async () => {
    const rows = seq(25).map((n, index) => suggestion(`t_${n}`, { weight_pct: 90 - index }))
    render({ ...base, suggestions: rows })
    await flushNetwork(30)

    expect(dataRows(NAME)).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(NAME)).toHaveLength(5)
  })

  it('SUGG-UI-006 expands a row to its statements, notes and the CREATE INDEX statement', async () => {
    const rows = [
      suggestion('orders', {
        overlapping_indexes: ['orders_ref_hash'],
        notes: ['width_assumed'],
      }),
    ]
    render({ ...base, suggestions: rows })
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: /Show details of orders/ }))
    expect(
      screen.getAllByText(/CREATE INDEX CONCURRENTLY idx_orders ON public.orders/).length,
    ).toBeGreaterThan(0)
    expect(screen.getByText('SELECT id FROM orders WHERE orders_col = $1')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '11' })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries/11`,
    )
    expect(screen.getByText('orders_ref_hash')).toBeInTheDocument()
    expect(screen.getByText(/32 bytes were assumed/)).toBeInTheDocument()
  })

  it('SUGG-UI-007 copies the statement and announces Copied', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    stubClipboard(writeText)
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: /Show details of res_partner/ }))
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy index statement' }))
      await Promise.resolve()
    })
    expect(writeText).toHaveBeenCalledWith(FIRST.create_statement)
    expect(screen.getByRole('button', { name: 'Index statement copied' })).toHaveTextContent(
      'Copied',
    )
  })

  it('SUGG-UI-008 tells no suggestions from statistics not collected', async () => {
    const { unmount } = render({ ...base, suggestions: [] })
    await flushNetwork(30)
    expect(screen.getByRole('heading', { name: 'No suggestions found' })).toBeInTheDocument()
    unmount()

    render(empty)
    await flushNetwork(30)
    expect(
      screen.getByRole('heading', { name: 'Statement statistics not collected' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /readiness/ })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}`,
    )
  })

  it('SUGG-UI-009 states how many statements were analysed and why the rest were skipped', async () => {
    render()
    await flushNetwork(30)

    expect(screen.getByTestId('suggestion-coverage')).toHaveTextContent(
      'Analysed 2 of 3 statements (1 skipped: 1 no stored text).',
    )
  })

  it('SUGG-UI-010 names the verdict in words, not only by colour', async () => {
    const rows = [
      suggestion('aaa', { verdict: 'write_heavy', weight_pct: 5 }),
      suggestion('bbb', { verdict: 'strong', weight_pct: 50 }),
    ]
    render({ ...base, suggestions: rows })
    await flushNetwork(30)

    const [first, second] = dataRows(NAME)
    expect(first).toHaveTextContent('Strong')
    expect(second).toHaveTextContent('Write-heavy')
  })

  it('SUGG-UI-011 has no accessibility violation', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('table', { name: NAME })
    fireEvent.click(screen.getByRole('button', { name: /Show details of res_partner/ }))

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('SUGG-UI-012 opens from ?tab=suggestions and keeps the window in the URL', async () => {
    render(base, '/indexes?tab=suggestions&window=6h')
    await flushNetwork(30)

    expect(screen.getByRole('tab', { name: 'Suggestions' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('combobox', { name: 'Analysis window' })).toHaveValue('6h')
    for (const tab of ['Indexes', 'Cleanup', 'Dead rows']) {
      expect(screen.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'false')
    }
  })
})
