import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { sequence } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

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

    expect(screen.getByRole('alert')).toHaveTextContent('Impossibile verificare la sessione.')
    fireEvent.click(screen.getByRole('button', { name: 'Riprova' }))
    await settle()

    expect(screen.getByText('Protected content')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
