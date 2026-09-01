import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import PlanHistory from './PlanHistory'
import QueryListPage from './QueryListPage'

import { REFRESH } from '@/api/policy'
import { renderWithProviders } from '@/test/render'
import { NOW } from '@/test/time'
import { server } from '@/test/msw/server'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'

function statementResponse() {
  return {
    statements: [
      {
        calls: 1,
        datname: 'postgres',
        query_text: 'SELECT 1',
        queryid: 123,
        rows: 1,
        total_exec_time_ms: 4,
        truncated: false,
      },
    ],
    truncated: false,
  }
}

function renderList() {
  return renderWithProviders(<QueryListPage instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}/queries?range=1h`,
  })
}

function renderDetail() {
  return renderWithProviders(<PlanHistory instanceId={INSTANCE_ID} queryid="123" />, {
    route: `/instances/${INSTANCE_ID}/queries/123`,
  })
}

describe('Query Inspector degraded and error states', () => {
  it('UI-QRY-040 renders a clear empty state when no statements are collected', async () => {
    vi.useRealTimers()
    server.use(
      http.get('*/api/v1/statements', () =>
        HttpResponse.json({ statements: [], truncated: false }),
      ),
    )

    renderList()

    expect(await screen.findByText('No statements')).toBeInTheDocument()
    expect(screen.getByText(/No pg_stat_statements entries match/)).toBeInTheDocument()
  })

  it('UI-QRY-041 explains when a query detail was evicted', async () => {
    vi.useRealTimers()
    server.use(http.get('*/api/v1/plans', () => new HttpResponse(null, { status: 404 })))

    renderDetail()

    expect(await screen.findByText(/no longer available in plan history/)).toBeInTheDocument()
    expect(screen.getByText(/may have been evicted from pg_stat_statements/)).toBeInTheDocument()
  })

  it('UI-QRY-042 marks statement data stale after the freshness threshold', async () => {
    server.use(http.get('*/api/v1/statements', () => HttpResponse.json(statementResponse())))

    const rendered = renderList()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    const query = rendered.queryClient.getQueryCache().findAll()[0]
    expect(query).toBeDefined()

    vi.setSystemTime(new Date(NOW.getTime() + REFRESH.statements.staleAfter + 1_000))
    act(() => {
      rendered.queryClient.setQueryData(query!.queryKey, query!.state.data, {
        updatedAt: NOW.getTime(),
      })
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(screen.getByText(/Stale/)).toBeInTheDocument()
  })

  it('UI-QRY-043 renders the authentication error and retry control for 401', async () => {
    vi.useRealTimers()
    server.use(http.get('*/api/v1/statements', () => new HttpResponse(null, { status: 401 })))

    renderList()

    expect(await screen.findByRole('alert')).toHaveTextContent('Authentication is required.')
    expect(screen.getByRole('button', { name: 'Retry statements' })).toBeInTheDocument()
  })

  it('UI-QRY-044 retries a failed statement request after a 500 response', async () => {
    vi.useRealTimers()
    let calls = 0
    server.use(
      http.get('*/api/v1/statements', () => {
        calls += 1
        return calls === 1
          ? HttpResponse.json(
              { error: 'backend unavailable', detail: 'try again' },
              { status: 500 },
            )
          : HttpResponse.json(statementResponse())
      }),
    )

    renderList()

    expect(await screen.findByRole('alert')).toHaveTextContent('backend unavailable')
    fireEvent.click(screen.getByRole('button', { name: 'Retry statements' }))
    await waitFor(() => expect(screen.getByRole('link', { name: 'Query 123' })).toBeInTheDocument())
    expect(calls).toBe(2)
  })
})
