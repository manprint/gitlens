import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'

import IndexAnalysisPage from './IndexAnalysisPage'

function index(table: string, name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: table,
    indexrelname: name,
    index_bytes: 1_000_000,
    idx_scan_rate: 1,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

function stats(table: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: table,
    n_live_tup: 1000,
    n_dead_tup: 0,
    total_bytes: 10_000_000,
    table_bytes: 2_000_000,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

const ITEMS = [
  index('orders', 'orders_pkey', { is_primary: true, is_unique: true, index_bytes: 500_000 }),
  index('orders', 'orders_old_idx', {
    last_scan_status: 'never_since_reset',
    index_bytes: 3_000_000,
  }),
  index('events', 'events_idx', { index_bytes: 200_000 }),
]
const TABLES = [
  stats('orders', {
    n_tup_ins_rate: 2,
    n_tup_upd_rate: 10,
    n_tup_hot_upd_rate: 8,
    n_tup_del_rate: 1,
  }),
]

function render(
  items: unknown[] = ITEMS,
  tables: unknown[] = TABLES,
  route = '/indexes?tab=tax',
  truncated = false,
) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items,
      truncated,
      relations_not_reported: truncated ? 1 : null,
    }),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: tables,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', []),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

describe('IndexTaxPanel', () => {
  it('TAXPANEL-001 shows the estimate callout and one row per table', async () => {
    render()
    await flushNetwork(30)

    expect(screen.getByRole('tab', { name: 'Index tax' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByText(/Every figure on this tab is an estimate/).length).toBeGreaterThan(0)
    // Largest index size first: orders (3.5 MB) before events (0.2 MB).
    expect(columnTexts('Index tax by table', 0)).toEqual([
      'app · public.orders',
      'app · public.events',
    ])
    const [orders, events] = dataRows('Index tax by table')
    const cells = (row: HTMLElement | undefined) =>
      within(row!)
        .getAllByRole('cell')
        .map((cell) => cell.textContent)
    // Table, Indexes, Index size, Table size, Index/table %, Unused size, Writes/s, Amplification.
    expect(cells(orders)).toEqual(
      expect.arrayContaining([
        '2',
        '3.3 MiB',
        '1.9 MiB',
        '175.0%',
        '2.9 MiB incl. raw counters',
        '5/s',
        '10/s',
      ]),
    )
    // No table statistics: no ratio, no amplification, no NaN.
    expect(cells(events)).toEqual(expect.arrayContaining(['Not measured', '—']))
    expect(screen.queryByText(/NaN|Infinity/)).not.toBeInTheDocument()
  })

  it('TAXPANEL-002 the table is sortable, searchable and paged', async () => {
    const many = seq(25).map((n, position) =>
      index(`t${n}`, `t${n}_idx`, { index_bytes: (position + 1) * 1000 }),
    )
    render(many, [])
    await flushNetwork(30)

    expect(screen.getByRole('searchbox', { name: 'Search Index tax by table' })).toBeInTheDocument()
    expect(dataRows('Index tax by table')).toHaveLength(20)
    expect(screen.getByRole('button', { name: /next page/i })).toBeInTheDocument()
    sortTable('Index tax by table', 'Indexes', 'ascending')
    searchTable('Index tax by table', 't07')
    expect(dataRows('Index tax by table')).toHaveLength(1)
  })

  it('TAXPANEL-003 links each table to the Cleanup tab narrowed to that table', async () => {
    render()
    await flushNetwork(30)

    const link = screen.getByRole('link', { name: 'Show cleanup candidates of public.orders' })
    const url = new URL(link.getAttribute('href') ?? '', 'http://localhost')
    expect(url.pathname).toBe('/indexes')
    expect(url.searchParams.get('tab')).toBe('cleanup')
    expect(url.searchParams.get('cleanup.q')).toBe('public.orders')
    expect(url.searchParams.get('instance')).toBe(INSTANCE_ID)
  })

  it('TAXPANEL-004 says so when the agent reported no index', async () => {
    render([])
    await flushNetwork(30)

    expect(screen.getByText('No indexes to show')).toBeInTheDocument()
  })

  it('TAXPANEL-006 labels the figures as covering the reported indexes only when the list is cut', async () => {
    render(ITEMS, TABLES, '/indexes?tab=tax', true)
    await flushNetwork(30)

    expect(screen.getByText('The list is incomplete.')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Index size' })).toHaveTextContent(
      'indexes reported',
    )
  })

  it('TAXPANEL-007 shows no incomplete note when every index is reported', async () => {
    render()
    await flushNetwork(30)

    expect(screen.queryByText('The list is incomplete.')).not.toBeInTheDocument()
  })

  it('TAXPANEL-008 says how many tables the write amplification covers, and never shows 0 for unknown', async () => {
    render()
    await flushNetwork(30)
    expect(screen.getByRole('article', { name: 'Write amplification' })).toHaveTextContent(
      '1 of 2 tables measured',
    )
  })

  it('TAXPANEL-009 shows Not measured when no table has write rates', async () => {
    render(ITEMS, [])
    await flushNetwork(30)
    const tile = screen.getByRole('article', { name: 'Write amplification' })
    expect(tile).toHaveTextContent('Not measured')
    expect(tile).toHaveTextContent('0 of 2 tables measured')
  })

  it('TAXPANEL-005 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('table', { name: 'Index tax by table' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
