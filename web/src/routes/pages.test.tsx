import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { base as clusters } from '@/test/fixtures/getClusters'
import { base as command } from '@/test/fixtures/getCommand'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { base as plans } from '@/test/fixtures/getPlans'
import { base as session } from '@/test/fixtures/getSession'
import { ok, slow } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { preloadLazyRoutes, renderRoute } from '@/test/render'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'
// The route waits on the instance response, which can take longer than
// vi.waitFor's 1 s default. The budget is counted on the fake clock: vi.waitFor
// spends a real millisecond for every fake one it advances, plus the check
// itself, so on a loaded run the instance's 1.5 s delay alone used up most of a
// real-time budget. The checks use queryByRole, because a failing getByRole
// lists every accessible role on the page, which made each check cost tens of
// milliseconds. The page modules are loaded in beforeAll, so the steps measure
// the page and not the module transform.
const ROUTE_BUDGET_MS = 4000
const ROUTE_STEP_MS = 50

async function waitForRoute(assertion: () => void): Promise<void> {
  for (let elapsed = 0; ; elapsed += ROUTE_STEP_MS) {
    try {
      assertion()
      return
    } catch (error) {
      if (elapsed >= ROUTE_BUDGET_MS) throw error
    }
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ROUTE_STEP_MS)
      await new Promise<void>((resolve) => {
        setImmediate(resolve)
      })
    })
  }
}

describe('query detail route', () => {
  beforeAll(preloadLazyRoutes)

  it('AC-08 runs EXPLAIN and reads plan history in the database the statement came from', async () => {
    let explainBody: unknown
    let plansUrl: URL | undefined
    server.use(
      ok('getSession', session),
      ok('getClusters', clusters),
      // Answers after vi.waitFor's 1 s default, as a loaded instance can.
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
    await waitForRoute(() =>
      expect(screen.queryByRole('button', { name: 'Run plan only' })).toBeEnabled(),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run plan only' }))
    await waitForRoute(() => expect(explainBody).toBeDefined())

    expect(explainBody).toEqual({
      args: { analyze: false, datname: 'app', queryid: '123' },
      kind: 'explain',
    })
    await waitForRoute(() => expect(plansUrl).toBeDefined())
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
    await waitForRoute(() =>
      expect(screen.queryByRole('alert')).toHaveTextContent('other is not monitored'),
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
