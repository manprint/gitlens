import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { base as clusters } from '@/test/fixtures/getClusters'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { custom as oneHourWindows } from '@/test/fixtures/getAdvisorWindows'
import { base as indexHistory } from '@/test/fixtures/getIndexHistory'
import { never, ok, status } from '@/test/msw/handlers'
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

vi.mock('echarts-for-react/esm/core', () => ({ default: () => null }))

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
    expect(screen.getByRole('article', { name: 'Indexes reported' })).toHaveTextContent('3')
    // The kind counts are on the Summary view of the tab (15.3).
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Summary' }))
    expect(screen.getByRole('article', { name: 'Primary key indexes' })).toHaveTextContent('1')
    expect(screen.getByRole('article', { name: 'Partial indexes' })).toHaveTextContent('1')
  })

  it('IDX-UI-002 shows the advisor findings and the foreign keys that lack an index', async () => {
    render()
    await flushNetwork(30)

    // Each list is a view of the tab (15.3): findings, foreign keys, then the summary tile.
    fireEvent.mouseDown(screen.getByRole('tab', { name: /^Advisor findings/ }))
    expect(screen.getByRole('heading', { level: 2, name: 'Index findings' })).toBeInTheDocument()
    const findings = screen.getByRole('table', { name: 'Index findings by index' })
    expect(within(findings).getByText('index.unused')).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('tab', { name: /^Foreign keys/ }))
    expect(
      screen.getByRole('heading', { level: 2, name: 'Unindexed foreign keys' }),
    ).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Summary' }))
    expect(
      screen.getByRole('article', { name: 'Foreign keys without an index' }),
    ).toHaveTextContent('1')
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

  it('IDX-UI-012 shows the history charts of an expanded index and the Index tax tab', async () => {
    server.use(ok('getIndexHistory', indexHistory))
    render()
    await flushNetwork(30)

    expect(screen.getByRole('tab', { name: 'Index tax' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show details of orders_customer_idx' }))
    await flushNetwork(30)
    expect(
      screen.getByRole('img', { name: 'Index scans of orders_customer_idx' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('img', { name: 'Index size of orders_customer_idx' }),
    ).toBeInTheDocument()
  })

  it('IDX-UI-005 warns that the list is incomplete when the agent truncated it', async () => {
    render(ITEMS, true)
    await flushNetwork(30)

    expect(screen.getByText('The list is incomplete.')).toBeInTheDocument()
  })

  it('IDX-UI-008 says the unused check is pending while the advisor lacks a window of history', async () => {
    server.use(
      ok('getAdvisorWindows', oneHourWindows),
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

  /** A degraded unused-index finding on `object`, with the advisor's `reason`. */
  function degradedUnused(id: string, object: string, reason: string) {
    return {
      finding_id: id,
      rule_id: 'index.unused',
      severity: 'warning',
      state: 'degraded',
      scope: 'relation',
      datname: 'app',
      object_name: `public.${object}`,
      title: 'Advisor input unavailable',
      degraded_reason: reason,
    }
  }

  /** 20 MiB: above the advisor's 10 MiB floor, so the index can be judged. */
  const LARGE = 20 * 1024 * 1024

  /**
   * The unused-index check pending, with `windows` answering the advisor
   * windows; `orders_old_idx` is never scanned and `oldBytes` large.
   */
  function renderPending(
    windows: Parameters<typeof server.use>[0],
    {
      findings = [
        degradedUnused('f9', 'orders_old_idx', 'index usage history is shorter than the window'),
      ] as unknown[],
      oldBytes = LARGE,
    } = {},
  ) {
    server.use(
      windows,
      ok('getClusters', clusters),
      ok('getInstanceIndexes', {
        instance_id: INSTANCE_ID,
        items: ITEMS.map((item) =>
          item.indexrelname === 'orders_old_idx' ? { ...item, index_bytes: oldBytes } : item,
        ),
        truncated: false,
        relations_not_reported: null,
      }),
      ok('getFindings', findings),
    )
    return renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })
  }

  /** The status note whose text starts with `headline`. */
  function noteTitle(headline: string): string {
    const note = screen
      .getAllByRole('status')
      .find((element) => element.textContent?.startsWith(headline))
    expect(note, headline).toBeDefined()
    return note!.getAttribute('title') ?? ''
  }

  /** The pending note and the "never scanned" hint, the two texts that name the window. */
  function windowTexts(): string[] {
    const hint = screen.getByText('never scanned').getAttribute('title') ?? ''
    expect(hint).toMatch(/^No scan since the statistics were reset\./)
    return [noteTitle('Unused-index check pending'), hint]
  }

  const bounds = { default_seconds: 604800, min_seconds: 3600, max_seconds: 7776000 } as const

  it.each([
    [129600, '36 hours'],
    [1209600, '14 days'],
  ])(
    'HDR-WIN-001 the unused-index texts name the configured window (%i s: %s)',
    async (seconds, phrase) => {
      renderPending(
        ok('getAdvisorWindows', [
          { key: 'window.unused_index', seconds, source: 'custom', ...bounds },
          { key: 'window.backup_history', seconds: 604800, source: 'default', ...bounds },
        ]),
      )
      await flushNetwork(30)

      for (const text of windowTexts()) {
        expect(text).toContain(`unused after ${phrase} without a scan`)
        // Either source can complete the window: on PostgreSQL 16 and later the
        // statistics settle it before pglens has that much history (D22b).
        expect(text).toContain('from the pglens history or, on PostgreSQL 16 and later, from')
        expect(text).not.toMatch(/7 days/)
      }
    },
  )

  it.each([
    ['loading', never('getAdvisorWindows')],
    ['unavailable', status('getAdvisorWindows', 503, { error: 'store unavailable', detail: '' })],
  ])('HDR-WIN-002 while the window is %s the texts name no length', async (_state, windows) => {
    renderPending(windows)
    await flushNetwork(30)

    for (const text of windowTexts()) {
      expect(text).toContain('unused after its full observation window without a scan')
      expect(text).not.toMatch(/\d+ (days?|hours?)/)
    }
  })

  it('HDR-WIN-003 the rule texts follow the configured window, not the window of an older evaluation', async () => {
    renderPending(
      ok('getAdvisorWindows', [
        { key: 'window.unused_index', seconds: 129600, source: 'custom', ...bounds },
        { key: 'window.backup_history', seconds: 604800, source: 'default', ...bounds },
      ]),
      {
        findings: [
          degradedUnused('f9', 'orders_old_idx', 'index usage history is shorter than 7 days'),
          // Evaluated before the window changed: its evidence still says 7 days.
          {
            ...FINDINGS[0],
            finding_id: 'f8',
            object_name: 'public.orders_customer_idx',
            evidence: { window_seconds: 604800 },
          },
        ],
      },
    )
    await flushNetwork(30)

    for (const text of windowTexts()) {
      expect(text).toContain('unused after 36 hours without a scan')
      expect(text).not.toMatch(/7 days/)
    }
  })

  it('HDR-WIN-004 the hint of a small index promises no verdict and names the floor', async () => {
    renderPending(ok('getAdvisorWindows', oneHourWindows), { oldBytes: 8192 })
    await flushNetwork(30)

    const hint = screen.getByText('never scanned').getAttribute('title') ?? ''
    expect(hint).toBe(
      'No scan since the statistics were reset. The advisor never judges indexes of 10.0 MiB or less, so this is the raw counter, not a verdict.',
    )
  })

  it('HDR-WIN-005 only a short history is pending; a check that waiting does not settle says why', async () => {
    renderPending(ok('getAdvisorWindows', oneHourWindows), {
      findings: [
        degradedUnused('f9', 'orders_old_idx', 'index usage history is shorter than 1 hour'),
        degradedUnused(
          'f10',
          'orders_a_idx',
          'the unused-index window (90 days) is longer than the kept index metrics history (30 days)',
        ),
        degradedUnused('f11', 'orders_b_idx', 'standby scans unknown: no sibling snapshot'),
        degradedUnused('f12', 'orders_c_idx', 'standby scans unknown: no sibling snapshot'),
      ],
    })
    await flushNetwork(30)

    expect(screen.getByText('Unused-index check pending for 1 index.')).toBeInTheDocument()
    const blocked = noteTitle('Unused-index check inconclusive for 3 indexes.')
    expect(blocked).toContain(
      '2: Standby scans unknown: no standby of the cluster has sent a snapshot',
    )
    expect(blocked).toContain(
      '1: The unused-index window (90 days) is longer than the kept index metrics history (30 days): the pglens history never covers the window, so waiting does not settle the check',
    )
    // Neither promises a verdict after the window.
    expect(blocked).not.toContain('unused after')
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
