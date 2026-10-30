import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { empty as emptyHistory } from '@/test/fixtures/getInstanceTableHistory'
import { base as search, empty as emptySearch } from '@/test/fixtures/searchInstance'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { dataRows, seq } from '@/test/table'

function index(name: string, bytes: number) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: bytes,
    idx_scan_rate: 0.5,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    last_scan_at: '2026-08-27T01:00:00Z',
    definition: `CREATE INDEX ${name} ON public.orders USING btree (id)`,
    ts: '2026-08-27T02:00:00Z',
  }
}

// The largest first (the inventory's default order): `orders_customer_idx` is 25th of 30.
const INDEXES = [
  ...seq(24).map((n, i) => index(`big_${n}_idx`, 1_000_000 - i)),
  index('orders_customer_idx', 500_000),
  ...seq(5).map((n, i) => index(`small_${n}_idx`, 1_000 - i)),
]

function table(name: string) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: name,
    n_live_tup: 1000,
    n_dead_tup: 100,
    total_bytes: 1_000_000,
    table_bytes: 800_000,
    ts: '2026-08-27T02:00:00Z',
  }
}

function render(
  route: string,
  requests: URL[] = [],
  body: object = search,
  tables = [table('orders'), table('customers')],
) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: INDEXES,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', []),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: tables,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceSettings', { instance_id: INSTANCE_ID, settings: [] }),
    ok('getInstanceTableHistory', emptyHistory),
    http.get('*/api/v1/instances/:id/search', ({ request }) => {
      requests.push(new URL(request.url))
      return HttpResponse.json(body)
    }),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

const results = () => screen.getByRole('region', { name: /^Search results for/ })

