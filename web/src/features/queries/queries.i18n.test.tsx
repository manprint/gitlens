import { screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import SlowQueriesPage from './SlowQueriesPage'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

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

describe('queries i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getClusters', clusters),
      http.get('*/api/v1/statements', () =>
        HttpResponse.json({
          comparable_scope: 'cluster',
          statements: [
            statement('11', 1_250, 4, 'SELECT * FROM orders WHERE customer_id = $1'),
            statement('22', 80, 1_000, 'UPDATE accounts SET balance = balance - $1 WHERE id = $2'),
          ],
          truncated: false,
        }),
      ),
    )
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-033 the slowest queries page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<SlowQueriesPage />, { route: '/queries' })

    expect(await screen.findByRole('heading', { name: 'Query più lente' })).toBeInTheDocument()
    const table = await screen.findByRole('table', { name: 'Query più lente' })
    expect(
      screen.getByRole('heading', { name: 'Ordinate per tempo medio per chiamata' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'Tempo medio per chiamata' })).toBeChecked()
    expect(screen.getByRole('searchbox', { name: 'Cerca statement' })).toBeInTheDocument()

    const database = screen.getByRole('combobox', { name: 'Database' })
    expect(within(database).getByRole('option', { name: 'Tutti i database' })).toBeInTheDocument()

    expect(within(table).getByRole('columnheader', { name: /Tempo totale/ })).toBeInTheDocument()
  })
})
