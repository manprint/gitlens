import { fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import SlowQueriesPage, { SLOWEST_LIMIT } from './SlowQueriesPage'

import { base as clusters } from '@/test/fixtures/getClusters'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
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
  it('ranks the top 20 statements by mean time on the first server that is up', async () => {
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
    expect(query?.get('limit')).toBe(String(SLOWEST_LIMIT))

    const list = screen.getByRole('list', { name: 'Slowest queries' })
    const items = within(list).getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(items[0]).toHaveTextContent('Query 11')
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
    screen.getByRole('list', { name: 'Slowest queries' })

    const database = screen.getByRole('combobox', { name: 'Database' })
    expect(
      within(database)
        .getAllByRole('option')
        .map((option) => option.textContent),
    ).toEqual(['All databases', 'app'])

    fireEvent.click(screen.getByRole('radio', { name: 'Total time' }))
    await flushNetwork(30)

    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('total_exec_time')
    expect(screen.getByRole('heading', { name: /Top 20 by total time/ })).toBeInTheDocument()
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
    await screen.findByRole('list', { name: 'Slowest queries' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
