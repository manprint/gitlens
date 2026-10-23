import { fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { base as clusters } from '@/test/fixtures/getClusters'
import { base as command } from '@/test/fixtures/getCommand'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { base as plans } from '@/test/fixtures/getPlans'
import { base as session } from '@/test/fixtures/getSession'
import { ok, slow } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderRoute } from '@/test/render'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'
// The route loads lazily and waits on the instance response. On a busy full run
// that can take longer than vi.waitFor's 1 s default. The budget stays below the
// 5 s testTimeout, so a stalled wait reports its own assertion instead of racing
// the test timeout.
const ROUTE_WAIT = { timeout: 4000 }

describe('query detail route', () => {
  it('AC-08 runs EXPLAIN and reads plan history in the database the statement came from', async () => {
    let explainBody: unknown
    let plansUrl: URL | undefined
    server.use(
      ok('getSession', session),
      ok('getClusters', clusters),
      // Answers after vi.waitFor's 1 s default, as a loaded full run can.
      slow('getInstance', monitoredInstance(['app']), 1500),
      ok('getCommand', command),
      http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
        explainBody = await request.json()
        return HttpResponse.json({ command_id: COMMAND_ID })
      }),
      http.get('*/api/v1/plans', ({ request }) => {
        plansUrl = new URL(request.url)
        return HttpResponse.json(plans)
      }),
    )

    renderRoute(`/instances/${INSTANCE_ID}/queries/123?db=app`)
    // Routes are lazy and the instance response sets the tier, so wait until
    // the plan button is enabled by T1 rather than labelled as not permitted.
    await vi.waitFor(
      () => expect(screen.getByRole('button', { name: 'Run plan only' })).toBeEnabled(),
      ROUTE_WAIT,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run plan only' }))
    await vi.waitFor(() => expect(explainBody).toBeDefined(), ROUTE_WAIT)

    expect(explainBody).toEqual({
      args: { analyze: false, datname: 'app', queryid: '123' },
      kind: 'explain',
    })
    await vi.waitFor(() => expect(plansUrl).toBeDefined(), ROUTE_WAIT)
    expect(plansUrl?.searchParams.get('datname')).toBe('app')
  })

  it('AC-08 refuses a database the instance does not monitor and sends nothing for it', async () => {
    let commandRequests = 0
    let plansRequests = 0
    server.use(
      ok('getSession', session),
      ok('getClusters', clusters),
      ok('getInstance', monitoredInstance(['app'])),
      http.post('*/api/v1/instances/:id/commands', () => {
        commandRequests += 1
        return HttpResponse.json({ command_id: COMMAND_ID })
      }),
      http.get('*/api/v1/plans', () => {
        plansRequests += 1
        return HttpResponse.json(plans)
      }),
    )

    renderRoute(`/instances/${INSTANCE_ID}/queries/123?db=other`)
    await vi.waitFor(
      () => expect(screen.getByRole('alert')).toHaveTextContent('other is not monitored'),
      ROUTE_WAIT,
    )
    expect(screen.queryByRole('button', { name: 'Run plan only' })).not.toBeInTheDocument()
    expect(commandRequests).toBe(0)
    expect(plansRequests).toBe(0)
  })
})

function monitoredInstance(monitored: readonly string[]) {
  return {
    ...makeGetInstance({ perm_tier: 'T1' }),
    databases: monitored.map((datname) => ({ datname, monitored: true, skip_reason: null })),
  }
}
