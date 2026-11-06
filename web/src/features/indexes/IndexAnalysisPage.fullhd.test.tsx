import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'
import { DEFAULT_INDEX_VIEW, INDEX_VIEWS, readIndexView } from './indexViews'

import { expectNoA11yViolations } from '@/test/a11y'
import { STANDBY_STATS_NOTE } from '@/lib/standby'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as advisorWindows } from '@/test/fixtures/getAdvisorWindows'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { dataRows } from '@/test/table'

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
]

const FINDINGS = [
  {
    finding_id: 'f1',
    rule_id: 'index.unused',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders_customer_idx',
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
    remediation: 'Create an index on public.orders (customer_id)',
    evidence: {
      table: 'public.orders',
      constraint: 'orders_customer_fk',
      columns: 'customer_id',
      referenced: 'public.customers',
    },
  },
]

function render(route = '/indexes', truncated = false) {
  server.use(
    ok('getAdvisorWindows', advisorWindows),
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: ITEMS,
      truncated,
      relations_not_reported: truncated ? 1 : null,
    }),
    ok('getFindings', FINDINGS),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

/** The header line of the open tab's panel. */
const panelHeader = () => screen.getByTestId('index-panel-header')

/** The status note that starts with `headline`. */
function noteFor(headline: string): HTMLElement {
  const note = screen
    .getAllByRole('status')
    .find((status) => status.textContent?.startsWith(headline))
  expect(note, headline).toBeDefined()
  return note!
}

const viewTab = (name: string | RegExp) =>
  within(screen.getByRole('tablist', { name: 'Index views' })).getByRole('tab', { name })

