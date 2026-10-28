import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it } from 'vitest'

import { QueryOverview } from './QueryOverview'

import { base as instance } from '@/test/fixtures/getInstance'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

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

function statements(rows: object[]) {
  return http.get('*/api/v1/statements', () =>
    HttpResponse.json({ comparable_scope: 'cluster', statements: rows, truncated: false }),
  )
}

describe('QueryOverview', () => {
  it('shows the statement text and its statistics for the selected window', async () => {
    server.use(ok('getInstance', instance), statements([STATEMENT]))

    renderWithProviders(<QueryOverview datname="app" instanceId={INSTANCE_ID} queryid="42" />)
    await flushNetwork(30)

    expect(screen.getByRole('heading', { name: 'Query detail and plan history' })).toBeVisible()
    expect(screen.getByRole('region', { name: 'Statement text' })).toHaveTextContent(
      STATEMENT.query_text,
    )
    expect(screen.getByRole('article', { name: 'Mean time' })).toHaveTextContent('1.5 s')
    expect(screen.getByRole('article', { name: 'Calls' })).toHaveTextContent('200')
    expect(screen.getByRole('article', { name: 'Rows per call' })).toHaveTextContent('2')
    expect(screen.getByRole('article', { name: 'Cache hit ratio' })).toHaveTextContent('90.0%')
    expect(screen.getByRole('link', { name: 'All queries' })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries`,
    )
  })

  it('explains when the query had no activity in the window', async () => {
    server.use(ok('getInstance', instance), statements([{ ...STATEMENT, queryid: '7' }]))

    renderWithProviders(<QueryOverview instanceId={INSTANCE_ID} queryid="42" />)
    await flushNetwork(30)

    expect(screen.getByText(/No pg_stat_statements activity was recorded/)).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Statement text' })).not.toBeInTheDocument()
  })
})
