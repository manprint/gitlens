import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

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
  it('UI-AUTH-001 submits the password and navigates to next', async () => {
    server.use(sequence('createSession', { status: 204 }))
    const view = renderWithProviders(<LoginPage />, {
      route: '/login?next=%2Fclusters%3Ftab%3Dtop',
    })
    const input = screen.getByLabelText('Password')

    fireEvent.change(input, { target: { value: 'correct horse battery staple' } })
    fireEvent.submit(input.closest('form') as HTMLFormElement)
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
    fireEvent.submit(input.closest('form') as HTMLFormElement)
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent('Password non valida.')
    expect(input).toHaveFocus()
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
    fireEvent.submit(input.closest('form') as HTMLFormElement)
    await settle()

    expect(view.router.state.location.pathname).toBe(expected)
  })

  it('UI-AUTH-004 explains how to configure a disabled server', async () => {
    server.use(status('getSession', 200, { authenticated: false, configured: false }))
    renderRoute('/')
    await settle()

    expect(screen.getByRole('heading', { name: 'Interfaccia non configurata' })).toBeInTheDocument()
    expect(screen.getByText('PGLENS_UI_PASSWORD')).toBeInTheDocument()
  })

  it('UI-AUTH-005 sign-out clears the cache and navigates to login', async () => {
    server.use(status('deleteSession', 204))
    const view = renderWithProviders(<SignOutProbe />, { route: '/clusters' })
    view.queryClient.setQueryData(['getClusters'], [{ cluster_id: 'stale' }])

    fireEvent.click(screen.getByRole('button', { name: 'Esci' }))
    await settle()

    expect(view.queryClient.getQueryData(['getClusters'])).toBeUndefined()
    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('UI-AUTH-006 has no login flash while session status is loading', async () => {
    server.use(slow('getSession', { authenticated: true }, 1000))
    const view = renderRoute('/')
    expect(screen.getByTestId('session-loading')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Accedi a pglens' })).not.toBeInTheDocument()
    view.unmount()
  })

  it('has no serious accessibility violations', async () => {
    vi.useRealTimers()
    const view = renderWithProviders(<LoginPage />, { route: '/login' })
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
      Esci
    </button>
  )
}
