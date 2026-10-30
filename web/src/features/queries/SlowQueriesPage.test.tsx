import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import SlowQueriesPage, { STATEMENT_FETCH_LIMIT } from './SlowQueriesPage'

import { base as clusters } from '@/test/fixtures/getClusters'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, seq, sortTable } from '@/test/table'
import { expectNoA11yViolations } from '@/test/a11y'

function statement(queryid: string, mean: number, calls: number, text: string) {
  return {
    calls,
    datname: 'app',
    mean_exec_time: mean,
    query_text: text,
    queryid,
    rows: calls,
    total_exec_time_ms: mean * calls,
    truncated: false,
  }
}

const STATEMENTS = [
  statement('11', 1_250, 4, 'SELECT * FROM orders WHERE customer_id = $1'),
  statement('22', 80, 1_000, 'UPDATE accounts SET balance = balance - $1 WHERE id = $2'),
]

function databases() {
  return {
    databases: [
      { datname: 'app', monitored: true },
      { datname: 'template1', monitored: false, skip_reason: 'template' },
    ],
    instance_id: INSTANCE_ID,
    not_monitored_count: 1,
  }
}

function captureStatements(
  requests: URL[],
  body: object = { comparable_scope: 'cluster', statements: STATEMENTS, truncated: false },
) {
  return http.get('*/api/v1/statements', ({ request }) => {
    requests.push(new URL(request.url))
    return HttpResponse.json(body)
  })
}

