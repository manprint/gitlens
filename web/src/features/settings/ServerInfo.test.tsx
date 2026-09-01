import { act, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { formatTimestamp } from '@/lib/format'
import { NOW } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { ServerInfo } from './ServerInfo'

async function renderServerInfo(session: Record<string, unknown> = {}) {
  server.use(
    ok('getSession', {
      authenticated: true,
      configured: true,
      expires_at: NOW,
      ...session,
    }),
  )
  const view = renderWithProviders(<ServerInfo />)
  vi.useRealTimers()
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
  return view
}

describe('ServerInfo', () => {
  it('UI-SET-010 renders the build identifier', async () => {
    await renderServerInfo()

    expect(screen.getByText(__PGLENS_BUILD__, { exact: true })).toBeInTheDocument()
  })

  it('UI-SET-011 names the retention window and PostgreSQL version range', async () => {
    await renderServerInfo()

    expect(screen.getByRole('heading', { name: 'Product limits' })).toBeInTheDocument()
    expect(screen.getByText(/retained for 30 days/i)).toBeInTheDocument()
    expect(screen.getByText(/PostgreSQL 15 through 18/i)).toBeInTheDocument()
  })

  it('UI-SET-012 states that no pooler view exists', async () => {
    await renderServerInfo()

    expect(screen.getByText('There is no pooler view in this release.')).toBeInTheDocument()
  })

  it('UI-SET-013 states the absence of user accounts and roles', async () => {
    await renderServerInfo()

    expect(screen.getByText('No user accounts exist.')).toBeInTheDocument()
    expect(screen.getByText('No roles or per-user audit exist.')).toBeInTheDocument()
  })

  it('UI-SET-014 renders session expiry from the session endpoint', async () => {
    await renderServerInfo()

    const expiry = screen.getByText(formatTimestamp(NOW, 'UTC'))
    expect(expiry).toBeInTheDocument()
    expect(expiry).toHaveAttribute('dateTime', NOW)
  })
})
