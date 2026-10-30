import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { base as clusters } from '@/test/fixtures/getClusters'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'
import { expectNoA11yViolations } from '@/test/a11y'

function index(name: string, definition: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: 8192,
    idx_scan_rate: 0.5,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    last_scan_at: '2026-08-27T01:00:00Z',
    definition,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

const ITEMS = [
  index('orders_pkey', 'CREATE UNIQUE INDEX orders_pkey ON public.orders USING btree (id)', {
    is_primary: true,
    is_unique: true,
    index_bytes: 16384,
  }),
  index(
    'orders_customer_idx',
    'CREATE INDEX orders_customer_idx ON public.orders USING btree (customer_id, created_at DESC) WHERE (customer_id IS NOT NULL)',
  ),
  index('orders_old_idx', 'CREATE INDEX orders_old_idx ON public.orders USING btree (legacy_ref)', {
    last_scan_status: 'never_since_reset',
    last_scan_at: null,
  }),
]

const FINDINGS = [
  {
    finding_id: 'f1',
    rule_id: 'index.unused',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders_old_idx',
    title: 'Unused index',
    detail: 'index was not scanned in the window',
    remediation: 'Confirm workload intent before dropping',
  },
  {
    finding_id: 'f2',
    rule_id: 'index.fk_unindexed',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders.orders_customer_fk',
    title: 'Foreign key orders_customer_fk on public.orders has no index',
    evidence: {
      table: 'public.orders',
      constraint: 'orders_customer_fk',
      columns: 'customer_id',
      referenced: 'public.customers',
    },
  },
]

function render(items: unknown[] = ITEMS, truncated = false) {
  return renderPage(items, truncated)
}

function renderPage(items: unknown[], truncated: boolean) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items,
      truncated,
      relations_not_reported: truncated ? 1 : null,
    }),
    ok('getFindings', FINDINGS),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })
}

describe('IndexAnalysisPage', () => {
  it('IDX-UI-001 lists every index with its columns, method and size', async () => {
    render()
    await flushNetwork(30)

    const table = screen.getByRole('table', { name: 'Indexes' })
    const rows = within(table).getAllByRole('row')
    // The header and one row per index.
    expect(rows).toHaveLength(4)
    expect(within(table).getByText('orders_pkey')).toBeInTheDocument()
    expect(within(table).getByText('customer_id, created_at')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Indexes' })).toHaveTextContent('3')
    expect(screen.getByRole('article', { name: 'Primary keys' })).toHaveTextContent('1')
    expect(screen.getByRole('article', { name: 'Partial' })).toHaveTextContent('1')
  })

  it('IDX-UI-002 shows the advisor findings and the foreign keys that lack an index', async () => {
    render()
    await flushNetwork(30)

    expect(screen.getByText('Advisor findings on indexes')).toBeInTheDocument()
    expect(screen.getByText('index.unused')).toBeInTheDocument()
    expect(screen.getByText('Foreign keys without a covering index')).toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Foreign keys w/o index' })).toHaveTextContent('1')
  })

  it('IDX-UI-003 filters the inventory and searches it', async () => {
    render()
    await flushNetwork(30)

    fireEvent.change(screen.getByRole('combobox', { name: 'Filter Indexes by Kind' }), {
      target: { value: 'partial' },
    })
    await flushNetwork(5)
    let table = screen.getByRole('table', { name: 'Indexes' })
    expect(within(table).getAllByRole('row')).toHaveLength(2)

    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    fireEvent.change(screen.getByLabelText('Search indexes'), { target: { value: 'legacy_ref' } })
    await flushNetwork(5)
    table = screen.getByRole('table', { name: 'Indexes' })
    expect(within(table).getAllByRole('row')).toHaveLength(2)
    expect(within(table).getByText('orders_old_idx')).toBeInTheDocument()
  })

  it('IDX-UI-004 expands an index to its columns, predicate and definition', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Show details of orders_customer_idx' }))
    expect(screen.getByText('customer_id IS NOT NULL')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy index definition' })).toBeInTheDocument()
  })

  it('IDX-UI-005 warns that the list is incomplete when the agent truncated it', async () => {
    render(ITEMS, true)
    await flushNetwork(30)

    expect(screen.getByText('The list is incomplete.')).toBeInTheDocument()
  })

  it('IDX-UI-008 says the unused check is pending while the advisor lacks 7 days of history', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceIndexes', {
        instance_id: INSTANCE_ID,
        items: ITEMS,
        truncated: false,
        relations_not_reported: null,
      }),
      ok('getFindings', [
        {
          finding_id: 'f9',
          rule_id: 'index.unused',
          severity: 'warning',
          state: 'degraded',
          scope: 'relation',
          datname: 'app',
          object_name: 'public.orders_old_idx',
          title: 'Advisor input unavailable',
          degraded_reason: 'index usage history is shorter than 7 days',
        },
      ]),
    )
    renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })
    await flushNetwork(30)

    expect(screen.getByText('Unused-index check pending for 1 index.')).toBeInTheDocument()
  })

  it('IDX-UI-006 says so when no index was reported', async () => {
    render([])
    await flushNetwork(30)

    expect(screen.getByText('No indexes to show')).toBeInTheDocument()
  })

  it('IDX-UI-007 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('table', { name: 'Indexes' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('IndexAnalysisPage table controls', () => {
  it('TBL-UI-100 indexes pages, searches, filters and sorts the inventory', async () => {
    const items = seq(25).map((n, index_) =>
      index(`idx_${n}`, `CREATE INDEX idx_${n} ON public.orders USING btree (c${n})`, {
        index_bytes: (index_ + 1) * 1024,
        is_valid: index_ >= 2,
      }),
    )
    const { router } = render(items)
    await flushNetwork(30)
    const name = 'Indexes'

    expect(dataRows(name)).toHaveLength(20)
    // Largest first by default; the name is the second cell (the first expands the row).
    expect(columnTexts(name, 1)[0]).toBe('idx_25')
    sortTable(name, 'Size', 'ascending')
    expect(columnTexts(name, 1)[0]).toBe('idx_01')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)
    expect(router.state.location.search).toContain('indexes.page=2')
    // Another database is another row set: back to page 1 (D23).
    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), {
      target: { value: 'app' },
    })
    expect(router.state.location.search).not.toContain('indexes.page')
    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), { target: { value: '' } })
    searchTable(name, 'idx_2', 'Search indexes')
    expect(dataRows(name)).toHaveLength(6)
    searchTable(name, '', 'Search indexes')
    filterTable(name, 'State', 'invalid')
    expect(columnTexts(name, 1)).toEqual(['idx_01', 'idx_02'])
  })
})