describe('SlowQueriesPage', () => {
  it('ranks the loaded statements by mean time on the first server that is up', async () => {
    const requests: URL[] = []
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements(requests),
    )

    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)

    expect(requests.length).toBeGreaterThan(0)
    const query = requests.at(-1)?.searchParams
    expect(query?.get('instance_id')).toBe(INSTANCE_ID)
    expect(query?.get('order_by')).toBe('mean_exec_time')
    expect(query?.get('limit')).toBe(String(STATEMENT_FETCH_LIMIT))

    const items = dataRows('Slowest queries')
    expect(items).toHaveLength(2)
    // The link reads as the query text; its name stays `Query <id>`.
    expect(items[0]).toHaveTextContent('1.25 s')
    expect(within(items[0]!).getByRole('link', { name: /Query 11/ })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries/11?db=app`,
    )
  })

  it('switches the ranking to total time and offers only monitored databases', async () => {
    const requests: URL[] = []
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements(requests),
    )

    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)
    screen.getByRole('table', { name: 'Slowest queries' })

    const database = screen.getByRole('combobox', { name: 'Database' })
    expect(
      within(database)
        .getAllByRole('option')
        .map((option) => option.textContent),
    ).toEqual(['All databases', 'app'])

    fireEvent.click(screen.getByRole('radio', { name: 'Total time' }))
    await flushNetwork(30)

    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('total_exec_time')
    expect(screen.getByRole('heading', { name: /Ranked by total time/ })).toBeInTheDocument()
  })

  it('explains an empty window instead of showing a blank list', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements([], { comparable_scope: 'cluster', statements: [], truncated: false }),
    )

    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)

    expect(screen.getByRole('heading', { name: 'No statements yet' })).toBeInTheDocument()
  })

  it('has no serious or critical accessibility violations', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements([]),
    )
    vi.useRealTimers()
    const { container } = renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await screen.findByRole('table', { name: 'Slowest queries' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('SlowQueriesPage table', () => {
  // 45 statements in server order: the first is the slowest per call.
  const MANY = seq(45).map((n, index) => ({
    calls: 1,
    datname: index < 5 ? 'crm' : 'app',
    query_text: `SELECT stmt_${n}`,
    queryid: String(100 + index),
    rows: 1,
    total_exec_time_ms: (45 - index) * 1_000,
    truncated: false,
  }))

  function renderMany(route = '/queries', requests: URL[] = []) {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements(requests, {
        comparable_scope: 'cluster',
        statements: MANY,
        truncated: false,
      }),
    )
    return renderWithProviders(<SlowQueriesPage />, { route })
  }

  const ranks = () => columnTexts('Slowest queries', 0)

  it('SLOWLIST-001 requests 200 statements ranked by the chosen order', async () => {
    const requests: URL[] = []
    renderMany('/queries', requests)
    await flushNetwork(30)
    expect(requests.at(-1)?.searchParams.get('limit')).toBe('200')
    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('mean_exec_time')
    fireEvent.click(screen.getByRole('radio', { name: 'Total time' }))
    await flushNetwork(30)
    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('total_exec_time')
    expect(requests.at(-1)?.searchParams.get('limit')).toBe('200')
  })

  it('SLOWLIST-002 shows 20 of 45 ranked rows per page with a pager', async () => {
    renderMany()
    await flushNetwork(30)

    expect(dataRows('Slowest queries')).toHaveLength(20)
    expect(screen.getByText('Page 1 of 3')).toBeVisible()
    expect(ranks()).toEqual(Array.from({ length: 20 }, (_, index) => `${index + 1}`))
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(ranks()[0]).toBe('21')
    expect(ranks().at(-1)).toBe('40')
  })

  it('SLOWLIST-003 switching the ranking refetches and returns to page 1', async () => {
    const requests: URL[] = []
    const { router } = renderMany('/queries?slow.page=2', requests)
    await flushNetwork(30)
    expect(ranks()[0]).toBe('21')

    fireEvent.click(screen.getByRole('radio', { name: 'Total time' }))
    await flushNetwork(30)
    expect(router.state.location.search).not.toContain('slow.page')
    expect(ranks()[0]).toBe('1')
  })

  it('SLOWLIST-004 keeps the server rank by default and sorts the whole loaded set', async () => {
    renderMany()
    await flushNetwork(30)
    expect(ranks()[0]).toBe('1')

    sortTable('Slowest queries', 'Mean time', 'ascending')
    // The fastest per call is ranked 45th by the server: it now leads page 1.
    expect(ranks()[0]).toBe('45')
  })

  it('SLOWLIST-006 has no serious accessibility violations with the table controls', async () => {
    vi.useRealTimers()
    const { container } = renderMany('/queries?q=stmt')
    await screen.findByRole('table', { name: 'Slowest queries' })
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('TBL-UI-110 slow pages and sorts the statements', async () => {
    renderMany()
    await flushNetwork(30)

    sortTable('Slowest queries', 'Calls', 'descending')
    expect(dataRows('Slowest queries')).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Last page' }))
    expect(dataRows('Slowest queries')).toHaveLength(5)
  })
})

describe('SlowQueriesPage search (D20)', () => {
  const PARTNER = [
    statement('31', 40, 3, 'SELECT id FROM res_partner WHERE name = $1'),
    statement('32', 20, 9, 'UPDATE Res_Partner SET active = $1'),
  ]

  function renderSearch(route: string, requests: URL[], body: object) {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements(requests, body),
    )
    return renderWithProviders(<SlowQueriesPage />, { route })
  }

  it('SRCH-UI-007 asks the server for ?q= with the ranking and marks the matches', async () => {
    const requests: URL[] = []
    renderSearch(`/queries?instance=${INSTANCE_ID}&rank=total&q=res_partner`, requests, {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
      has_more: false,
    })
    await flushNetwork(30)

    const query = requests.at(-1)?.searchParams
    expect(query?.get('q')).toBe('res_partner')
    expect(query?.get('order_by')).toBe('total_exec_time')
    expect(query?.get('limit')).toBe(String(STATEMENT_FETCH_LIMIT))
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveValue('res_partner')
    // One search box: the table has no search of its own.
    expect(screen.getAllByRole('searchbox')).toHaveLength(1)
    const table = screen.getByRole('table', { name: 'Slowest queries' })
    expect(
      within(table)
        .getAllByRole('mark')
        .map((mark) => mark.textContent),
    ).toEqual(['res_partner', 'Res_Partner'])
    expect(screen.getByText(/2 matches\./)).toBeInTheDocument()
  })

  it('SRCH-UI-007 typing searches after the wait and keeps the ranking and database', async () => {
    const requests: URL[] = []
    const { router } = renderSearch('/queries?rank=total&db=app&slow.page=2', requests, {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
    })
    await flushNetwork(30)
    expect(requests.at(-1)?.searchParams.has('q')).toBe(false)

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search statements' }), {
      target: { value: 'res_partner' },
    })
    await flushNetwork(30)
    // Nothing is asked before the wait ends.
    expect(requests.at(-1)?.searchParams.has('q')).toBe(false)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)

    const search = router.state.location.search
    expect(search).toContain('q=res_partner')
    expect(search).toContain('rank=total')
    expect(search).toContain('db=app')
    expect(search).not.toContain('slow.page')
    const query = requests.at(-1)?.searchParams
    expect(query?.get('q')).toBe('res_partner')
    expect(query?.get('database')).toBe('app')
    expect(query?.get('order_by')).toBe('total_exec_time')
  })

  it('SRCH-UI-008 no match shows the no-match state, not the no-data state', async () => {
    const { router } = renderSearch('/queries?q=zzzzqqqq', [], {
      comparable_scope: 'cluster',
      statements: [],
      truncated: false,
      has_more: false,
    })
    await flushNetwork(30)

    expect(
      screen.getByRole('heading', { name: 'No statement matches “zzzzqqqq” in this range' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'No statements yet' })).not.toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveValue('zzzzqqqq')
    fireEvent.click(screen.getByRole('button', { name: 'Show all statements' }))
    await flushNetwork(30)
    expect(router.state.location.search).not.toContain('q=')
    // The button is gone: the search box keeps the keyboard.
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveFocus()
  })

  it('says when more statements match than were loaded (I-9)', async () => {
    renderSearch('/queries?q=res_partner', [], {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
      has_more: true,
    })
    await flushNetwork(30)
    expect(screen.getByText(/More than 2 matches — narrow the search\./)).toBeInTheDocument()
  })

  it('keeps typing that goes on while a search loads, when the box moves out of the table', async () => {
    const requests: URL[] = []
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      http.get('*/api/v1/statements', ({ request }) => {
        const url = new URL(request.url)
        requests.push(url)
        return HttpResponse.json({
          comparable_scope: 'cluster',
          statements: url.searchParams.has('q') ? [] : STATEMENTS,
          truncated: false,
          ...(url.searchParams.has('q') ? { has_more: false } : {}),
        })
      }),
    )
    const { router } = renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)
    expect(screen.getByRole('table', { name: 'Slowest queries' })).toBeInTheDocument()

    const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
    act(() => box().focus())
    fireEvent.change(box(), { target: { value: 'zz' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    expect(router.state.location.search).toContain('q=zz')
    // More text before the answer to `zz`, which has no rows: the box leaves the table.
    fireEvent.change(box(), { target: { value: 'zzqq' } })
    await flushNetwork(30)
    expect(
      screen.getByRole('heading', { name: 'No statement matches “zz” in this range' }),
    ).toBeInTheDocument()
    expect(box()).toHaveValue('zzqq')
    expect(box()).toHaveFocus()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)
    expect(router.state.location.search).toContain('q=zzqq')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('zzqq')
    expect(box()).toHaveValue('zzqq')
  })

  it('keeps the box when the server refuses the search, to change or clear it', async () => {
    const long = 'x'.repeat(201)
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      http.get('*/api/v1/statements', ({ request }) =>
        new URL(request.url).searchParams.has('q')
          ? HttpResponse.json(
              { error: 'invalid q', detail: 'q must be 2–200 characters' },
              { status: 400 },
            )
          : HttpResponse.json({
              comparable_scope: 'cluster',
              statements: STATEMENTS,
              truncated: false,
            }),
      ),
    )
    const { router } = renderWithProviders(<SlowQueriesPage />, { route: `/queries?q=${long}` })
    await flushNetwork(60)

    const box = screen.getByRole('searchbox', { name: 'Search statements' })
    expect(box).toHaveValue(long)
    expect(box).toHaveAttribute('maxlength', '200')
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
    await flushNetwork(60)
    expect(router.state.location.search).not.toContain('q=')
    expect(screen.getByRole('table', { name: 'Slowest queries' })).toBeInTheDocument()
  })

  it('an older link that searched the table (slow.q) now searches the server', async () => {
    const requests: URL[] = []
    const { router } = renderSearch('/queries?slow.q=res_partner', requests, {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
      has_more: false,
    })
    await flushNetwork(30)
    expect(router.state.location.search).toContain('q=res_partner')
    expect(router.state.location.search).not.toContain('slow.q')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('res_partner')
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveValue('res_partner')
  })

  it('announces the match count politely', async () => {
    renderSearch('/queries?q=res_partner', [], {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
      has_more: true,
    })
    await flushNetwork(30)
    expect(screen.getByText(/More than 2 matches/)).toHaveAttribute('aria-live', 'polite')
  })

  it('keeps the marks and the count of the shown statements while a new search loads (I-9)', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      http.get('*/api/v1/statements', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q')
        if (q === 'update') await gate
        return HttpResponse.json({
          comparable_scope: 'cluster',
          statements: q === 'update' ? [PARTNER[1]] : PARTNER,
          truncated: false,
          has_more: false,
        })
      }),
    )
    renderWithProviders(<SlowQueriesPage />, { route: '/queries?q=res_partner' })
    await flushNetwork(30)

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search statements' }), {
      target: { value: 'update' },
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)
    // Still the `res_partner` rows: marked and counted for `res_partner`, not `update`.
    const table = screen.getByRole('table', { name: 'Slowest queries' })
    expect(
      within(table)
        .getAllByRole('mark')
        .map((mark) => mark.textContent),
    ).toEqual(['res_partner', 'Res_Partner'])
    expect(screen.getByText(/2 matches\./)).toBeInTheDocument()

    release?.()
    await flushNetwork(30)
    expect(
      within(screen.getByRole('table', { name: 'Slowest queries' }))
        .getAllByRole('mark')
        .map((mark) => mark.textContent),
    ).toEqual(['UPDATE'])
    expect(screen.getByText(/1 match\./)).toBeInTheDocument()
  })

  it('never labels the shown statements with a search that failed (I-9)', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      http.get('*/api/v1/statements', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q')
        if (q === 'ab') {
          return HttpResponse.json({ error: 'search failed', detail: 'boom' }, { status: 500 })
        }
        if (q === 'abc') await gate
        return HttpResponse.json({
          comparable_scope: 'cluster',
          statements: [statement('41', 10, 1, 'SELECT abc FROM t')],
          truncated: false,
          ...(q === null ? {} : { has_more: false }),
        })
      }),
    )
    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)
    const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
    for (const value of ['ab', 'abc']) {
      fireEvent.change(box(), { target: { value } })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      await flushNetwork(60)
    }
    // `abc` is still loading: nothing on screen claims `ab` or `abc`.
    expect(screen.queryAllByRole('mark')).toHaveLength(0)
    expect(screen.queryByText(/\bmatch(es)?\./)).not.toBeInTheDocument()

    release?.()
    await flushNetwork(30)
    expect(screen.getAllByRole('mark').map((mark) => mark.textContent)).toEqual(['abc'])
  })

  it('SRCH-UI-012 has no serious accessibility violations with a search', async () => {
    vi.useRealTimers()
    const { container } = renderSearch('/queries?q=res_partner', [], {
      comparable_scope: 'cluster',
      statements: PARTNER,
      truncated: false,
      has_more: false,
    })
    await screen.findByRole('table', { name: 'Slowest queries' })
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('SlowQueriesPage Full HD layout (D26)', () => {
  function renderLoaded(
    body: object = { comparable_scope: 'cluster', statements: STATEMENTS, truncated: false },
  ) {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', databases()),
      captureStatements([], body),
    )
    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
  }

  it('FHD-APPLY-001 puts the server, ranking and database controls in the list, after the search box', async () => {
    renderLoaded()
    await flushNetwork(30)
    const list = screen.getByRole('region', { name: /Ranked by mean time per call/ })
    const search = within(list).getByRole('search')
    for (const [label, id] of [
      ['Server', 'slow-instance'],
      ['Database', 'slow-db'],
    ] as const) {
      const control = within(list).getByRole('combobox', { name: label })
      expect(
        search.compareDocumentPosition(control) & Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy()
      // From 1600 px the control names leave the line but still label the controls.
      expect(within(list).getByText(label, { selector: `label[for="${id}"]` })).toHaveClass(
        'min-[1600px]:sr-only',
      )
    }
    expect(within(list).getByRole('radio', { name: 'Mean time per call' })).toBeChecked()
  })

  it('FHD-APPLY-002 shows the summary tiles in the ranked list header', async () => {
    renderLoaded()
    await flushNetwork(30)
    const list = screen.getByRole('region', { name: /Ranked by mean time per call/ })
    expect(within(list).getAllByRole('article')).toHaveLength(4)
    expect(within(list).getByRole('article', { name: 'Calls' })).toHaveTextContent('1,004')
    expect(
      screen
        .getByRole('heading', { name: /Ranked by/ })
        .compareDocumentPosition(within(list).getByRole('article', { name: 'Calls' })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('FHD-APPLY-003 states the top-N cut in the list header description', async () => {
    renderLoaded({ comparable_scope: 'cluster', statements: STATEMENTS, truncated: true })
    await flushNetwork(30)
    const description = screen.getByTitle(/Some statements may be missing/)
    expect(description).toHaveTextContent('Some statements may be missing')
    expect(description).toHaveClass('min-[1600px]:truncate')
  })

  it('FHD-APPLY-004 keeps the controls in a bar when there is no statement to list', async () => {
    renderLoaded({ comparable_scope: 'cluster', statements: [], truncated: false })
    await flushNetwork(30)
    expect(screen.queryByRole('table', { name: 'Slowest queries' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Server')).toBeInTheDocument()
    expect(screen.getByLabelText('Database')).toBeInTheDocument()
    expect(screen.getByText('No statements yet')).toBeInTheDocument()
  })
})
