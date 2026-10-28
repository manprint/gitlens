import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ok, sequence } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderRoute, renderWithProviders } from '@/test/render'

import RequireSession from './RequireSession'

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

describe('RequireSession', () => {
  it('UI-AUTH-009 retries a failed session check from the page and renders the content', async () => {
    server.use(
      sequence(
        'getSession',
        { status: 500, body: { detail: 'down', error: 'internal_error' } },
        { authenticated: true, configured: true },
      ),
    )
    renderWithProviders(
      <RequireSession>
        <p>Protected content</p>
      </RequireSession>,
      { route: '/private' },
    )
    await settle()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not verify the session.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await settle()

    expect(screen.getByText('Protected content')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('sends a signed-out operator to sign-in with the requested page as next', async () => {
    server.use(ok('getSession', { authenticated: false, configured: true }))
    const result = renderRoute('/instances/i1/ash?range=6h')
    await settle()

    expect(result.router.state.location.pathname).toBe('/login')
    expect(new URLSearchParams(result.router.state.location.search).get('next')).toBe(
      '/instances/i1/ash?range=6h',
    )
  })
})
