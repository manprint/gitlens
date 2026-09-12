import { screen, waitFor } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useStatements } from './queries'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

function StatementsProbe() {
  const query = useStatements({ instance_id: 'i-1', from: '2026-01-01T00:00:00Z' })
  return <output data-testid="probe">{query.isLoading ? 'loading' : 'settled'}</output>
}

/**
 * UI-API-ABORT-001 — every read in queries.ts polls on an interval, and the
 * expensive ones (statements, ASH over a wide range, metrics/query) hold one
 * of the server's bounded pool connections for the whole query. Without
 * react-query's AbortSignal threaded into the request, unmounting — a
 * navigation, a closed tab — left those requests running to completion with
 * nobody to receive them: the browser kept the socket and the server kept the
 * database connection until the query finished on its own. This asserts the
 * request is genuinely aborted, which is the only thing that frees either.
 */
describe('request cancellation', () => {
  beforeEach(() => {
    // The suite runs on frozen fake timers; this test needs msw's delay and
    // waitFor's own polling to actually advance.
    vi.useRealTimers()
  })

  it('UI-API-ABORT-001 aborts an in-flight read when the consumer unmounts', async () => {
    let sawAbort = false
    server.use(
      http.get('/api/v1/statements', async ({ request }) => {
        const aborted = new Promise<void>((resolve) => {
          request.signal.addEventListener('abort', () => {
            sawAbort = true
            resolve()
          })
        })
        await Promise.race([aborted, delay(2_000)])
        return HttpResponse.json({ items: [], truncated: false })
      }),
    )

    const view = renderWithProviders(<StatementsProbe />)
    expect(await screen.findByTestId('probe')).toHaveTextContent('loading')
    view.unmount()

    await waitFor(() => {
      expect(sawAbort).toBe(true)
    })
  })
})