describe('Index analysis: Search everything (D20)', () => {
  it('SRCH-UI-010 groups the hits with their counts and links that open them', async () => {
    const requests: URL[] = []
    const columns = search.groups[3]!
    render(`/indexes?instance=${INSTANCE_ID}&q=orders`, requests, {
      ...search,
      groups: [...search.groups.slice(0, 3), { ...columns, total: 132 }],
    })
    await flushNetwork(30)

    // One request: the window is resolved once, not at every render.
    expect(requests).toHaveLength(1)
    const query = requests.at(-1)?.searchParams
    expect(query?.get('q')).toBe('orders')
    // The window of the page's time range, resolved once.
    expect(query?.get('from')).toMatch(/^\d{4}-/)
    expect(query?.get('to')).toMatch(/^\d{4}-/)
    expect(screen.getByRole('searchbox', { name: 'Search everything' })).toHaveValue('orders')

    const panel = results()
    expect(
      screen.getByRole('searchbox', { name: 'Search everything' }),
    ).toHaveAccessibleDescription('Searches everything pglens has collected in this range')
    expect(
      within(panel)
        .getAllByRole('heading', { level: 3 })
        .map((heading) => heading.textContent),
    ).toEqual(['Indexes1 match', 'Tables1 match', 'Columns132 matches', 'Queries2 matches'])
    expect(within(panel).getByText('Showing 50 of 132')).toBeInTheDocument()
    expect(within(panel).getAllByRole('link')).toHaveLength(1 + 1 + 50 + 2)

    // From 1600 px the lists stay short, so the tab's table header stays on screen (D26).
    expect(within(panel).getByRole('list', { name: 'Indexes found' })).toHaveClass(
      'min-[1600px]:max-h-40',
    )
    const indexLink = within(within(panel).getByRole('list', { name: 'Indexes found' })).getByRole(
      'link',
    )
    expect(indexLink).toHaveAttribute(
      'href',
      `/indexes?instance=${INSTANCE_ID}&tab=indexes&focus=app.public.orders_customer_idx`,
    )
    // The server marks the snippet; the client only renders it.
    expect(
      within(indexLink)
        .getAllByRole('mark')
        .map((mark) => mark.textContent),
    ).toContain('orders')
    const tableLink = within(within(panel).getByRole('list', { name: 'Tables found' })).getByRole(
      'link',
    )
    expect(tableLink.getAttribute('href')).toContain('tab=dead-rows')
    expect(tableLink.getAttribute('href')).toContain('focus=app.public.orders')
    for (const link of within(
      within(panel).getByRole('list', { name: 'Queries found' }),
    ).getAllByRole('link')) {
      expect(link).toHaveAttribute('href', `/queries?instance=${INSTANCE_ID}&q=orders`)
    }
  })

  it('keeps the time range of the page in the links of the hits', async () => {
    render(`/indexes?instance=${INSTANCE_ID}&range=6h&q=orders`)
    await flushNetwork(30)

    const panel = results()
    expect(
      within(within(panel).getByRole('list', { name: 'Indexes found' })).getByRole('link'),
    ).toHaveAttribute(
      'href',
      `/indexes?instance=${INSTANCE_ID}&tab=indexes&focus=app.public.orders_customer_idx&range=6h`,
    )
    for (const link of within(
      within(panel).getByRole('list', { name: 'Queries found' }),
    ).getAllByRole('link')) {
      // A relative range stays relative: `range`, not the bounds the request was resolved to.
      expect(link).toHaveAttribute('href', `/queries?instance=${INSTANCE_ID}&q=orders&range=6h`)
    }
  })

  it('says when nothing matches and clears the search', async () => {
    const { router } = render(`/indexes?instance=${INSTANCE_ID}&q=no-such-thing`, [], emptySearch)
    await flushNetwork(30)

    expect(within(results()).getByRole('status')).toHaveTextContent(
      'Nothing matches “no-such-thing” in this range',
    )
    fireEvent.click(screen.getByRole('button', { name: 'Clear the search' }))
    await flushNetwork(30)
    expect(router.state.location.search).not.toContain('q=')
    // The button went with the results: the search box keeps the keyboard.
    expect(screen.getByRole('searchbox', { name: 'Search everything' })).toHaveFocus()
    expect(screen.queryByRole('region', { name: /^Search results for/ })).not.toBeInTheDocument()
  })

  it('asks nothing for a term under 2 characters and searches after typing', async () => {
    const requests: URL[] = []
    const { router } = render(`/indexes?instance=${INSTANCE_ID}`, requests)
    await flushNetwork(30)
    expect(requests).toHaveLength(0)

    const box = screen.getByRole('searchbox', { name: 'Search everything' })
    fireEvent.change(box, { target: { value: 'o' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600)
    })
    expect(requests).toHaveLength(0)
    expect(screen.getByText('Type at least 2 characters')).toBeInTheDocument()

    fireEvent.change(box, { target: { value: 'orders' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)
    expect(router.state.location.search).toContain('q=orders')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('orders')
    expect(results()).toBeInTheDocument()
  })

  it('SRCH-UI-011 a hit opens its tab, turns to the row, expands it and drops focus from the URL', async () => {
    const { router } = render(`/indexes?instance=${INSTANCE_ID}&tab=cleanup&q=orders`)
    await flushNetwork(30)

    const panel = results()
    fireEvent.click(
      within(within(panel).getByRole('list', { name: 'Indexes found' })).getByRole('link'),
    )
    await flushNetwork(30)

    const toggle = screen.getByRole('button', { name: 'Hide details of orders_customer_idx' })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(toggle).toHaveFocus()
    // The row is 25th by size: the table turned to page 2.
    const search = router.state.location.search
    expect(search).toContain('indexes.page=2')
    expect(search).not.toContain('focus=')
    expect(search).not.toContain('q=')
    expect(screen.queryByRole('region', { name: /^Search results for/ })).not.toBeInTheDocument()
    expect(dataRows('Indexes').length).toBeGreaterThan(0)
  })

  it('SRCH-UI-011 a table or column hit expands its row on the Dead rows tab', async () => {
    const { router } = render(
      `/indexes?instance=${INSTANCE_ID}&tab=dead-rows&focus=app.public.customers`,
    )
    await flushNetwork(30)

    expect(screen.getByRole('button', { name: 'Hide details of customers' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )
    expect(screen.getByRole('button', { name: 'Show details of orders' })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
    expect(router.state.location.search).not.toContain('focus=')
  })

  it('SRCH-UI-011 clears the table search that hides the row of a hit', async () => {
    const { router } = render(
      `/indexes?instance=${INSTANCE_ID}&tab=indexes&indexes.q=big_1&focus=app.public.orders_customer_idx`,
    )
    await flushNetwork(30)

    expect(
      screen.getByRole('button', { name: 'Hide details of orders_customer_idx' }),
    ).toHaveAttribute('aria-expanded', 'true')
    expect(router.state.location.search).not.toContain('indexes.q')
    expect(router.state.location.search).not.toContain('focus=')
  })

  it('says a hit is not in the list when the tab has no rows', async () => {
    const { router } = render(
      `/indexes?instance=${INSTANCE_ID}&tab=dead-rows&focus=app.public.orders`,
      [],
      search,
      [],
    )
    await flushNetwork(30)

    expect(screen.getByText(/^The table/)).toHaveTextContent(
      'The table app.public.orders is not in the current list',
    )
    expect(router.state.location.search).not.toContain('focus=')
  })

  it('says a hit is not in the list when the Indexes tab has no rows', async () => {
    const { router } = render(
      `/indexes?instance=${INSTANCE_ID}&tab=indexes&focus=app.public.orders_customer_idx`,
    )
    // An empty inventory, in place of the one render() registers; the request is not sent yet.
    server.use(
      ok('getInstanceIndexes', {
        instance_id: INSTANCE_ID,
        items: [],
        truncated: false,
        relations_not_reported: null,
      }),
    )
    await flushNetwork(30)

    expect(screen.getByText(/^The index/)).toHaveTextContent(
      'The index app.public.orders_customer_idx is not in the current list',
    )
    expect(router.state.location.search).not.toContain('focus=')
  })

  it('never labels the results with a search that failed (I-9)', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    render(`/indexes?instance=${INSTANCE_ID}&q=orders`)
    server.use(
      http.get('*/api/v1/instances/:id/search', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q')
        if (q === 'ab') {
          return HttpResponse.json({ error: 'search failed', detail: 'boom' }, { status: 500 })
        }
        if (q === 'abc') await gate
        return HttpResponse.json({ ...search, q })
      }),
    )
    await flushNetwork(30)
    const box = () => screen.getByRole('searchbox', { name: 'Search everything' })
    for (const value of ['ab', 'abc']) {
      fireEvent.change(box(), { target: { value } })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      await flushNetwork(60)
    }
    // While `abc` loads, nothing claims `ab`: the shown groups are still those of `orders`.
    expect(screen.queryByRole('heading', { name: /“ab”/ })).not.toBeInTheDocument()
    expect(
      screen.queryAllByRole('mark').filter((mark) => /^ab/i.test(mark.textContent ?? '')),
    ).toHaveLength(0)

    release?.()
    await flushNetwork(30)
    expect(screen.getByRole('heading', { name: 'Search results for “abc”' })).toBeInTheDocument()
  })

  it('says when the row of a hit is not in the current list', async () => {
    const { router } = render(
      `/indexes?instance=${INSTANCE_ID}&tab=indexes&focus=app.public.gone_idx`,
    )
    await flushNetwork(30)

    const notice = screen
      .getAllByRole('status')
      .find((status) => status.textContent?.startsWith('The index'))
    expect(notice).toHaveTextContent(
      'The index app.public.gone_idx is not in the current list: Index analysis shows only what the agent reported in its latest collection.',
    )
    expect(router.state.location.search).not.toContain('focus=')
    expect(screen.getByRole('table', { name: 'Indexes' })).toBeInTheDocument()
    fireEvent.click(within(notice!).getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText(/is not in the current list/)).not.toBeInTheDocument()
  })

  it('SRCH-UI-012 has no serious accessibility violations with the results open', async () => {
    vi.useRealTimers()
    const { container } = render(`/indexes?instance=${INSTANCE_ID}&q=orders`)
    await screen.findByRole('region', { name: /^Search results for/ })
    await screen.findByRole('list', { name: 'Columns found' })
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