describe('IndexAnalysisPage on a Full HD screen (15.3)', () => {
  it('HA-V-10-4 a standby note takes one compact line with its full text in the title', async () => {
    render()
    server.use(
      ok('getClusters', [
        { ...clusters[0], instances: [{ ...clusters[0]!.instances[0], role: 'standby' }] },
      ]),
    )
    const { unmount } = renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })
    await flushNetwork(30)
    const note = screen.getAllByTestId('standby-stats-note').at(-1)!
    expect(note).toHaveAttribute('role', 'note')
    expect(note).toHaveAttribute('title', STANDBY_STATS_NOTE)
    for (const name of ['truncate', 'min-[1600px]:py-0.5', 'min-[1600px]:text-xs'])
      expect(note.className.split(' ')).toContain(name)
    unmount()
  })

  it('FHD-APPLY-030 the Indexes tab shows one table, the inventory, on its default view', async () => {
    const { router } = render()
    await flushNetwork(30)

    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Indexes' })).toBeInTheDocument()
    expect(
      within(screen.getByRole('tablist', { name: 'Index views' }))
        .getAllByRole('tab')
        .map((tab) => tab.textContent),
    ).toEqual(['Inventory', 'Summary', 'Advisor findings2', 'Foreign keys1'])
    expect(viewTab('Inventory')).toHaveAttribute('aria-selected', 'true')
    // The default view is never written to the URL.
    expect(router.state.location.search).toBe('')
  })

  it('FHD-APPLY-031 the view lives in ?view=, the default is not written and an unknown value falls back', async () => {
    expect(INDEX_VIEWS.map((view) => view.id)).toEqual([
      'inventory',
      'summary',
      'findings',
      'foreign-keys',
    ])
    expect(readIndexView(null)).toBe(DEFAULT_INDEX_VIEW)
    expect(readIndexView('nope')).toBe('inventory')

    const { router } = render('/indexes?view=nope')
    await flushNetwork(30)
    expect(viewTab('Inventory')).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('table', { name: 'Indexes' })).toBeInTheDocument()

    fireEvent.mouseDown(viewTab('Summary'))
    expect(router.state.location.search).toBe('?view=summary')
    // The summary holds the tiles and the breakdowns, no table.
    expect(screen.queryAllByRole('table')).toHaveLength(0)
    expect(screen.getByRole('article', { name: 'Primary key indexes' })).toHaveTextContent('1')

    fireEvent.mouseDown(viewTab('Inventory'))
    expect(router.state.location.search).toBe('')
  })

  it('FHD-APPLY-032 the findings and the foreign keys are one table each, their text truncated with a title', async () => {
    render('/indexes?view=findings')
    await flushNetwork(30)

    expect(screen.getAllByRole('table')).toHaveLength(1)
    const findings = screen.getByRole('table', { name: 'Index findings by index' })
    expect(findings).toHaveClass('min-[1600px]:table-fixed')
    expect(within(findings).getByTitle('Unused index')).toHaveClass('min-[1600px]:truncate')
    fireEvent.click(screen.getByRole('button', { name: 'Show the detail of Unused index' }))
    expect(screen.getByText('Confirm workload intent before dropping')).toBeInTheDocument()

    fireEvent.mouseDown(viewTab(/^Foreign keys/))
    expect(screen.getAllByRole('table')).toHaveLength(1)
    const gaps = screen.getByRole('table', { name: 'Foreign keys without an index' })
    expect(dataRows('Foreign keys without an index')).toHaveLength(1)
    expect(within(gaps).getByTitle('Create an index on public.orders (customer_id)')).toHaveClass(
      'min-[1600px]:truncate',
    )
  })

  it('FHD-APPLY-033 the tab list, the server and the database share the page title line', async () => {
    render()
    await flushNetwork(30)

    const line = within(screen.getByRole('banner'))
    const tabs = line.getByRole('tablist', { name: 'Index management sections' })
    expect(
      within(tabs)
        .getAllByRole('tab')
        .map((tab) => tab.textContent),
    ).toEqual(['Indexes', 'Cleanup', 'Dead rows', 'Suggestions', 'Index tax'])
    const controls = screen.getByTestId('index-page-controls')
    expect(controls).toHaveClass('min-[1600px]:flex-nowrap')
    expect(controls).toContainElement(tabs)
    for (const name of ['Server', 'Database']) {
      expect(line.getByRole('combobox', { name })).toHaveClass('min-[1600px]:h-8')
      expect(line.getByText(name, { selector: 'label' })).toHaveClass('min-[1600px]:sr-only')
    }
  })

  it('FHD-APPLY-034 an inventory row is one line: fixed widths, truncated text, compact badges and toggle', async () => {
    render()
    await flushNetwork(30)

    const table = screen.getByRole('table', { name: 'Indexes' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getByTitle('orders_customer_idx')).toHaveClass('min-[1600px]:truncate')
    expect(within(table).getByTitle('customer_id, created_at')).toHaveClass('min-[1600px]:truncate')
    const partial = within(table).getByText('partial')
    expect(partial).toHaveClass('min-[1600px]:py-0')
    const flags = within(table).getByTitle('partial, unused')
    // Whole badges only: the row stays one badge line and hides the overflow.
    expect(flags).toHaveClass('min-[1600px]:max-h-5', 'min-[1600px]:overflow-hidden')
    expect(flags).toContainElement(partial)
    expect(within(table).getAllByRole('button', { name: /^Show details of/ })[0]).toHaveClass(
      'min-[1600px]:size-5',
    )
  })

  it('FHD-APPLY-035 the title, description, figures, view tabs and notes share one header row, a note wraps and keeps its title', async () => {
    render('/indexes', true)
    await flushNetwork(30)

    const header = panelHeader()
    expect(header).toContainElement(screen.getByRole('heading', { name: 'Index inventory' }))
    expect(header).toHaveClass('min-[1600px]:flex-row')
    const tile = screen.getByRole('article', { name: 'Indexes reported' })
    expect(header).toContainElement(tile)
    expect(tile).toHaveAttribute('title', 'Indexes reported: 2 indexes')
    expect(header).toContainElement(screen.getByRole('tablist', { name: 'Index views' }))

    const note = noteFor('The list is incomplete.')
    expect(header).toContainElement(note)
    expect(note.className).not.toMatch(/truncate/)
    expect(note.getAttribute('title')).toMatch(/^The list is incomplete\. .*top_n/)
    expect(screen.getByText(/^2 indexes\. Scan figures/)).toHaveClass('min-[1600px]:line-clamp-3')
  })

  it('FHD-APPLY-038 a tab change drops the view and a database change sends the new tables to page 1', async () => {
    const { router } = render('/indexes?view=findings&index-findings.page=2&fk-gaps.page=2')
    await flushNetwork(30)

    fireEvent.change(screen.getByRole('combobox', { name: 'Database' }), {
      target: { value: 'app' },
    })
    expect(router.state.location.search).not.toContain('index-findings.page')
    expect(router.state.location.search).not.toContain('fk-gaps.page')
    expect(router.state.location.search).toContain('view=findings')

    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Cleanup' }))
    expect(router.state.location.search).toContain('tab=cleanup')
    expect(router.state.location.search).not.toContain('view=')
  })

  it('FHD-APPLY-039 the findings view has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render('/indexes?view=findings')
    await screen.findByRole('table', { name: 'Index findings by index' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('HDR-UI-004 the findings and foreign-key views keep their header line short: short titles, a compact Findings link', async () => {
    render('/indexes?view=findings', true)
    await flushNetwork(30)

    const line = panelHeader()
    expect(line).toContainElement(screen.getByRole('heading', { level: 2, name: 'Index findings' }))
    // From 1600 px the link reads "Findings" with an arrow; its name stays "Open Findings".
    const link = screen.getByRole('link', { name: 'Open Findings' })
    expect(screen.getByTestId('index-panel-actions')).toContainElement(link)
    expect(link).toHaveAttribute('href', '/findings')
    expect(link).toHaveClass('whitespace-nowrap')
    expect(within(link).getByText('Open')).toHaveClass('min-[1600px]:sr-only')

    fireEvent.mouseDown(viewTab(/^Foreign keys/))
    expect(
      within(panelHeader()).getByRole('heading', { level: 2, name: 'Unindexed foreign keys' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Open Findings' })).not.toBeInTheDocument()
  })
})
