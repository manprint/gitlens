import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import QueryListPage from './QueryListPage'

import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'

function statementResponse(overrides: Record<string, unknown> = {}) {
  return {
    comparable_scope: 'cluster',
    statements: [
      {
        calls: 12,
        datname: 'postgres',
        query_text: 'SELECT  *  FROM users WHERE id = $1',
        queryid: 123,
        rows: 12,
        total_exec_time_ms: 240,
        truncated: false,
      },
    ],
    truncated: false,
    ...overrides,
  }
}

function renderPage(route = `/instances/${INSTANCE_ID}/queries?range=1h`) {
  return renderWithProviders(<QueryListPage instanceId={INSTANCE_ID} />, { route })
}

function statementsHandler(body: object) {
  return http.get('*/api/v1/statements', () => HttpResponse.json(body))
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

describe('QueryListPage', () => {
  it('UI-QRY-010 changes the server-side order_by when sorting changes', async () => {
    const orderBys: string[] = []
    server.use(
      http.get('*/api/v1/statements', ({ request }) => {
        orderBys.push(new URL(request.url).searchParams.get('order_by') ?? '')
        return HttpResponse.json(statementResponse())
      }),
    )

    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&sort=calls`)

    await settle()
    expect(orderBys).toContain('calls')
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort statements' }), {
      target: { value: 'mean_exec_time' },
    })

    await settle()
    expect(orderBys).toContain('mean_exec_time')
    expect(screen.getByRole('combobox', { name: 'Sort statements' })).toHaveValue('mean_exec_time')
  })

  it('UI-QRY-011 shows the configured truncation budget', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          truncated: true,
        }),
      ),
    )

    renderPage()

    await settle()
    expect(screen.getAllByText(/truncated; budget 50/).length).toBeGreaterThan(0)
    expect(screen.getByText('checks.stat_statements.top_n')).toBeInTheDocument()
  })

  it('UI-QRY-012 renders Disabled when pg_stat_statements is absent', async () => {
    server.use(http.get('*/api/v1/statements', () => new HttpResponse(null, { status: 404 })))

    renderPage()

    await settle()
    expect(screen.getByText(/pg_stat_statements disabled/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /README extension guidance/i })).toHaveAttribute(
      'href',
      '/README.md#agent-configuration',
    )
  })

  it('UI-QRY-013 keeps the pg_stat_statements eviction and queryid scope note visible', async () => {
    server.use(statementsHandler(statementResponse({ statements: [] })))

    renderPage()

    await settle()
    expect(screen.getByText(/Entries beyond/)).toBeInTheDocument()
    expect(document.body.textContent).toContain(
      'queryid values are comparable only within this cluster',
    )
  })

  it('UI-QRY-014 links each row to its detail route without converting queryid to a number', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          statements: [
            {
              calls: 1,
              datname: 'postgres',
              query_text: 'SELECT 1',
              queryid: '9223372036854775807',
              rows: 1,
              total_exec_time_ms: 2,
              truncated: false,
            },
          ],
        }),
      ),
    )

    renderPage()

    await settle()
    const link = screen.getByRole('link', { name: 'Query 9223372036854775807' })
    expect(link).toHaveAttribute('href', `/instances/${INSTANCE_ID}/queries/9223372036854775807`)
  })

  it('renders the populated statement list without accessibility violations', async () => {
    server.use(statementsHandler(statementResponse()))

    const { container } = renderPage()

    await settle()
    screen.getByRole('link', { name: 'Query 123' })
    vi.useRealTimers()
    await expectNoA11yViolations(container)
    expect(container).toBeTruthy()
  })
})
