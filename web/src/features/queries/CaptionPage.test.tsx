import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it } from 'vitest'

import ExplainPanel from './ExplainPanel'
import PlanHistory from './PlanHistory'
import QueryListPage from './QueryListPage'
import { QueryOverview } from './QueryOverview'
import SlowQueriesPage from './SlowQueriesPage'

import { CAPTIONS } from '@/lib/captions'
import { base as clusters } from '@/test/fixtures/getClusters'
import { base as instance } from '@/test/fixtures/getInstance'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { expectCaptionedCards, expectClearCard } from '@/test/clearCard'

const STATEMENT = {
  calls: 200,
  datname: 'app',
  mean_exec_time: 1_500,
  query_text: 'SELECT * FROM orders WHERE customer_id = $1',
  queryid: '42',
  rows: 400,
  shared_blks_hit: 90,
  shared_blks_read: 10,
  total_exec_time_ms: 300_000,
  truncated: false,
  wal_bytes: 2048,
}

function statements() {
  return http.get('*/api/v1/statements', () =>
    HttpResponse.json({ comparable_scope: 'cluster', statements: [STATEMENT], truncated: false }),
  )
}

function captionedArticles(container: HTMLElement) {
  return [...container.querySelectorAll<HTMLElement>('article[data-card]')]
}

/** The visible caption line: a one-line caption carries its sentence in `title`. */
function expectCaptionLine(what: string) {
  expect(screen.getAllByTitle(what)[0], what).toBeVisible()
}

describe('queries captions (CAPTION-PAGE)', () => {
  it('CAPTION-PAGE-001 the statement list captions its section and its table', async () => {
    server.use(statements())
    const { container } = renderWithProviders(<QueryListPage instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}/queries?range=1h`,
    })
    await flushNetwork(30)

    const ids = expectCaptionedCards(container)
    expect(ids).toEqual(['queries.statements-section', 'queries.statements-table'])
    expectCaptionLine(CAPTIONS['queries.statements-section'].what)
    expectCaptionLine(CAPTIONS['queries.statements-table'].what)
    expect(screen.getByRole('button', { name: 'About Sort statements' })).toBeVisible()
  })

  it('CAPTION-PAGE-002 the slowest queries page captions every tile and its table', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', {
        databases: [{ datname: 'app', monitored: true }],
        instance_id: INSTANCE_ID,
        not_monitored_count: 0,
      }),
      statements(),
    )
    const { container } = renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)

    const ids = expectCaptionedCards(container)
    expect(ids).toEqual([
      'slow.slowest-mean',
      'slow.total-time',
      'slow.calls',
      'slow.statements-ranked',
      'slow.table',
    ])
    expectCaptionLine(CAPTIONS['slow.table'].what)
    expect(screen.getByRole('button', { name: 'About Rank by' })).toBeVisible()
  })

  it('CARD-ALL-001 every tile of the slowest queries page is a clear card', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceDatabases', {
        databases: [{ datname: 'app', monitored: true }],
        instance_id: INSTANCE_ID,
        not_monitored_count: 0,
      }),
      statements(),
    )
    const { container } = renderWithProviders(<SlowQueriesPage />, { route: '/queries' })
    await flushNetwork(30)

    const tiles = captionedArticles(container)
    expect(tiles).toHaveLength(4)
    tiles.forEach(expectClearCard)
    const ranked = screen.getByRole('article', { name: 'Statements ranked' })
    expect(within(ranked).getByText('1')).toBeVisible()
    expect(ranked).toHaveTextContent('1statements')
    expect(ranked).toHaveTextContent('The page ranks at most 200 statements.')
    expect(within(ranked).queryByText(/of 200 max/)).not.toBeInTheDocument()
  })

  it('CAPTION-PAGE-003 the query detail captions its six tiles', async () => {
    server.use(ok('getInstance', instance), statements())
    const { container } = renderWithProviders(
      <QueryOverview datname="app" instanceId={INSTANCE_ID} queryid="42" />,
    )
    await flushNetwork(30)

    const ids = expectCaptionedCards(container)
    expect(ids).toEqual([
      'queries.detail-mean-time',
      'queries.detail-total-time',
      'queries.detail-calls',
      'queries.detail-rows-per-call',
      'queries.detail-cache-hit-ratio',
      'queries.detail-wal',
    ])
    expect(screen.getByRole('button', { name: 'About Cache hit ratio' })).toBeVisible()
  })

  it('CARD-ALL-001 every tile of the query detail is a clear card', async () => {
    server.use(ok('getInstance', instance), statements())
    const { container } = renderWithProviders(
      <QueryOverview datname="app" instanceId={INSTANCE_ID} queryid="42" />,
    )
    await flushNetwork(30)

    const tiles = captionedArticles(container)
    expect(tiles).toHaveLength(6)
    tiles.forEach(expectClearCard)
  })

  it('CAPTION-PAGE-004 the execution plan panel is captioned and says which tier each plan needs', () => {
    const { container } = renderWithProviders(
      <ExplainPanel currentTier="T1" instanceId={INSTANCE_ID} queryid={42} />,
    )

    expect(expectCaptionedCards(container)).toEqual(['queries.explain-panel'])
    expect(screen.getByRole('button', { name: 'About Execution plan' })).toBeVisible()
    expect(CAPTIONS['queries.explain-panel'].howToRead).toMatch(/T1/)
    expect(CAPTIONS['queries.explain-panel'].howToRead).toMatch(/allow_explain_analyze/)
  })

  it('CAPTION-PAGE-005 the plan history is captioned', async () => {
    server.use(http.get('*/api/v1/plans', () => HttpResponse.json({ plans: [], total_shapes: 0 })))
    const { container } = renderWithProviders(<PlanHistory instanceId={INSTANCE_ID} queryid="42" />)
    await flushNetwork(30)

    expect(expectCaptionedCards(container)).toEqual(['queries.plan-history'])
    expect(screen.getByRole('button', { name: 'About Plan history' })).toBeVisible()
  })
})
