import { act } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { REFRESH } from '@/api/policy'
import { flushNetwork } from '@/test/network'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { AshPage } from './AshPage'

// The other AshPage suites mock the query hooks, so they cannot see a request
// loop. This one runs the real hooks against MSW on the real clock: a relative
// window must not move between renders, or every completed ASH request starts
// the next one.
const ashResponse = {
  buckets: [],
  enabled: true,
  resolution_seconds: 30,
  statistical: false,
}

vi.mock('echarts-for-react', () => ({
  default: () => <div data-testid="mock-echarts" />,
}))

const instanceId = '11111111-1111-4111-8111-111111111111'
const observedAshRequests: string[] = []

afterEach(() => {
  server.events.removeAllListeners()
  observedAshRequests.length = 0
})

describe('AshPage refresh', () => {
  it('UI-ASH-045 does not refetch a relative range between renders', async () => {
    vi.useRealTimers()
    server.events.on('request:start', ({ request }) => {
      if (new URL(request.url).pathname.endsWith('/api/v1/ash')) {
        observedAshRequests.push(request.url)
      }
    })
    server.use(
      ok('getAsh', ashResponse),
      ok('getInstanceSettings', { instance_id: instanceId, settings: [] }),
    )

    renderWithProviders(<AshPage instanceId={instanceId} />, {
      route: `/instances/${instanceId}/ash?range=1h`,
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 1_500))
    })

    // The first fetch is one request; a clock-derived window produces hundreds in this window.
    expect(observedAshRequests.length).toBeLessThanOrEqual(3)
  })

  it('UI-ASH-046 sends one request per poll for a relative range', async () => {
    server.events.on('request:start', ({ request }) => {
      if (new URL(request.url).pathname.endsWith('/api/v1/ash')) {
        observedAshRequests.push(request.url)
      }
    })
    server.use(
      ok('getAsh', ashResponse),
      ok('getInstanceSettings', { instance_id: instanceId, settings: [] }),
    )

    renderWithProviders(<AshPage instanceId={instanceId} />, {
      route: `/instances/${instanceId}/ash?range=1h`,
    })
    await flushNetwork()

    // Five minutes at the ASH poll interval: the mount fetch, then one per poll.
    // A window taken from the render clock sent a second request per poll.
    for (let poll = 0; poll < 10; poll += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(REFRESH.ash.interval)
      })
      await flushNetwork()
    }

    expect(observedAshRequests).toHaveLength(11)
  })
})
