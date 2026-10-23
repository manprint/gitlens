import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { base as clusters } from '@/test/fixtures/getClusters'
import { base as command } from '@/test/fixtures/getCommand'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { base as plans } from '@/test/fixtures/getPlans'
import { base as session } from '@/test/fixtures/getSession'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderRoute } from '@/test/render'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'

describe('query detail route', () => {
  it('AC-08 runs EXPLAIN and reads plan history in the database the statement came from', async () => {
    let explainBody: unknown
    let plansUrl: URL | undefined
    server.use(
      ok('getSession', session),
      ok('getClusters', clusters),
      ok('getInstance', makeGetInstance({ perm_tier: 'T1' })),
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
    await act(async () => {
      await import('./pages')
    })
    await vi.waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run plan only' })).toBeEnabled(),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run plan only' }))
    await vi.waitFor(() => expect(explainBody).toBeDefined())

    expect(explainBody).toEqual({
      args: { analyze: false, datname: 'app', queryid: '123' },
      kind: 'explain',
    })
    await vi.waitFor(() => expect(plansUrl).toBeDefined())
    expect(plansUrl?.searchParams.get('datname')).toBe('app')
  })
})
