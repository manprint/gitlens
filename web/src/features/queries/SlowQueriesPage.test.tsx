import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import SlowQueriesPage, { STATEMENT_FETCH_LIMIT } from './SlowQueriesPage'

import { base as clusters } from '@/test/fixtures/getClusters'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'
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

  it('SLOWLIST-005 searches the loaded statements and says when nothing matches', async () => {
    renderMany()
    await flushNetwork(30)

    searchTable('Slowest queries', 'stmt_4')
    expect(ranks()).toEqual(['40', '41', '42', '43', '44', '45'])
    searchTable('Slowest queries', 'no-such-statement')
    expect(screen.getByText('No rows match')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(dataRows('Slowest queries')).toHaveLength(20)
  })

  it('SLOWLIST-006 has no serious accessibility violations with the table controls', async () => {
    vi.useRealTimers()
    const { container } = renderMany('/queries?slow.q=stmt')
    await screen.findByRole('table', { name: 'Slowest queries' })
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('TBL-UI-110 slow pages, searches by database and sorts the statements', async () => {
    renderMany()
    await flushNetwork(30)

    searchTable('Slowest queries', 'crm')
    expect(dataRows('Slowest queries')).toHaveLength(5)
    searchTable('Slowest queries', '104')
    expect(ranks()).toEqual(['5'])
    searchTable('Slowest queries', '')
    sortTable('Slowest queries', 'Calls', 'descending')
    expect(dataRows('Slowest queries')).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Last page' }))
    expect(dataRows('Slowest queries')).toHaveLength(5)
  })
})
