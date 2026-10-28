import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw/http'

import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'

import { configureAuth, useSignOut } from '@/api/auth'
import LoginPage from './LoginPage'
import { renderRoute, renderWithProviders } from '@/test/render'
import { expectNoA11yViolations } from '@/test/a11y'
import { server } from '@/test/msw/server'
import { sequence, slow, status } from '@/test/msw/handlers'

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

describe('LoginPage', () => {
  it('shows and hides the typed characters on request', () => {
    renderWithProviders(<LoginPage />, { route: '/login' })
    const input = screen.getByLabelText('Password')

    expect(input).toHaveAttribute('type', 'password')
    fireEvent.click(screen.getByRole('button', { name: 'Show password' }))
    expect(input).toHaveAttribute('type', 'text')
    fireEvent.click(screen.getByRole('button', { name: 'Hide password' }))
    expect(input).toHaveAttribute('type', 'password')
  })

  it('UI-AUTH-001 submits the password and navigates to next', async () => {
    server.use(sequence('createSession', { status: 204 }))
    const view = renderWithProviders(<LoginPage />, {
      route: '/login?next=%2Fclusters%3Ftab%3Dtop',
    })
    const input = screen.getByLabelText('Password')

    fireEvent.change(input, { target: { value: 'correct horse battery staple' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(view.router.state.location.pathname).toBe('/clusters')
    expect(view.router.state.location.search).toBe('?tab=top')
  })

  it('UI-AUTH-002 alerts on a wrong password and focuses the field', async () => {
    server.use(
      status('createSession', 401, {
        detail: 'password rejected',
        error: 'unauthorized',
      }),
    )
    renderWithProviders(<LoginPage />, { route: '/login' })
    const input = screen.getByLabelText('Password')

    fireEvent.change(input, { target: { value: 'wrong' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent('Invalid password.')
    expect(input).toHaveFocus()
  })

  it('UI-AUTH-007 says the server is unreachable instead of blaming the password', async () => {
    server.use(http.post('/api/v1/session', () => HttpResponse.error()))
    renderWithProviders(<LoginPage />, { route: '/login' })

    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Cannot reach the server. Check the connection and try again.',
    )
    expect(screen.queryByText('Invalid password.')).not.toBeInTheDocument()
  })

  it('UI-AUTH-008 says the server could not finish sign-in on a server error', async () => {
    server.use(
      status('createSession', 503, {
        detail: 'session store is not configured',
        error: 'session_unavailable',
      }),
    )
    renderWithProviders(<LoginPage />, { route: '/login' })

    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent(
      'The server could not complete the request.',
    )
    expect(screen.queryByText('Invalid password.')).not.toBeInTheDocument()
  })

  it('UI-AUTH-010 says how long to wait when the server refuses too many sign-in attempts', async () => {
    server.use(
      http.post('*/api/v1/session', () =>
        HttpResponse.json(
          {
            error: 'too_many_attempts',
            detail: 'too many sign-in attempts; wait before trying again',
          },
          { status: 429, headers: { 'Retry-After': '30' } },
        ),
      ),
    )
    renderWithProviders(<LoginPage />, { route: '/login' })

    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Too many sign-in attempts. Try again in 30 seconds.',
    )
    expect(screen.queryByText('Invalid password.')).not.toBeInTheDocument()
  })

  it('UI-AUTH-011 gives a general wait when a refused sign-in carries no Retry-After', async () => {
    server.use(
      http.post('*/api/v1/session', () =>
        HttpResponse.json(
          {
            error: 'too_many_attempts',
            detail: 'too many sign-in attempts; wait before trying again',
          },
          { status: 429 },
        ),
      ),
    )
    renderWithProviders(<LoginPage />, { route: '/login' })

    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Too many sign-in attempts. Wait a few minutes and try again.',
    )
  })

  it.each([
    ['/valid/path', '/valid/path'],
    ['https://evil.example', '/'],
    ['//evil.example', '/'],
  ])('UI-AUTH-003 accepts only a same-origin relative next (%s)', async (next, expected) => {
    server.use(sequence('createSession', { status: 204 }))
    const view = renderWithProviders(<LoginPage />, {
      route: `/login?next=${encodeURIComponent(next)}`,
    })
    const input = screen.getByLabelText('Password')

    fireEvent.change(input, { target: { value: 'correct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await settle()

    expect(view.router.state.location.pathname).toBe(expected)
  })

  it('UI-AUTH-004 explains how to configure a disabled server', async () => {
    server.use(status('getSession', 200, { authenticated: false, configured: false }))
    renderRoute('/')
    await settle()

    expect(screen.getByRole('heading', { name: 'Interface not configured' })).toBeInTheDocument()
    expect(screen.getByText('PGLENS_UI_PASSWORD')).toBeInTheDocument()
  })

  it('UI-AUTH-005 sign-out clears the cache and navigates to login', async () => {
    server.use(status('deleteSession', 204))
    const view = renderWithProviders(<SignOutProbe />, { route: '/clusters' })
    view.queryClient.setQueryData(['getClusters'], [{ cluster_id: 'stale' }])

    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
    await settle()

    expect(view.queryClient.getQueryData(['getClusters'])).toBeUndefined()
    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('UI-AUTH-006 has no login flash while session status is loading', () => {
    server.use(slow('getSession', { authenticated: true }, 1000))
    const view = renderRoute('/')
    expect(screen.getByTestId('session-loading')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Sign in to pglens' })).not.toBeInTheDocument()
    view.unmount()
  })

  it('has no serious accessibility violations', async () => {
    vi.useRealTimers()
    const view = renderWithProviders(<LoginPage />, { route: '/login' })
    expect(screen.getByRole('heading', { name: 'Sign in to pglens' })).toBeInTheDocument()
    await expectNoA11yViolations(view.container)
  })
})

function SignOutProbe() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  configureAuth(queryClient, navigate)
  const signOut = useSignOut()

  return (
    <button type="button" onClick={() => void signOut.mutateAsync()}>
      Sign out
    </button>
  )
}
