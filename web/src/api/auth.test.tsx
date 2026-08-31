import { act, fireEvent, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { NavigateFunction } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { useQueryClient } from '@tanstack/react-query'

import { client } from './client'
import { configureAuth, useSession, useSignIn, useSignOut } from './auth'
import { qk } from './keys'
import { renderRoute, renderWithProviders } from '@/test/render'
import { unauthenticated } from '@/test/fixtures/getSession'
import { server } from '@/test/msw/server'
import { status } from '@/test/msw/handlers'

const navigate = () => vi.fn() as unknown as NavigateFunction

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

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

  it('reads an unauthenticated session response without treating it as a transport error', async () => {
    server.use(status('getSession', 401, unauthenticated))
    renderWithProviders(<SessionProbe />)
    await settle()

    expect(screen.getByTestId('session-probe')).toHaveTextContent('unauthenticated')
  })

  it('reports malformed and network session responses', async () => {
    server.use(http.get('*/api/v1/session', () => new HttpResponse(null, { status: 200 })))
    renderWithProviders(<SessionProbe />)
    await settle()
    expect(screen.getByTestId('session-probe')).toHaveTextContent('malformed')

    server.resetHandlers()
    server.use(http.get('*/api/v1/session', () => HttpResponse.error()))
    renderWithProviders(<SessionProbe />)
    await settle()
    expect(screen.getAllByTestId('session-probe').at(-1)).toHaveTextContent('network')
  })

  it('covers the session action wrappers and successful mutations', async () => {
    server.use(
      status('getSession', 200, { authenticated: true, configured: true }),
      status('createSession', 204),
      status('deleteSession', 204),
    )
    const view = renderWithProviders(<SessionActionsProbe />)
    await settle()
    fireEvent.click(screen.getByRole('button', { name: 'sign in' }))
    await settle()
    fireEvent.click(screen.getByRole('button', { name: 'sign out' }))
    await settle()

    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('normalizes sign-in network failures', async () => {
    server.use(http.post('*/api/v1/session', () => HttpResponse.error()))
    renderWithProviders(<SignInErrorProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'sign in' }))
    await settle()

    expect(screen.getByTestId('mutation-probe')).toHaveTextContent('network')
  })

  it('normalizes sign-out HTTP and network failures', async () => {
    server.use(status('deleteSession', 401, { detail: 'expired', error: 'unauthorized' }))
    const httpFailure = renderWithProviders(<SignOutErrorProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'sign out' }))
    await settle()
    expect(screen.getByTestId('mutation-probe')).toHaveTextContent('unauthorized')
    httpFailure.unmount()

    server.resetHandlers()
    server.use(http.delete('*/api/v1/session', () => HttpResponse.error()))
    renderWithProviders(<SignOutErrorProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'sign out' }))
    await settle()
    expect(screen.getByTestId('mutation-probe')).toHaveTextContent('network')
  })

  it('RequireSession redirects an unauthenticated response and reports other errors', async () => {
    server.use(status('getSession', 401, unauthenticated))
    const unauthenticatedView = renderRoute('/private')
    await settle()
    expect(unauthenticatedView.router.state.location.pathname).toBe('/login')
    unauthenticatedView.unmount()

    server.resetHandlers()
    server.use(status('getSession', 500, { detail: 'down', error: 'internal_error' }))
    renderRoute('/private')
    await settle()
    expect(screen.getByRole('alert')).toHaveTextContent('Impossibile verificare')
  })
})

function SessionProbe() {
  const session = useSession()
  const label = session.isPending
    ? 'loading'
    : (session.error?.kind ?? (session.data?.authenticated ? 'authenticated' : 'unauthenticated'))
  return <output data-testid="session-probe">{label}</output>
}

function SessionActionsProbe() {
  const session = useSession()
  return (
    <>
      <button type="button" onClick={() => void session.signIn('secret')}>
        sign in
      </button>
      <button type="button" onClick={() => void session.signOut()}>
        sign out
      </button>
    </>
  )
}

function SignInErrorProbe() {
  const signIn = useSignIn()
  const [error, setError] = useState('idle')
  return (
    <>
      <button
        type="button"
        onClick={() => {
          void signIn
            .mutateAsync('secret')
            .then(() => setError('success'))
            .catch((failure: { kind?: string }) => setError(failure.kind ?? 'unknown'))
        }}
      >
        sign in
      </button>
      <output data-testid="mutation-probe">{error}</output>
    </>
  )
}

function SignOutErrorProbe() {
  const queryClient = useQueryClient()
  const signOut = useSignOut()
  const [error, setError] = useState('idle')
  configureAuth(queryClient)
  return (
    <>
      <button
        type="button"
        onClick={() => {
          void signOut
            .mutateAsync()
            .then(() => setError('success'))
            .catch((failure: { kind?: string }) => setError(failure.kind ?? 'unknown'))
        }}
      >
        sign out
      </button>
      <output data-testid="mutation-probe">{error}</output>
    </>
  )
}
