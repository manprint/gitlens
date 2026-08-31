import { act } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { NavigateFunction } from 'react-router-dom'

import { client } from './client'
import { configureAuth } from './auth'
import { qk } from './keys'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'
import { status } from '@/test/msw/handlers'

const navigate = () => vi.fn() as unknown as NavigateFunction

describe('authentication middleware', () => {
  it('UI-API-006 navigates once for concurrent unauthorized responses', async () => {
    server.use(status('getClusters', 401))
    const view = renderWithProviders(<output>auth test</output>, { route: '/clusters' })
    const navigateSpy = navigate()
    configureAuth(view.queryClient, navigateSpy)

    await act(async () => {
      await Promise.all(Array.from({ length: 8 }, () => client.GET('/api/v1/clusters')))
    })

    expect(navigateSpy).toHaveBeenCalledTimes(1)
    expect(navigateSpy).toHaveBeenCalledWith(expect.stringContaining('/login?next='), {
      replace: true,
    })
  })

  it('UI-API-007 clears cached application data after a 401', async () => {
    server.use(status('getClusters', 401))
    const view = renderWithProviders(<output>auth test</output>)
    view.queryClient.setQueryData(['getClusters'], [{ cluster_id: 'stale' }])
    const navigateSpy = navigate()
    configureAuth(view.queryClient, navigateSpy)

    await act(async () => {
      await client.GET('/api/v1/clusters')
    })

    expect(view.queryClient.getQueryData(['getClusters'])).toBeUndefined()
    expect(view.queryClient.getQueryData(qk.session())).toEqual({
      authenticated: false,
      configured: true,
    })
  })
})
